# OrangeFS Session File Storage Specification

## Overview

OrangeFS (OFS) is an S3-compatible cloud file store that persists session history and agent workspace files across ephemeral sandbox restarts. There are two distinct access paths:

| Access path | Who uses it | What is stored |
|---|---|---|
| **FUSE mount** (`orangefs posix mount`) | Sandbox entrypoint | Agent workspace files at `/workspace/{username}/{task_id}/` |
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

The entrypoint mounts:
```
/workspace/{USERNAME}/{TASK_ID}/    ← agent workspace files (FUSE)
```

---

## S3 Storage Layout

Session history and process records are stored in OFS via the S3 API. The top-level namespace is `{username}/`.

```
{username}/                                     # top-level per-user prefix
├── history/
│   └── {encoded_cwd}/                          # CWD path with '/' replaced by '-'
│       └── {session_id}/                       # agent session UUID
│           └── part-{13ms}-{rand}.ndjson       # JSONL conversation parts
└── .claude/
    └── sessions/
        └── {pid}.json                          # agent process record
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
type OFSClient interface {
    // ListHistory returns session prefixes for the given task.
    // Each prefix is "{username}/history/{encoded_cwd}/{session_id}/".
    ListHistory(ctx context.Context, username, taskID string) ([]string, error)

    // GetHistory downloads all part files under a session prefix and returns
    // their NDJSON entries. Entries with isMeta:true are excluded.
    GetHistory(ctx context.Context, sessionPrefix string) ([]json.RawMessage, error)

    // GetSessionMeta returns the agent process record for the given task by
    // scanning "{username}/.claude/sessions/" and matching on CWD.
    GetSessionMeta(ctx context.Context, username, taskID string) (*SessionMeta, error)
}
```

### Retrieving History for a Task

```go
keys, err := fileStore.ListHistory(ctx, t.Username, t.ID)
if err != nil || len(keys) == 0 {
    // OFS not configured, sandbox not yet started, or no history written
}
entries, err := fileStore.GetHistory(ctx, keys[0])
```

---

## S3 Key Reference

| Object | S3 Key |
|--------|--------|
| Agent process record | `{username}/.claude/sessions/{pid}.json` |
| Session history parts | `{username}/history/{encoded_cwd}/{session_id}/part-{13ms}-{rand}.ndjson` |
| Agent workspace files | FUSE-mounted; not accessible via S3 from backend |

`{encoded_cwd}` = CWD with every `/` replaced by `-`, e.g. `-workspace-{username}-{task_id}`.

`{username}` and `{task_id}` come from the backend `Task` record. The agent `sessionId` is discovered via `ListHistory` (extracted from the S3 key path) or `GetSessionMeta` (read from the process record JSON).
