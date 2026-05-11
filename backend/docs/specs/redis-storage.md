# Redis Storage

The backend uses Redis to persist task state across server restarts and share it across multiple backend instances.

## When Redis is active

Set `redis.url` in `config.yaml`:

```yaml
redis:
  url: "redis://localhost:6379"
```

When `redis.url` is empty, the backend falls back to an in-memory store that is lost on restart.

---

## Data model

### Task hash — `task:{task_id}`

Each task is stored as a single Redis hash. All fields are always present; empty string denotes null.

| Field | Type | Example | Notes |
|---|---|---|---|
| `id` | string | `"a1b2c3d4-..."` | UUID; immutable |
| `username` | string | `"alice"` | Task owner; immutable |
| `state` | int string | `"0"` | Sandbox liveness state (see below) |
| `sandbox_id` | string | `"sb-xyz"` or `""` | Cleared on sandbox destroy |
| `proxy_base_url` | string | `"https://.../proxy/3000"` or `""` | Cleared on sandbox destroy |
| `proxy_headers` | JSON string | `{"Authorization":"Bearer tok"}` or `"{}"` | Cleared on sandbox destroy |
| `session_id` | string | `"uuid-..."` or `""` | Write-once; never cleared once set |
| `extra_env` | JSON string | `{"KEY":"val"}` or `"null"` | Set at creation; immutable |
| `provisioned` | `"0"` or `"1"` | `"1"` | Whether a sandbox has been successfully started |

#### State values

| Value | Constant | Effective spec state |
|---|---|---|
| `"0"` | `StateNew` | `pending` (no session) / `paused` (session set) |
| `"1"` | `StateProvisioning` | `provisioning` / `resuming` |
| `"2"` | `StateRunning` | `idle` / `active` |
| `"3"` | `StateError` | `error` |

The API-visible state label (e.g. `"active"`) is derived by combining `state` and whether `session_id` is non-empty. See [resource-mapping.md](resource-mapping.md) for the full state table.

### Distributed lock — `task-lock:{task_id}`

A per-task lock key used to serialise provisioning and liveness-reset operations.

| Property | Value |
|---|---|
| Key | `task-lock:{task_id}` |
| Value | `{hostname}:{pid}` (lock holder identity) |
| Acquire | `SET task-lock:{id} {holder} NX PX 30000` |
| Release | Lua CAS: `if GET key == holder then DEL key end` |
| TTL | 30 seconds (auto-released on crash) |

The lock is acquired by two operations:

- **`EnsureProvisioned`** — prevents two instances from provisioning the same task simultaneously. Deadline: 30 s (full lock TTL).
- **`ResetIfExpired`** — prevents a liveness reset from racing with re-provisioning. Deadline: 5 s (short so a request is not blocked long if another instance holds the lock).

Retry uses exponential backoff: 50 ms → 100 ms → 200 ms → … capped at 5 s per attempt.

---

## Key operations

### Create task

```
HSET task:{id}
  id            {uuid}
  username      {username}
  state         0
  sandbox_id    ""
  proxy_base_url ""
  proxy_headers  "{}"
  session_id    ""
  extra_env     {JSON}
  provisioned   "0"
```

No TTL is set. Tasks persist until `DELETE /api/tasks/:id`.

### Get task

```
HGETALL task:{id}
```

Returns `nil` (empty map) when the key does not exist → 404.

### Delete task

```
DEL task:{id} task-lock:{id}
```

Both the data hash and the lock key are removed atomically.

### SetRunning (sandbox provisioned)

```
HSET task:{id}
  state          2
  sandbox_id     {sandbox_id}
  proxy_base_url {url}
  proxy_headers  {JSON}
```

Called from inside the `EnsureProvisioned` lock callback, after the sandbox is confirmed healthy.

### SetProvisioning / SetError

```
HSET task:{id} state 1   -- SetProvisioning
HSET task:{id} state 3   -- SetError
```

### SetSessionID (write-once, Lua)

```lua
if redis.call("HGET", "task:{id}", "session_id") == "" then
    redis.call("HSET", "task:{id}", "session_id", {session_id})
    return 1
end
return 0
```

Returns `1` if the value was written, `0` if it was already set. Enforces invariant #4 (session_id is never replaced once established) atomically across instances.

### EnsureProvisioned

```
-- 1. Acquire lock (NX, TTL 30s)
SET task-lock:{id} {holder} NX PX 30000

-- 2. Check if already provisioned
HGET task:{id} provisioned  -- skip fn() if "1"

-- 3. Run provisioning fn() [calls SetRunning → HSET state/sandbox_id/...]

-- 4. Verify state was persisted (guard against silent SetRunning failure)
HGET task:{id} state  -- must equal "2" (StateRunning)

-- 5. Commit
HSET task:{id} provisioned 1

-- 6. Release lock (Lua CAS)
```

If step 4 finds `state != "2"`, the operation returns an error without setting `provisioned=1`, so the next caller will retry provisioning.

### ResetIfExpired (sandbox liveness check)

```
-- 1. Acquire lock (NX, TTL 30s, deadline 5s)
SET task-lock:{id} {holder} NX PX 30000

-- 2. Short-circuit if not provisioned or sandbox_id is empty
HGET task:{id} provisioned
HGET task:{id} sandbox_id

-- 3. Call isAlive(sandbox_id)

-- 4. If not alive: clear sandbox fields
HSET task:{id}
  state          0
  sandbox_id     ""
  proxy_base_url ""
  proxy_headers  "{}"
  provisioned    "0"

-- 5. Release lock
```

`session_id` is intentionally not cleared — it must be retained for OFS history reads even when no sandbox is active.

---

## Lifecycle example

```
POST /api/tasks
  → HSET task:abc ... state=0 provisioned=0

POST /api/tasks/abc/messages  (first message)
  → HGETALL task:abc
  → SET task-lock:abc {holder} NX PX 30000      (acquire lock)
  → HGET task:abc provisioned  → "0"
  → provision sandbox → SetRunning
      → HSET task:abc state=2 sandbox_id=sb-1 proxy_base_url=... proxy_headers=...
  → HGET task:abc state → "2"  (guard passes)
  → HSET task:abc provisioned 1
  → DEL task-lock:abc                            (release lock)
  → proxy message to sandbox
  → [SSE session.init received] SetSessionID
      → Lua: if session_id == "" → HSET session_id=sess-xyz

GET /api/tasks/abc
  → HGETALL task:abc
  → state="2", session_id set → response: state="active"

[sandbox expires]

POST /api/tasks/abc/messages  (next message)
  → HGETALL task:abc
  → ResetIfExpired:
      → SET task-lock:abc {holder} NX PX 30000
      → HGET provisioned → "1"
      → HGET sandbox_id  → "sb-1"
      → isAlive(sb-1)    → false
      → HSET state=0 sandbox_id="" ... provisioned=0
      → DEL task-lock:abc
  → EnsureProvisioned: re-provision new sandbox → state=2, provisioned=1
  → session_id retained → proxy resumes existing session

DELETE /api/tasks/abc
  → DEL task:abc task-lock:abc
```

---

## Configuration reference

```yaml
redis:
  url: ""  # redis://[:password@]host[:port][/db]
           # empty = in-memory store (no persistence)
```

Tested with Redis 6+. Single-node only (no Sentinel or Cluster in v1).
