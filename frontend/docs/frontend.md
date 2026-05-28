# Frontend

Vite + React + TypeScript chat interface. Talks to the Go backend at `:8091` via a Vite dev proxy — no CORS configuration needed.

## Running

```bash
cd frontend
npm run dev      # http://localhost:5173
npm run build    # type-check + production bundle
npm run lint     # ESLint
```

## Stack

| Layer      | Choice                                |
| ---------- | ------------------------------------- |
| Bundler    | Vite 6                                |
| UI         | React 19 + TypeScript                 |
| Styling    | Tailwind CSS v4 (`@tailwindcss/vite`) |
| Components | shadcn/ui (neutral palette)           |
| Icons      | lucide-react                          |
| Markdown   | react-markdown                        |

Font: Inter (loaded from `rsms.me/inter` CDN, applied via `@theme { --font-sans }` in `index.css`).

## File structure

```
src/
├── api/
│   └── client.ts                # fetch wrappers for the backend REST API
├── components/
│   ├── ui/                      # shadcn primitives (Button, Textarea, ScrollArea, Badge, Switch, Tabs, Dialog, Input)
│   ├── ChatMessage.tsx          # single message bubble
│   ├── ChatInput.tsx            # auto-resizing textarea + send button
│   ├── HistorySidepanel.tsx     # left sidebar: task list + new-chat button
│   ├── NewTaskDialog.tsx        # dialog for creating a new task (with title/git URL options)
│   ├── ProtectedRoute.tsx       # redirects to /login if no valid JWT
│   ├── ResourceForm.tsx         # shared create/edit form for skill and MCP resources
│   ├── StatusBadge.tsx          # sandbox connection indicator
│   └── WorkspacePanel.tsx       # right panel: workspace file browser
├── hooks/
│   └── useChat.ts               # all state + SSE streaming logic
├── lib/
│   ├── auth.ts                  # JWT token management (localStorage)
│   ├── chainBuilder.ts          # convert SessionEntry[] → Message[] for history replay
│   ├── cron.ts                  # human-readable cron expression descriptions (cronstrue)
│   └── utils.ts                 # cn() helper (clsx + tailwind-merge)
├── pages/
│   ├── ChatPage.tsx             # three-column resizable layout: sidebar | chat | workspace
│   ├── LoginPage.tsx            # SSO / OIDC / password login buttons
│   ├── ResourcesPage.tsx        # CRUD for skill and MCP resources (tabbed)
│   ├── ScheduleDetailPage.tsx   # schedule detail + run history
│   ├── ScheduleFormPage.tsx     # create/edit form for schedules
│   ├── SchedulesPage.tsx        # list of schedules with enable/disable toggle
│   ├── SettingsPage.tsx         # user settings: SSH key + Anthropic API key management
│   └── SSOCallbackPage.tsx      # reads #access_token= from URL fragment (SSO + OIDC)
├── types/
│   └── session.ts               # typed SessionEntry union from claude-agent-sdk
├── types.ts                     # shared TypeScript types (Message, ToolUseBlock, …)
├── App.tsx                      # router setup and route definitions
├── index.css                    # Tailwind import + Inter @theme
└── main.tsx
```

## API client (`src/api/client.ts`)

Thin fetch wrappers. Base URL comes from `VITE_API_BASE` (defaults to `''`, so the Vite proxy handles routing in dev).

**Auth**

| Function                                  | Description                                                         |
| ----------------------------------------- | ------------------------------------------------------------------- |
| `getRuntimeConfig()`                      | `GET /api/runtime-config` → `RuntimeConfig` (login mode flags)      |
| `loginWithPassword(username, password)`   | `POST /api/auth/login` → access token string                        |
| `register(username, password, email?)`    | `POST /api/auth/register` → access token string                     |

**Tasks**

