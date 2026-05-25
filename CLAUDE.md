# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Rules

- *ALWAYS* actively ask questions if there is anything unclear.
- *ALWAYS* update related documents if you made functional changes.
- *ALWAYS* read the relevant spec documents under `backend/docs/specs/` before modifying backend code. The specs are the authoritative source for design decisions; do not infer intent from code alone.

## Planning & Design

When asked to plan or design something, create structured plan/spec artifacts (in `/plans/`) BEFORE writing any implementation files. Only proceed to code after the plan is reviewed. See `plans/README.md` for conventions.

## Testing

Always run the full test suite with race detection after multi-file backend changes, and verify a clean build before reporting completion:

```bash
cd backend && go test -race ./...
cd backend && go build ./...
```

## Code Modification Guidelines

Before removing existing logic (waits, retries, deduplication, lifecycle hooks), explain why it exists and confirm with the user. Do not assume code is dead.

---

## Commands

### Backend (Go)

```bash
cd backend

# Run dev server
go run ./cmd/server

# Run with custom config
go run ./cmd/server -config /path/to/config.yaml

# Build binary
go build -o bin/server ./cmd/server

# Run all tests (with race detection)
go test -race ./...

# Run a single package
go test -race ./internal/task/...

# Run a single test
go test -race -run TestName ./internal/task/...

# Regenerate Swagger docs (after editing handler annotations)
swag init -g cmd/server/main.go --output docs --parseDependency --parseInternal
```

### Frontend (Node)

```bash
cd frontend

npm run dev      # http://localhost:5173
npm run build    # type-check + production bundle
npm run lint     # ESLint
```

### Prerequisites

```bash
# MySQL database
mysql -u root -p -e "CREATE DATABASE IF NOT EXISTS l_lab CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"

# Copy and fill in config
cp backend/config.example.yaml backend/config.yaml
```

---

## Architecture

This is a full-stack Claude Code hosting platform. The browser talks to a Go backend, which provisions isolated OpenSandbox containers running `claude-agent-server` and proxies SSE streams back to the client.

```
Browser (React/Vite :5173)
  │  /api/*  (Vite proxy in dev)
  ▼
Go backend (:8091)
  │  lazy-provisions sandbox on first message
  ▼
OpenSandbox server (:8080)
  └─ container port 3000 → claude-agent-server
```

### Backend (`backend/`)

**Entry point:** `cmd/server/main.go` — loads config, wires deps, starts Gin server.

**Key packages:**

| Package              | Responsibility                                                        |
| -------------------- | --------------------------------------------------------------------- |
| `internal/api`       | Gin router + HTTP handlers (split per domain) + request/response types |
| `internal/task`      | Task state machine, repository interface, Memory/MySQL/Redis backends  |
| `internal/sandbox`   | OpenSandbox lifecycle (create → poll → health-check) + SSE proxy       |
| `internal/schedule`  | Cron scheduler, schedule CRUD service, per-schedule fire logic         |
| `internal/session`   | OFS session metadata retrieval + SessionStore interface                |
| `internal/auth`      | HS256 JWT issue/verify, Bearer middleware, Gin context helpers         |
| `internal/crypto`    | AES-256-GCM encrypt/decrypt for per-user secrets (SSH key, API key)   |
| `internal/db`        | GORM models (User, Task, ScheduledTask, Kind, ScheduleToken), AutoMigrate, MySQL connection |
| `internal/oidc`      | go-oidc provider wrapper + CLI login flow                              |
| `internal/sso`       | Didi SSO HTTP client + handlers                                        |
| `internal/storage`   | OFS (S3-compatible) client for conversation history                    |
| `pkg/config`         | YAML config loader with defaults                                       |

**Task state machine:** `pending → provisioning → running`, with `error` on failure. The sandbox is created lazily on the first message via `EnsureProvisioned`, which holds a distributed Redis lock to prevent double-provisioning. `session_id` is write-once and never cleared, so history in OFS remains accessible after a sandbox expires.

**Storage layout:**
- **MySQL** — durable task fields (id, username, state, title, session_id, extra_env, schedule_id) + `scheduled_tasks` table
- **Redis** — ephemeral sandbox routing (`sandbox:{id}`) + distributed provisioning lock (`task-lock:{id}`)
- **OFS (S3)** — NDJSON conversation history keyed by `TASK_ID`

**Swagger UI:** `http://localhost:8091/swagger/index.html`

### Frontend (`frontend/src/`)

