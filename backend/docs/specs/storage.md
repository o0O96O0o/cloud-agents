# Storage Architecture

The backend splits task state into two stores based on durability:

| Store | What lives here | Persistence |
|---|---|---|
| **MySQL** (`tasks` table) | Task identity and durable fields | Survives restarts; shared across instances |
| **Redis** (`sandbox:{id}` hash) | Ephemeral sandbox routing data | Evicted on sandbox destroy; 7-day safety TTL |
| **Redis** (`task-lock:{id}`) | Distributed provisioning lock | Ephemeral; auto-expires on crash |

Both MySQL and Redis are **required** in production. The server fatals at startup if either is unreachable.

---

## MySQL — `tasks` table

Managed by GORM `AutoMigrate` on startup. Schema:

| Column | Go type | Notes |
|---|---|---|
| `id` | `string` (36) | 12-char lowercase hex short ID; primary key; immutable. GORM column declared `size:36` (oversized but harmless). |
| `user_id` | `uint` | FK → `users.id`; immutable; indexed. Owner resolved via JOIN at query time — `username` is not a stored column. |
| `state` | `int` | Sandbox liveness state (see below) |
| `title` | `string` (255) | Set after first session completes; initially empty |
| `session_id` | `string` (36) | Write-once; never cleared once set (invariant 4) |
| `extra_env` | `text` (JSON) | Per-task environment variables; set at creation; immutable |
| `provisioned` | `bool` | Whether a sandbox has been successfully provisioned |
| `git_url` | `string` (512) | Optional; repo URL cloned into sandbox at provision time. Empty string means no clone. |
| `error_msg` | `text` | Set when `state=StateError`; holds clone stderr or other provisioning failure detail. |
| `schedule_id` | `string` (36) | Nullable FK → `scheduled_tasks.id`; set when a schedule fires this task. Indexed. `""` is never written; null for manually created tasks. |
| `run_outcome` | `string` (20) | Write-once terminal outcome set by the schedule runner: `"completed"`, `"failed"`, or `"timeout"`. Empty for in-progress or manually created tasks. |
| `created_at` | `datetime` | Managed by GORM |
| `updated_at` | `datetime` | Managed by GORM |

#### State values

| Value | Constant | Effective spec state |
|---|---|---|
| `0` | `StateNew` | `pending` (no session) / `paused` (session set) |
| `1` | `StateProvisioning` | `provisioning` / `resuming` |
| `2` | `StateRunning` | `idle` / `active` |
| `3` | `StateError` | `error` |

The API-visible state label (e.g. `"active"`) is derived by combining `state` and whether `session_id` is non-empty. See [resource-mapping.md](resource-mapping.md) for the full state table.

#### Write-once `session_id`

`session_id` is set exactly once using a conditional `UPDATE`:

```sql
UPDATE tasks SET session_id = ? WHERE id = ? AND session_id = ''
```

`RowsAffected == 1` confirms the write succeeded; `0` means it was already set. No transaction or advisory lock is needed — row-level locking in MySQL makes this atomic.

---

## Redis — sandbox hash `sandbox:{task_id}`

Holds ephemeral sandbox routing information. Written when a sandbox becomes healthy, cleared when the sandbox is destroyed or resets.

| Field | Type | Notes |
|---|---|---|
| `sandbox_id` | string | OpenSandbox sandbox identifier |
| `proxy_base_url` | string | e.g. `https://…/sandboxes/:id/proxy/3000` |
| `proxy_headers` | JSON string | `{"Authorization":"Bearer tok"}` |

**TTL:** 7 days (`HSET` + `EXPIRE` in a single pipeline). This bounds memory growth from orphaned keys if a task is deleted after a Redis failure.

**Deleted by:** `DEL sandbox:{id}` when the task is deleted or its sandbox resets.

---

## Redis — distributed lock `task-lock:{task_id}`

Serialises concurrent provisioning and liveness-reset operations across backend instances.

| Property | Value |
|---|---|
| Key | `task-lock:{task_id}` |
| Value | `{hostname}:{pid}` (lock holder identity) |
| Acquire | `SET task-lock:{id} {holder} NX PX 30000` |
| Release | Lua CAS: `if GET key == holder then DEL key end` |
| TTL | 30 seconds (auto-released on crash) |

Retry uses exponential backoff: 50 ms → 100 ms → 200 ms → … capped at 5 s per attempt.

Used by:
- **`EnsureProvisioned`** — deadline 30 s; prevents two instances from provisioning the same task simultaneously.
- **`ResetIfExpired`** — deadline 5 s; prevents a liveness reset from racing with re-provisioning.

---

## Redis — CLI login session `cli_login_session:{session_id}`

Used by the OIDC CLI login flow only. Not part of task storage.

| Property | Value |
|---|---|
| Value | JSON `{status, token}` |
| TTL | 5 minutes |

---

## Key operations

### Create task

```sql
INSERT INTO tasks (id, username, state, extra_env, provisioned, ...) VALUES (...)
```