| Function                                              | Description                                                            |
| ----------------------------------------------------- | ---------------------------------------------------------------------- |
| `listTasks()`                                         | `GET /api/tasks` → `TaskSummary[]` (newest first)                      |
| `createTask(username, options?)`                      | `POST /api/tasks` → task `id`; options: `{ title, gitUrl, env }`       |
| `getTask(taskId)`                                     | `GET /api/tasks/:id` → `Task`                                          |
| `sendMessage(taskId, prompt, files?, permissionMode?)` | `POST /api/tasks/:id/messages` → raw `Response` for SSE reading       |
| `steerMessage(taskId, prompt, priority?)`             | `POST /api/tasks/:id/steer` → void (injects prompt into active run)    |
| `getHistory(taskId, cursor?)`                         | `GET /api/tasks/:id/history` → `HistoryPage { entries, nextCursor }`   |
| `deleteTask(taskId)`                                  | `DELETE /api/tasks/:id`                                                |
| `respondToPermission(taskId, decision)`               | `POST /api/tasks/:id/permissions`                                      |
| `respondToQuestion(taskId, answers)`                  | `POST /api/tasks/:id/questions`                                        |

`sendMessage` supports both JSON and multipart (when `files` are provided). It returns the raw `Response` for SSE streaming.

`steerMessage` accepts an optional `priority` (`'now' | 'next' | 'later'`). `useChat` hardcodes `'now'` when routing steered messages from `sendMessage`.

`getHistory` returns paginated results. Pass `cursor` from the previous page's `nextCursor` to fetch older entries.

**`TaskSummary`** (returned by `listTasks`):

| Field         | Type              | Description                                           |
| ------------- | ----------------- | ----------------------------------------------------- |
| `id`          | `string`          | Task UUID                                             |
| `title`       | `string`          | Display title                                         |
| `state`       | `string`          | Derived sandbox state label                           |
| `git_url`     | `string?`         | Git repo URL if task was cloned from git              |
| `error_msg`   | `string?`         | Error message when state is `error`                   |
| `schedule_id` | `string?`         | UUID of the parent schedule (scheduled tasks only)    |
| `created_at`  | `string`          | ISO 8601 timestamp                                    |
| `updated_at`  | `string`          | ISO 8601 timestamp                                    |

`schedule_id` is used by `HistorySidepanel` to show a calendar icon on schedule-triggered tasks (when `git_url` is not also set).

**Resources**

| Function                             | Description                                                     |
| ------------------------------------ | --------------------------------------------------------------- |
| `listResources()`                    | `GET /api/resources` → `Resource[]`                             |
| `createResource(payload)`            | `POST /api/resources` → `Resource`                              |
| `createSkillFromZip(name, file)`     | `POST /api/resources/zip` → `Resource` (multipart ZIP upload)   |
| `getSkillContent(id)`                | `GET /api/resources/:id/content` → skill SKILL.md text          |
| `updateResource(id, payload)`        | `PUT /api/resources/:id` → `Resource`                           |
| `deleteResource(id)`                 | `DELETE /api/resources/:id`                                     |

**Workspace**

| Function                       | Description                                              |
| ------------------------------ | -------------------------------------------------------- |
| `listDir(taskId, dir)`         | `GET /api/tasks/:id/workspace/files?path=…` → `FileInfo[]` |
| `readFile(taskId, filePath)`   | `GET /api/tasks/:id/workspace/file?path=…` → text        |

**User settings**

| Function                         | Description                                                        |
| -------------------------------- | ------------------------------------------------------------------ |
| `getUserSettings()`              | `GET /api/user/settings` → `{ has_ssh_key, has_anthropic_key }`    |
| `updateUserSettings(body)`       | `PUT /api/user/settings`; body: `{ ssh_private_key?, anthropic_api_key? }` |

**Schedules**