**Stack:** Vite 6, React 19, TypeScript, Tailwind CSS v4, shadcn/ui (neutral palette), lucide-react, react-markdown.

**Key files:**

| File                          | Responsibility                                                        |
| ----------------------------- | --------------------------------------------------------------------- |
| `hooks/useChat.ts`            | Single source of truth: all chat state + SSE streaming logic          |
| `api/client.ts`               | Thin fetch wrappers for the backend REST API                          |
| `lib/chainBuilder.ts`         | Converts `SessionEntry[]` (OFS history) → `Message[]` for replay      |
| `lib/auth.ts`                 | JWT token management (localStorage)                                   |
| `pages/ChatPage.tsx`          | Three-column resizable layout: sidebar | chat | workspace panel        |
| `pages/SchedulesPage.tsx`     | List of user schedules with enable/disable toggle                     |
| `pages/ScheduleFormPage.tsx`  | Create/edit form for schedules (recurring cron or one-time)           |
| `pages/ScheduleDetailPage.tsx`| Schedule detail + run history; links runs back to ChatPage            |
| `types/session.ts`            | Typed union for every NDJSON entry type                               |

**Routes:**

| Path | Component |
|---|---|
| `/` | `ChatPage` |
| `/resources` | `ResourcesPage` |
| `/settings` | `SettingsPage` |
| `/schedules` | `SchedulesPage` |
| `/schedules/new` | `ScheduleFormPage mode="create"` |
| `/schedules/:id` | `ScheduleDetailPage` |
| `/schedules/:id/edit` | `ScheduleFormPage mode="edit"` |
| `/login` | `LoginPage` |
| `/login/sso` | `SSOCallbackPage` |
| `/login/oidc` | `SSOCallbackPage` |

**SSE flow:** `useChat.sendMessage` → creates task if needed → POST to backend → reads SSE body as stream via `parseSSE` async generator → dispatches events to update message state. `session.completed` (not `result`) is the terminal event that re-enables input. When an SSE stream is already open (`sending === true`), subsequent `sendMessage` calls are routed to `POST /api/tasks/:id/steer` instead (steering), which injects the prompt into the active agent run without opening a new stream.

**Key SSE events:** `session.init` (sets cwd, transitions sandbox to running), `message.assistant` (text delta + tool_use blocks), `message.user` (tool_result blocks — updates matching ToolUseBlock.result), `task.started` (appends ToolActivity), `task.progress` (updates last ToolActivity description + `lastToolName`), `permission.requested` (tool permission prompt), `question.asked` (AskUserQuestion prompt), `session.status` (sets message `status: 'done'` when payload status is `'idle'`), `result` (run complete), `session.completed` (terminal — clears sending, calls onSessionCompleted).

**History replay:** Clicking a task in the sidebar calls `getHistory(id)` → `buildMessages(entries)` → `loadTask(id, messages, cwd)`, making the task immediately resumable. `useChat` returns `{ messages, taskId, cwd, sandboxState, sending, hasMoreHistory, loadingMoreHistory, sendMessage, approvePermission, answerQuestion, newChat, loadTask, loadMoreHistory, startTask }`.

**Message type** (`types.ts`): each message carries `text`, `status`, `toolActivity?: ToolActivity[]`, `toolUseBlocks?: ToolUseBlock[]`, `thinkingBlocks?: ThinkingBlock[]`, `permissionRequest?`, `pendingQuestions?`, `answeredQuestions?`, `attachments?: { name: string; blob: Blob }[]`, and `isCompactSummary?`.

**Subagent support:** `chainBuilder.ts` separates main-agent entries (`isSidechain: false`) from subagent entries (`isSidechain: true`), builds a `SubagentTrace` per subagent, and attaches it to the corresponding `Agent` tool-use block as `block.subagentTrace`. `ChatMessage.tsx` renders Agent tool-use blocks as `SubagentTraceCard` (violet-themed expandable card) rather than the generic `ToolUseCard` used for other tool calls.

**Sidebar schedule icon:** Tasks with a `schedule_id` show a `<Calendar>` icon in `HistorySidepanel` — but only when `git_url` is not also set (git-cloned tasks use the git icon instead).

**Path alias:** `@/*` → `src/*`.

**Adding shadcn components:** `npx shadcn@latest add <name>`

### Golang Best Practices

- Prefer concrete types over `map[string]any`; define a struct if one doesn't exist.
- Always update Swagger annotations in the relevant `internal/api/handlers_<domain>.go` file when adding or changing endpoints, then regenerate docs.