No Redis writes at creation time.

### Get task

```sql
SELECT * FROM tasks WHERE id = ?
```

```
HGETALL sandbox:{id}    -- merge ephemeral sandbox fields onto result
```

### Delete task

```sql
DELETE FROM tasks WHERE id = ?
```

```
DEL sandbox:{id} task-lock:{id}    -- best-effort Redis cleanup
```

### SetRunning (sandbox provisioned)

```sql
UPDATE tasks SET state = 2 WHERE id = ?
```

```
-- atomically write sandbox hash + set TTL
MULTI
  HSET sandbox:{id}  sandbox_id {id}  proxy_base_url {url}  proxy_headers {JSON}
  EXPIRE sandbox:{id} 604800   -- 7 days
EXEC
```

### EnsureProvisioned

```
-- 1. Acquire lock
SET task-lock:{id} {holder} NX PX 30000

-- 2. Check provisioned flag in MySQL
SELECT provisioned FROM tasks WHERE id = ?   -- skip fn() if true

-- 3. Run provisioning fn() [calls SetRunning → MySQL UPDATE + Redis HSET]

-- 4. Verify state was persisted in MySQL
SELECT state FROM tasks WHERE id = ?   -- must equal 2 (StateRunning)

-- 5. Verify sandbox hash was written to Redis
HGET sandbox:{id} sandbox_id   -- must be non-empty

-- 6. Mark provisioned
UPDATE tasks SET provisioned = true WHERE id = ?

-- 7. Release lock (Lua CAS)
```

If step 4 or 5 fails, the operation returns an error without setting `provisioned=true`, so the next caller will retry.

### ResetIfExpired (sandbox liveness check)

```
-- 1. Acquire lock (deadline 5 s)
SET task-lock:{id} {holder} NX PX 30000

-- 2. Short-circuit if not provisioned
SELECT provisioned FROM tasks WHERE id = ?

-- 3. Read sandbox_id from Redis
HGET sandbox:{id} sandbox_id

-- 4. Call isAlive(sandbox_id)

-- 5. If not alive: reset MySQL + clear Redis
UPDATE tasks SET state = 0, provisioned = false WHERE id = ?
DEL sandbox:{id}

-- 6. Release lock
```

`session_id` is intentionally not cleared — it must be retained for OFS history reads even when no sandbox is active.

---

## Lifecycle walkthrough

```
POST /api/tasks
  → INSERT INTO tasks (state=0, provisioned=false)

POST /api/tasks/abc/messages  (first message)
  → SELECT * FROM tasks WHERE id='abc'
  → HGETALL sandbox:abc                             (empty — no sandbox yet)
  → SET task-lock:abc {holder} NX PX 30000          (acquire lock)
  → SELECT provisioned FROM tasks WHERE id='abc'    → false
  → provision sandbox → SetRunning:
      → UPDATE tasks SET state=2 WHERE id='abc'
      → HSET sandbox:abc sandbox_id=sb-1 proxy_base_url=… proxy_headers=…
      → EXPIRE sandbox:abc 604800
  → SELECT state FROM tasks WHERE id='abc'          → 2  (guard passes)
  → HGET sandbox:abc sandbox_id                     → "sb-1"  (guard passes)
  → UPDATE tasks SET provisioned=true WHERE id='abc'
  → DEL task-lock:abc                               (release lock)
  → proxy message to sandbox
  → [SSE session.init] UPDATE tasks SET session_id=? WHERE id=? AND session_id=''
  → [stream complete]  GET /sessions/:sid → title → UPDATE tasks SET title=?

GET /api/tasks/abc
  → SELECT * FROM tasks WHERE id='abc'              (state=2, session_id set)
  → HGETALL sandbox:abc                             (sandbox_id, proxy info)
  → response: {state:"active", title:"...", session_id:"..."}

[sandbox expires]

POST /api/tasks/abc/messages  (next message)
  → SELECT * FROM tasks WHERE id='abc'
  → HGETALL sandbox:abc
  → ResetIfExpired:
      → SET task-lock:abc {holder} NX PX 30000
      → SELECT provisioned FROM tasks            → true
      → HGET sandbox:abc sandbox_id              → "sb-1"
      → isAlive(sb-1)                            → false
      → UPDATE tasks SET state=0, provisioned=false WHERE id='abc'
      → DEL sandbox:abc
      → DEL task-lock:abc
  → EnsureProvisioned: re-provision → state=2, provisioned=true
  → session_id retained → proxy resumes existing session

DELETE /api/tasks/abc
  → DELETE FROM tasks WHERE id='abc'
  → DEL sandbox:abc task-lock:abc
```

---

## Configuration reference

```yaml
mysql:
  dsn: "user:pass@tcp(localhost:3306)/dbname?parseTime=true&loc=UTC"

redis:
  url: "redis://localhost:6379"   # required — sandbox mapping + distributed locks
```

Both fields are required. The server exits at startup if either is absent or unreachable.
