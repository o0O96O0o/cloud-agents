# OrangeFS Session File Storage Specification

## Overview

OrangeFS (OFS) is an S3-compatible cloud file store that persists session history and agent workspace files across ephemeral sandbox restarts. There are two distinct access paths:

| Access path | Who uses it | What is stored |
|---|---|---|
| **FUSE mount** (`orangefs posix mount`) | Sandbox entrypoint | Agent workspace files; OFS subpath `{username}/workspaces/{task_id}` → mounted at `/workspace/{username}/{task_id}/` |
| **S3 API** (`ORANGEFS_ENDPOINT`) | Agent server (write), backend (read) | Session history NDJSON parts and process records |

**Key identity mapping:**

| Identifier | Source | Role |
|---|---|---|
| `task_id` | Backend UUID (assigned at `POST /api/tasks`) | Workspace subpath key and encoded CWD segment |
| `username` | Request body of `POST /api/tasks` | Top-level OFS namespace; prefixes all S3 keys |
| `sandbox_id` | OpenSandbox container ID | Ephemeral — can be destroyed and recreated |
| Agent `sessionId` | Emitted in SSE `session.init` event | S3 key segment for history parts; stored in session record |

The FUSE mount is set up at **sandbox creation time**, before any agent session exists. Session history is written by the agent server over the S3 API during the session, and remains readable after the sandbox is destroyed.

---

## OFS Configuration

Credentials live in `config.yaml` under `orangefs` and are loaded into `OrangeFSConfig` (`pkg/config/config.go`):

```yaml
orangefs:
  addr: "10.88.151.122:8030"                   # FUSE: registry server address → ORANGEFS_RS_ADDR
  token: "..."                                  # FUSE: auth token → ORANGEFS_TOKEN
  endpoint: "https://s3-yspu.didistatic.com"   # S3: public endpoint for backend + agent server
  volume: "ofs-llab-lflow"                     # S3 bucket name; also used as FUSE volume
  access_key: "..."
  secret_key: "..."
```

| Config field | Injected as | Used by |
|---|---|---|
| `addr` | `ORANGEFS_RS_ADDR` | Sandbox entrypoint — FUSE mount |
| `token` | `ORANGEFS_TOKEN` | Sandbox entrypoint — FUSE mount auth |
| `volume` | `ORANGEFS_VOLUME` | Sandbox entrypoint (FUSE) + agent server (S3 bucket) |
| `endpoint` | `ORANGEFS_ENDPOINT` | Agent server (write history) + backend client (read history) |
| `access_key` | `S3_ACCESS_KEY` | Agent server + backend client |
| `secret_key` | `S3_SECRET_KEY` | Agent server + backend client |

The backend never mounts OFS itself; FUSE mounting is handled by the entrypoint inside the sandbox.

---

## Environment Variables Injected at Sandbox Creation

`manager.ProvisionForTask` (`internal/sandbox/manager.go`) injects these into every sandbox via `baseEnv` (set in `cmd/server/main.go`):

| Variable | Value | Purpose |
|---|---|---|
| `ORANGEFS_RS_ADDR` | `orangefs.addr` | OFS resource server for FUSE mount |
| `ORANGEFS_TOKEN` | `orangefs.token` | Auth token for FUSE mount |
| `ORANGEFS_VOLUME` | `orangefs.volume` | Volume/bucket for FUSE mount and S3 writes |
| `ORANGEFS_ENDPOINT` | `orangefs.endpoint` | S3 endpoint for agent server session storage |
| `S3_ACCESS_KEY` | `orangefs.access_key` | S3 credentials for agent server |
| `S3_SECRET_KEY` | `orangefs.secret_key` | S3 credentials for agent server |
| `USERNAME` | `task.Username` | Sets workspace path and S3 key namespace |
| `TASK_ID` | `task.ID` | Sets workspace subpath; encoded into S3 history key |

