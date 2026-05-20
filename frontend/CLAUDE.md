# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
npm run dev      # http://localhost:5173 (Vite dev server, proxies /api to :8091)
npm run build    # type-check + production bundle
npm run lint     # ESLint
```

Adding a shadcn component: `npx shadcn@latest add <name>` (neutral palette, CSS variables — see `components.json`).

## Architecture

### Routing (`src/App.tsx`)

| Route                       | Component                        | Auth                         |
| --------------------------- | -------------------------------- | ---------------------------- |
| `/`                         | `ChatPage`                       | Protected (`ProtectedRoute`) |
| `/resources`                | `ResourcesPage`                  | Protected                    |
| `/settings`                 | `SettingsPage`                   | Protected                    |
| `/schedules`                | `SchedulesPage`                  | Protected                    |
| `/schedules/new`            | `ScheduleFormPage mode="create"` | Protected                    |
| `/schedules/:id`            | `ScheduleDetailPage`             | Protected                    |
| `/schedules/:id/edit`       | `ScheduleFormPage mode="edit"`   | Protected                    |
| `/login`                    | `LoginPage`                      | Public                       |
| `/login/sso`, `/login/oidc` | `SSOCallbackPage`                | Public                       |

`ProtectedRoute` reads the JWT from localStorage via `auth.ts` and redirects to `/login` if absent or expired.

### Central state: `useChat` (`src/hooks/useChat.ts`)

All chat state lives here. Returns:

```ts
{ messages, taskId, cwd, sandboxState, sending,
  hasMoreHistory, loadingMoreHistory,
  sendMessage, approvePermission, answerQuestion,
  newChat, loadTask, loadMoreHistory, startTask }
```

- `hasMoreHistory` / `loadingMoreHistory` / `loadMoreHistory()` — paginated history loading; `loadMoreHistory` prepends older messages and advances the cursor.
- `startTask(tid)` — activates a pre-created task (from `NewTaskDialog`) without fetching history.
- `loadTask(tid, messages, cwd?, cursor?)` — loads history into state and sets the cursor for further pagination.

`sendMessage(prompt, files?, permissionMode?)` flow:
1. If `sending === true` (agent already active), routes to `steerMessage` immediately.
2. Creates task if `taskId` is null.
3. Appends optimistic user + empty assistant message (`status: 'streaming'`).
4. Sets `sandboxState → 'provisioning'`.
5. POSTs to backend (JSON or multipart), reads SSE body via the internal `parseSSE` async generator.

`parseSSE` buffers partial chunks, yields `{ event, data }` pairs.

`session.completed` (not `result`) is the terminal event that clears `sending` and calls the optional `onSessionCompleted` callback.

### SSE event → state mapping

| Event                   | Effect                                                               |
| ----------------------- | -------------------------------------------------------------------- |
| `session.init`          | `sandboxState → 'running'`; stores `cwd`; if count > 0 (steer), creates a new assistant bubble |
| `message.assistant`     | Appends `data.text` delta; collects `tool_use` blocks                |
| `message.user`          | Reads `tool_result` blocks from `data.message.content`; matches each by `tool_use_id` and sets `ToolUseBlock.result` on the active message |
| `permission.requested`  | Sets `status: 'requesting'`, attaches `permissionRequest` to message |
| `question.asked`        | Sets `status: 'asking'`, attaches `pendingQuestions` to message      |
| `session.status` (idle) | Sets `status: 'done'`                                                |
| `task.started`          | Pushes a new `ToolActivity{done: false}`                             |
| `task.progress`         | Updates last `ToolActivity` description (`data.description`) + tool name (`data.lastToolName`) |
| `result`                | Sets `status: 'done'`; aborted empty runs (`isError + aborted_streaming + no content`) are removed |
| `session.completed`     | Marks all tool activities `done`, clears `sending`, calls `onSessionCompleted` |
| `error`                 | Sets `status: 'error'`, `sandboxState → 'error'`                     |

### Message status lifecycle

```
streaming → done        (normal completion via result or session.status idle)
streaming → requesting  (pending tool permission — user must approve/deny)
streaming → asking      (pending AskUserQuestion — user must answer)
requesting → streaming  (after approvePermission)
asking → streaming      (after answerQuestion)
any → error             (SSE error event or HTTP non-ok)
```

`currentAssistantMsgIdRef` tracks the in-flight assistant message ID so out-of-order events are applied to the right message.

### Pages

**`ChatPage`** — three-column resizable layout: sidebar | chat | workspace panel (workspace only when `workspaceOpen && taskId && cwd`). Sidebar and workspace widths are draggable (160–480 px). `refreshToken` counter is incremented on `session.completed` to trigger workspace refresh.

**`ResourcesPage`** — CRUD for `skill` and `mcp` resources. Tabbed view. Optimistic toggle for `is_active`. `ResourceForm` is a shared create/edit form component. Supports ZIP upload for skills.

**`SettingsPage`** — SSH private key and Anthropic API key management. Both stored encrypted server-side; `has_ssh_key` / `has_anthropic_key` flags indicate presence.

**`SchedulesPage`** — lists user schedules with enabled/disabled toggle. Links to create and detail pages.

**`ScheduleFormPage`** — create or edit a schedule (`mode="create" | "edit"`). Supports recurring cron expressions, one-time `run_at`, optional `git_url`, and `extra_env`.

**`ScheduleDetailPage`** — schedule metadata + run history list. Each run links back to its task in `ChatPage`.

### Types (`src/types.ts`)

`MessageStatus`: `'streaming' | 'done' | 'error' | 'requesting' | 'asking'`

`Message` carries optional `permissionRequest`, `pendingQuestions`, `answeredQuestions`, `toolActivity[]`, `toolUseBlocks[]`, `thinkingBlocks[]`, `attachments[]`, and `isCompactSummary` alongside `text`.

`ToolUseBlock` carries an optional `subagentTrace?: SubagentTrace` for subagent results from `chainBuilder.ts`.

### History replay (`src/lib/chainBuilder.ts`)

`buildMessages(entries: SessionEntry[])` converts NDJSON entries from `GET /api/tasks/:id/history` into the same `Message[]` shape used by live SSE. All resulting messages have `status: 'done'`.

### API client (`src/api/client.ts`)

Base URL from `VITE_API_BASE` (default `''` — Vite proxy handles `/api/*` in dev). All auth calls include `Authorization: Bearer <token>` from `auth.ts`. `sendMessage` returns the raw `Response` for SSE streaming; all other functions return parsed data.

### Path alias

`@/*` → `src/*` (configured in `tsconfig.app.json` and `vite.config.ts`).
