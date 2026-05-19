# Session Store Interface

## Goal

Replace the two-step `ListHistory` + `GetHistory(key)` loop in `GetTaskHistory` with a
`SessionStore` interface whose single `GetHistory(ctx, username, taskID)` method
encapsulates full history retrieval. The OFS/S3-backed client becomes one implementation;
future implementations (local FS, in-memory mock) can be added without touching handlers.

---

## Current State

`GetTaskHistory` (`internal/api/handlers_tasks.go:453`) couples the handler to the OFS
key-addressing scheme:

```go
keys, _ := h.fileStore.ListHistory(ctx, username, id)   // returns key prefixes
for _, key := range keys {
    part, _ := h.fileStore.GetHistory(ctx, key)          // fetches one prefix at a time
    entries = append(entries, part...)
}
```

`FileStore` (in `internal/api/interfaces.go`) mirrors `storage.OFSClient` at the same
low-level granularity — callers must know about the two-pass prefix listing:

```go
type FileStore interface {
    ListHistory(ctx, username, taskID string) ([]string, error)
    GetHistory(ctx, key string) ([]json.RawMessage, error)
    GetSessionMeta(ctx, username, taskID string) (*storage.SessionMeta, error)
    DeleteHistory(ctx, username, taskID string) error
}
```

---

## Target State

### New package: `internal/session/`

```
internal/session/
  session.go      — SessionStore interface
  ofs.go          — OFSSessionStore (wraps storage.OFSClient)
```

**`session.go`**

```go
package session

import (
    "context"
    "encoding/json"
)

// SessionStore retrieves conversation history for a task.
// Implementations hide storage addressing details from callers.
type SessionStore interface {
    GetHistory(ctx context.Context, username, taskID string) ([]json.RawMessage, error)
}
```

**`ofs.go`**

```go
package session

import (
    "context"
    "encoding/json"

    "github.com/l-lab/cloud-agents/internal/storage"
)

// OFSSessionStore implements SessionStore backed by an OFS S3 client.
type OFSSessionStore struct {
    client storage.OFSClient
}

func NewOFSSessionStore(client storage.OFSClient) *OFSSessionStore {
    return &OFSSessionStore{client: client}
}

func (s *OFSSessionStore) GetHistory(ctx context.Context, username, taskID string) ([]json.RawMessage, error) {
    keys, err := s.client.ListHistory(ctx, username, taskID)
    if err != nil {
        return nil, err
    }
    var entries []json.RawMessage
    for _, key := range keys {
        part, err := s.client.GetHistory(ctx, key)
        if err != nil {
            return nil, err
        }
        entries = append(entries, part...)
    }
    return entries, nil
}
```

### Updated `TaskHandler`

Add `sessionStore session.SessionStore` alongside the existing `fileStore FileStore`.
`fileStore` stays for `GetSessionMeta` and `DeleteHistory` (used by other handlers).

```go
type TaskHandler struct {
    store        TaskStore
    sandbox      SandboxManager
    proxy        MessageProxy
    fileStore    FileStore         // GetSessionMeta, DeleteHistory
    sessionStore session.SessionStore  // GetHistory
    ...
}
```

### Updated `GetTaskHistory`

```go
if h.sessionStore == nil {
    c.String(http.StatusServiceUnavailable, "history storage not configured")
    return
}
entries, err := h.sessionStore.GetHistory(c.Request.Context(), t.Username, id)
if err != nil {
    c.String(http.StatusInternalServerError, "failed to get history")
    return
}
c.JSON(http.StatusOK, entries)
```

---

## Migration Steps

### 1. Create `internal/session/session.go`
Define the `SessionStore` interface (see above).

### 2. Create `internal/session/ofs.go`
Define `OFSSessionStore` with `NewOFSSessionStore` constructor (see above).
Move the `ListHistory` + loop logic from the handler here.

### 3. Update `internal/api/interfaces.go`
No interface changes needed — `SessionStore` lives in `internal/session`.
`FileStore` remains unchanged for the other operations it supports.

### 4. Update `TaskHandler` struct and constructor
- Add `sessionStore session.SessionStore` field.
- Add it as a parameter to `NewTaskHandler` (or wire via `RouterDeps`).

### 5. Update `internal/api/router.go` / `RouterDeps`
- Add `SessionStore session.SessionStore` to `RouterDeps`.
- Pass `deps.SessionStore` when constructing `TaskHandler`.

### 6. Update `cmd/server/main.go`
After constructing `storage.Client`, wrap it:
```go
sessionStore := session.NewOFSSessionStore(storageClient)
```
Inject into `RouterDeps`.

### 7. Update `GetTaskHistory`
Replace the `ListHistory` + loop with a single `h.sessionStore.GetHistory(...)` call.

### 8. Update tests
- `internal/api/smoke_test.go` or any handler tests: supply a stub `SessionStore`
  (e.g. `session.NewOFSSessionStore(mockOFSClient)` or a hand-written mock).
- Add a unit test for `OFSSessionStore.GetHistory` in `internal/session/ofs_test.go`.

---

## What Does NOT Change

- `FileStore` interface and its usage for `GetSessionMeta`, `DeleteHistory`.
- `storage.OFSClient` and `storage.Client` — `OFSSessionStore` delegates to them.
- Frontend API (`GET /api/tasks/:id/history`) — response shape is identical.
- Task state machine, sandbox lifecycle, all other handlers.

---

## Non-goals

- Typed session entries (still `[]json.RawMessage`) — a future follow-up if needed.
- Caching or streaming — out of scope.
- Migrating `DeleteHistory` or `GetSessionMeta` to `SessionStore` — they have different
  semantics and are only called by other handlers; leave them in `FileStore`.