| Function                          | Description                                          |
| --------------------------------- | ---------------------------------------------------- |
| `listSchedules()`                 | `GET /api/schedules` → `Schedule[]`                  |
| `createSchedule(payload)`         | `POST /api/schedules` → `Schedule`                   |
| `getSchedule(id)`                 | `GET /api/schedules/:id` → `Schedule`                |
| `updateSchedule(id, payload)`     | `PUT /api/schedules/:id` → `Schedule`                |
| `deleteSchedule(id)`              | `DELETE /api/schedules/:id`                          |
| `enableSchedule(id)`              | `POST /api/schedules/:id/enable`                     |
| `disableSchedule(id)`             | `POST /api/schedules/:id/disable`                    |
| `runScheduleNow(id)`              | `POST /api/schedules/:id/run` → `{ task_id }`        |
| `listScheduleRuns(id)`            | `GET /api/schedules/:id/runs` → `ScheduleRun[]`      |
| `generateScheduleToken(id)`       | `POST /api/schedules/:id/tokens` → `ScheduleTokenInfo` |
| `revokeScheduleToken(id)`         | `DELETE /api/schedules/:id/tokens`                   |

## State & SSE (`src/hooks/useChat.ts`)

`useChat(username, onSessionCompleted?)` is the single source of truth for all chat state.

```ts
const {
  messages, taskId, cwd, sandboxState, sending,
  hasMoreHistory, loadingMoreHistory,
  sendMessage, approvePermission, answerQuestion,
  newChat, loadTask, loadMoreHistory, startTask,
} = useChat(username, onSessionCompleted)
```

| Value                                          | Type                                                         | Description                                              |
| ---------------------------------------------- | ------------------------------------------------------------ | -------------------------------------------------------- |
| `messages`                                     | `Message[]`                                                  | Full conversation history                                |
| `taskId`                                       | `string \| null`                                             | Current task ID; null for a fresh chat                   |
| `cwd`                                          | `string \| null`                                             | Working directory reported by the most recent `session.init` |
| `sandboxState`                                 | `SandboxState`                                               | `idle \| provisioning \| running \| error`               |
| `sending`                                      | `boolean`                                                    | True while a message is in-flight                        |
| `hasMoreHistory`                               | `boolean`                                                    | True when there are older history pages to load          |
| `loadingMoreHistory`                           | `boolean`                                                    | True while an older-history fetch is in progress         |
| `sendMessage(prompt, files?, permissionMode?)` | function                                                     | Send a message; steers if agent is already running       |
| `loadTask(id, messages, cwd?, cursor?)`        | function                                                     | Load history from a previous task and make it resumable  |
| `loadMoreHistory()`                            | `() => Promise<void>`                                        | Fetch the next page of older history (prepends messages) |
| `startTask(tid)`                               | `(string) => void`                                           | Activate a pre-created task without loading history      |
| `newChat()`                                    | `() => void`                                                 | Reset to an empty fresh chat                             |
| `approvePermission(approved)`                  | `(boolean) => void`                                          | Respond to a pending tool permission                     |
| `answerQuestion(answers)`                      | `(Record) => void`                                           | Submit answers to a pending question                     |

### `sendMessage` flow

1. If `sending === true` (agent already running), routes directly to `steerMessage` and returns.
2. If `taskId` is null, calls `createTask(username)` and stores the id.
3. Appends the user message and an empty assistant message (status `streaming`) to the list.
4. Sets `sandboxState` to `provisioning` (backend may need to cold-start the sandbox).
5. POSTs to the backend (JSON or multipart) and reads the SSE stream via `parseSSE`.

### SSE event handling