The entrypoint mounts the OFS subpath `{USERNAME}/workspaces/{TASK_ID}` at:
```
/workspace/{USERNAME}/{TASK_ID}/    ← agent workspace files (FUSE)
```

---

## S3 Storage Layout

Session history and process records are stored in OFS via the S3 API. The top-level namespace is `{username}/`.

```
{username}/                                     # top-level per-user prefix
├── workspaces/
│   └── {task_id}/                              # FUSE-mounted at /workspace/{username}/{task_id}/
│       └── ...                                 # agent project files (not S3-accessible from backend)
├── history/
│   └── {encoded_cwd}/                          # CWD path with '/' replaced by '-'
│       └── {session_id}/                       # agent session UUID
│           └── part-{13ms}-{rand}.ndjson       # JSONL conversation parts
├── .claude/
│   └── sessions/
│       └── {pid}.json                          # agent process record
└── resources/                                  # user-registered resources (written by backend)
    ├── skills/
    │   └── {name}/
    │       └── SKILL.md                        # skill markdown; injected by manager at provision
    └── mcp/
        └── {name}.json                         # MCP server config JSON; injected into .mcp.json
```

### Working Directory Encoding

CWD is `/workspace/{username}/{task_id}`. Each `/` is replaced by `-` (leading `/` included):

```
/workspace/alice/task-abc  →  -workspace-alice-task-abc
```

### Part File Naming

Each part file is named `part-{epochMs13}-{rand6}.ndjson`. The 13-digit millisecond epoch prefix makes lexicographic sort equivalent to chronological order. A single session may have multiple part files; all are concatenated in order to reconstruct the full history.

---

## File Formats

### `{username}/.claude/sessions/{pid}.json`

One JSON object per file. Records agent process metadata.

```json
{
  "pid": 62137,
  "sessionId": "86613a9f-11bf-4a23-b32a-6c5cf76d2cee",
  "cwd": "/workspace/alice/task-abc",
  "startedAt": 1778307176338,
  "status": "busy",
  "updatedAt": 1778315027087,
  "version": "2.1.119"
}
```

### `{username}/history/{encoded_cwd}/{session_id}/part-*.ndjson`

NDJSON — one JSON object per line. Entry types:

| `type` | Description |
|--------|-------------|
| `user` | User turn: `message.role = "user"`, `uuid`, `parentUuid`, `timestamp` |
| `assistant` | Assistant turn: `message.model`, `message.content[]` (text / thinking / tool blocks) |
| `system` | System prompt snapshot |
| `attachment` | File attachment metadata |
| `last-prompt` | Most recent raw user prompt (used for resume) |

Entries with `isMeta: true` are internal bookkeeping and are excluded by the storage client.

Sample `user` entry:

```json
{
  "type": "user",
  "uuid": "0ab269f4-1046-4981-b3c7-010ac62f81d0",
  "parentUuid": null,
  "isMeta": false,
  "timestamp": "2026-05-09T06:12:42.697Z",
  "message": {
    "role": "user",
    "content": "Hello"
  }
}
```

---

## Storage Client

Implemented in `internal/storage/client.go` using `aws-sdk-go-v2`. Initialised in `cmd/server/main.go` from `orangefs.endpoint`, `access_key`, `secret_key`.

### Interface

```go
// OFSClient is the read-only interface used by handlers and history APIs.
type OFSClient interface {
    // ListHistory returns session prefixes for the given task.
    // Each prefix is "{username}/history/{encoded_cwd}/{session_id}/".
    // Used internally; callers should prefer GetHistoryPage.
    ListHistory(ctx context.Context, username, taskID string) ([]string, error)

    // GetHistory downloads all part files under a session prefix and returns
    // their NDJSON entries. Entries with isMeta:true are excluded.
    GetHistory(ctx context.Context, sessionPrefix string) ([]json.RawMessage, error)

    // GetHistoryPage returns entries for one session and the cursor for the
    // next-older session. cursor="" requests the latest (most recent) session.
    // nextCursor="" means no more history is available.
    // Sessions are ranked newest-first by their highest part-file epoch timestamp.
    GetHistoryPage(ctx context.Context, username, taskID, cursor string) (entries []json.RawMessage, nextCursor string, err error)

    // GetSessionMeta returns the agent process record for the given task by
    // scanning "{username}/.claude/sessions/" and matching on CWD.
    GetSessionMeta(ctx context.Context, username, taskID string) (*SessionMeta, error)
}
```