| Event                   | Action                                                                   |
| ----------------------- | ------------------------------------------------------------------------ |
| `session.init`          | Sets `sandboxState → 'running'`; stores `cwd`; on steer creates a new assistant bubble |
| `message.assistant`     | Appends `data.text` delta; collects `tool_use` blocks                    |
| `message.user`          | Reads `tool_result` blocks from `data.message.content`; matches by `tool_use_id` and sets `ToolUseBlock.result` on the active message |
| `permission.requested`  | Sets `status: 'requesting'`, attaches `permissionRequest` to message     |
| `question.asked`        | Sets `status: 'asking'`, attaches `pendingQuestions` to message          |
| `session.status` (idle) | Sets `status: 'done'`                                                    |
| `task.started`          | Pushes a new `ToolActivity{done: false}`                                 |
| `task.progress`         | Updates the last `ToolActivity` description (`data.description`) + tool name (`data.lastToolName`) |
| `result`                | Sets `status: 'done'`; aborted empty runs with no content are removed    |
| `session.completed`     | Marks all tool activities `done`, clears `sending`, calls `onSessionCompleted` |
| `error`                 | Sets `status: 'error'`, `sandboxState → 'error'`, clears `sending`      |

`session.completed` (not `result`) is the terminal event that re-enables input.

### SSE parser

`parseSSE(response)` is an async generator that reads `response.body` as a `ReadableStream`, decodes chunks, and yields `{ event, data }` pairs on each complete `event:` / `data:` block. Partial chunks are buffered across reads.

## History replay (`src/lib/chainBuilder.ts`)

`buildMessages(entries: SessionEntry[]): Message[]` converts the raw NDJSON entries returned by `GET /api/tasks/:id/history` into the same `Message[]` shape that the live SSE stream produces.

Only `user` and `assistant` entries are processed:
- **User entries** — content is extracted from `message.content` (string or `TextBlockParam[]`).
- **Assistant entries** — text is extracted from `BetaTextBlock` blocks; `BetaToolUseBlock` blocks become `ToolUseBlock[]` on the message.

All resulting messages have `status: 'done'` so they render identically to completed live messages.

## Session types (`src/types/session.ts`)

Typed union for every entry type that can appear in a `.jsonl` history file. Uses the `@anthropic-ai/claude-agent-sdk` package (devDependency) to reuse the message payload types (`SDKAssistantMessage`, `SDKUserMessage`, etc.) rather than duplicating them.

Key types exported:
- `SessionEntry` — full union of all entry types
- `VisibleEntry` — `UserEntry | AssistantEntry` (shown in chat)
- `ConversationEntry` — entries with `uuid` (participate in chain building)

## Routes

| Path                  | Component                        | Auth      |
| --------------------- | -------------------------------- | --------- |
| `/`                   | `ChatPage`                       | Protected |
| `/resources`          | `ResourcesPage`                  | Protected |
| `/settings`           | `SettingsPage`                   | Protected |
| `/schedules`          | `SchedulesPage`                  | Protected |
| `/schedules/new`      | `ScheduleFormPage mode="create"` | Protected |
| `/schedules/:id`      | `ScheduleDetailPage`             | Protected |
| `/schedules/:id/edit` | `ScheduleFormPage mode="edit"`   | Protected |
| `/login`              | `LoginPage`                      | Public    |
| `/login/sso`          | `SSOCallbackPage`                | Public    |
| `/login/oidc`         | `SSOCallbackPage`                | Public    |

## Components

### `ChatPage` (`src/pages/ChatPage.tsx`)

Three-column resizable layout filling `100dvh`. Sidebar (left) and workspace panel (right) widths are draggable (160–480 px). Workspace panel only shows when `workspaceOpen && taskId && cwd`.

```
┌──────────────┬──────────────────────┬──────────────┐
│  History     │  [☰] | StatusBadge   │  Workspace   │
│  ──────────  ├──────────────────────┤  file tree   │
│  Task A      │  ScrollArea (flex-1) │              │
│  Task B      │    messages          │              │
│  …           ├──────────────────────┤              │
│  [✏ New]     │  ChatInput           │              │
└──────────────┴──────────────────────┴──────────────┘
```

On mount, `listTasks()` populates the sidebar. Clicking a task calls `getHistory(id)` → `buildMessages(entries)` → `loadTask(id, messages, cwd)`, loading its history. `refreshToken` is incremented on `session.completed` to trigger workspace refresh.

### `SettingsPage` (`src/pages/SettingsPage.tsx`)

User settings: SSH private key and Anthropic API key storage. Both are encrypted server-side and never returned in plaintext.

### `SchedulesPage` (`src/pages/SchedulesPage.tsx`)

Lists all user schedules with enabled/disabled toggle. Links to the create form and schedule detail pages.

### `ScheduleFormPage` (`src/pages/ScheduleFormPage.tsx`)

Create or edit a schedule. Supports recurring cron expressions and one-time `run_at` timestamps. Optional `git_url` and `extra_env` fields.

### `ScheduleDetailPage` (`src/pages/ScheduleDetailPage.tsx`)

Shows schedule details + paginated run history with status badges. Each run links back to its ChatPage task.

### `ChatMessage` (`src/components/ChatMessage.tsx`)

Renders one message bubble.

- **User** — right-aligned, dark neutral bubble, plain text (`whitespace-pre-wrap`).
- **Assistant** — left-aligned, light neutral bubble, rendered as Markdown via `react-markdown`. Appends a blinking cursor while `status === 'streaming'`.
- **Error** — red tint + `AlertCircle` icon regardless of role.
- **Tool activity** — shown below the assistant text behind a toggle button (`ChevronDown` icon + count label). When expanded, each item has a pulsing neutral dot while `done === false`, green dot when complete, with tool name bolded before the description.

### `ChatInput` (`src/components/ChatInput.tsx`)

Textarea that auto-resizes up to ~6 lines. Submits on `Enter`; `Shift+Enter` inserts a newline. Both the send button and Enter submit are disabled while `sending === true` or the input is empty/whitespace-only.

### `HistorySidepanel` (`src/components/HistorySidepanel.tsx`)

Left sidebar showing the authenticated user's task history. Props:

| Prop           | Type                   | Description                                         |
| -------------- | ---------------------- | --------------------------------------------------- |
| `tasks`        | `TaskSummary[]`        | List from `listTasks()`                             |
| `activeTaskId` | `string \| null`       | Highlights the currently loaded task                |
| `onSelectTask` | `(id: string) => void` | Called when a task row is clicked                   |
| `onNewChat`    | `() => void`           | Called when the pencil (new-chat) button is clicked |

Dates are formatted as time (`10:30 AM`) for today, `Yesterday` for yesterday, and `Month Day` for older entries.

### `StatusBadge` (`src/components/StatusBadge.tsx`)

Renders nothing when `idle`. Otherwise shows a colored dot + label:

| State          | Dot             | Label               |
| -------------- | --------------- | ------------------- |
| `provisioning` | pulsing neutral | Starting workspace… |
| `running`      | green           | Connected           |
| `error`        | red             | Connection error    |

## Configuration

### Vite proxy (`vite.config.ts`)

```ts
server: {
  proxy: {
    '/api': { target: 'http://localhost:8091', changeOrigin: true }
  }
}
```

All `/api/*` requests from the browser are forwarded to the Go backend. `VITE_API_BASE` can be left unset in dev.

### Environment variables

| Variable        | Default | Description                                                                                                               |
| --------------- | ------- | ------------------------------------------------------------------------------------------------------------------------- |
| `VITE_API_BASE` | `''`    | Backend base URL. Leave empty when using the Vite proxy. Set to `http://localhost:8091` for environments without a proxy. |

### Path alias

`@/*` maps to `src/*`, configured in `tsconfig.app.json` and `vite.config.ts`.

## Tooling

- **TypeScript** — strict mode, `moduleResolution: bundler`, `noUncheckedSideEffectImports`.
- **ESLint** — flat config (`eslint.config.js`) with `typescript-eslint`, `eslint-plugin-react-hooks`, `eslint-plugin-react-refresh`. The `react-refresh` export warning is suppressed for `src/components/ui/` (shadcn components intentionally export both component and variant helpers).
- **shadcn** — configured in `components.json` with neutral base color and `cssVariables: true`. Add new components with `npx shadcn@latest add <name>`.