`*storage.Client` also exposes two write methods used by the resource subsystem, but **not** declared on `OFSClient` (they are accessed via narrow interfaces to avoid widening the read-only contract):

```go
// PutObject writes data to an arbitrary S3 key in the OFS volume.
// Used by the resource API handler (api.ResourceWriter interface).
func (c *Client) PutObject(ctx context.Context, key string, data []byte) error

// GetObjectBytes downloads a single S3 object and returns its raw bytes.
// Used by the sandbox manager (sandbox.ofsReader interface).
func (c *Client) GetObjectBytes(ctx context.Context, key string) ([]byte, error)
```

### SessionStore abstraction

`internal/session.SessionStore` wraps `OFSClient` and hides addressing details from API handlers:

```go
type SessionStore interface {
    GetHistory(ctx context.Context, username, taskID, cursor string) (entries []json.RawMessage, nextCursor string, err error)
}
```

`OFSSessionStore` is the production implementation. `GET /api/tasks/:id/history` calls `SessionStore.GetHistory` — it never touches `OFSClient` directly.

### Retrieving History for a Task (paginated)

```go
// First page — newest turn (2 part files)
entries, nextCursor, err := sessionStore.GetHistory(ctx, username, taskID, "")

// Subsequent pages — older turns
for nextCursor != "" {
    entries, nextCursor, err = sessionStore.GetHistory(ctx, username, taskID, nextCursor)
}
```

The HTTP API exposes this via `GET /api/tasks/:id/history?cursor=<cursor>`:

```json
{
  "entries": [ ... ],
  "nextCursor": "alice/history/-workspace-alice-task-abc/86613a9f-.../part-1778587171934-af0c9c.ndjson"
  // nextCursor is "" when no older files exist
}
```

`GetHistoryPage` implementation: one `ListObjectsV2` pass collects all part-file keys (metadata only, no downloads). Keys are sorted lexicographically (= chronological order). The newest `historyPageSize` (2) keys are sliced; if a cursor is given, the 2 keys strictly older than that key are returned instead. Only the keys in the window are downloaded. The cursor is the S3 key of the **oldest** part file in the current page.

Since tasks always have a single agent session, and the SDK flushes exactly 2 part files per user turn (one content file, one `last-prompt` meta file), a page of 2 part files = 1 conversation turn.

---

## S3 Key Reference

| Object | S3 Key | Writer |
|--------|--------|--------|
| Agent process record | `{username}/.claude/sessions/{pid}.json` | Agent server |
| Session history parts | `{username}/history/{encoded_cwd}/{session_id}/part-{13ms}-{rand}.ndjson` | Agent server |
| Skill content | `{username}/resources/skills/{name}/SKILL.md` | Backend (`PutObject`) |
| MCP config | `{username}/resources/mcp/{name}.json` | Backend (`PutObject`) |
| Agent workspace files | FUSE-mounted; not accessible via S3 from backend | Entrypoint / agent |

`{encoded_cwd}` = CWD with every `/` replaced by `-`, e.g. `-workspace-{username}-{task_id}`.

`{username}` and `{task_id}` come from the backend `Task` record. The agent `sessionId` is embedded in the S3 key path and extracted by `GetHistoryPage` during session ranking. `GetSessionMeta` (process record JSON) is an alternative lookup used by non-history paths.
