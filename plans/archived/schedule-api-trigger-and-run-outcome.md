# Schedule API Trigger & Run Outcome

Two additions to the existing scheduled-tasks system:

1. **API Trigger** — a per-schedule bearer token that allows any external system to fire a schedule via HTTP, without a user JWT.
2. **Run Outcome** — a `completed / failed / timeout` field on task runs so callers (and the UI) can tell whether the agent actually succeeded, independent of sandbox infrastructure state.

**Depends on:** `scheduled-tasks.md`

---

## Part 1: API Trigger

### Motivation

Right now, firing a schedule requires a user JWT (via `POST /api/schedules/:id/run`). This makes it impossible to wire a schedule into external systems (CI pipelines, alerting tools, deploy scripts) without embedding a user credential. A per-schedule token scoped only to triggering that one schedule is a safer, more composable interface.

The fire endpoint also accepts an optional `text` field — run-specific context appended to the prompt. This lets a CI system pass a build log, an alerting tool pass an error payload, etc.

---

### DB: `schedule_tokens` table

New GORM model at `internal/db/schedule_token.go`:

```go
type ScheduleToken struct {
    ID         string     `gorm:"primaryKey;size:36"`
    ScheduleID string     `gorm:"not null;size:36;index"`
    TokenHash  string     `gorm:"not null;size:64"` // SHA-256 hex of raw token
    CreatedAt  time.Time
    RevokedAt  *time.Time `gorm:"default:null"`
}
```

`TokenHash` stores `hex(sha256(rawToken))`. The raw token is returned once on generation and never stored. `RevokedAt != nil` means the token is no longer valid. A schedule can have at most one active token (enforced at the service layer).

Add `AutoMigrate(&ScheduleToken{})` to `db.Open`.

---

### Token lifecycle

**Generate:**
1. `crypto/rand.Read(32 bytes)` → hex-encode → raw token (64 hex chars)
2. Compute `sha256(rawToken)` → store as `TokenHash`
3. If an active token already exists for this schedule, revoke it first (set `RevokedAt = now`)
4. Insert new `ScheduleToken` record
5. Return raw token to caller (shown once)

**Revoke:**
- Set `RevokedAt = now` on the matching record.

**Lookup (used by fire middleware):**
- `SELECT * FROM schedule_tokens WHERE token_hash = ? AND revoked_at IS NULL` — returns the `ScheduleToken`, from which the `ScheduleID` is read to load the schedule.

---

### New endpoints

Three new endpoints on the schedule handler. The first two require the user's JWT (same as all other schedule endpoints). The third is on a public router group authenticated by the schedule token.

```
POST   /api/schedules/:id/tokens        generate a fire token
DELETE /api/schedules/:id/tokens        revoke the current token
POST   /public/schedules/:id/fire       fire the schedule (token auth only, no JWT)
```

#### `POST /api/schedules/:id/tokens`

Requires JWT. Revokes any existing token for the schedule and generates a new one.

**Response `201`:**
```json
{
  "token_id":   "<uuid>",
  "raw_token":  "abc123...",
  "created_at": "2026-05-19T10:00:00Z"
}
```

`raw_token` is shown **once only**. The caller must store it.

#### `DELETE /api/schedules/:id/tokens`

Requires JWT. Revokes the active token (no-op if none exists).

**Response `204`.**

#### `POST /public/schedules/:id/fire`

**No JWT required.** Authenticated via `Authorization: Bearer <raw_token>`.

The middleware hashes the incoming token, looks it up in `schedule_tokens`, and rejects with `401` if no active token matches or the schedule ID in the path doesn't match the token's `schedule_id`.

**Request body (optional):**
```json
{ "text": "Deploy #442 failed. Logs: ..." }
```

**Behavior:**
1. Load the schedule (must be enabled; return `404` if not found, `409` if disabled).
2. If `text` is non-empty, append it to the schedule's prompt:
   ```
   <original prompt>

   ---
   Context from trigger:
   <text>
   ```
3. Run the fire logic (same path as `runFire` in `runner.go`):
   - Respect concurrency policy (`Concurrency == 0` → skip if already running).
   - Create task, provision, stream message with the combined prompt.
4. Return `200 { "task_id": "<uuid>" }`.

Note: `@once` schedules can also be fired via this endpoint. The one-shot auto-disable logic (`enabled = false` after firing) still applies.

---

### Router change

Add a new public router group in `internal/api/router.go`:

```go
public := r.Group("/public")
public.Use(ScheduleTokenAuthMiddleware(tokenRepo))
public.POST("/schedules/:id/fire", schedHandler.FireSchedule)
```

`ScheduleTokenAuthMiddleware` hashes the Bearer token, queries `schedule_tokens`, and stores the matched `*db.ScheduleToken` in the Gin context so `FireSchedule` can read it without a second DB query.

---

### `internal/schedule/service.go` additions

Add token methods to `Service`:

```go
func (s *Service) GenerateToken(ctx context.Context, scheduleID string, userID uint) (rawToken string, rec *db.ScheduleToken, err error)
func (s *Service) RevokeToken(ctx context.Context, scheduleID string, userID uint) error
func (s *Service) LookupScheduleByToken(ctx context.Context, rawToken string) (*db.ScheduledTask, error)
```

`GenerateToken` checks ownership (`getOwned`) before writing. `LookupScheduleByToken` hashes the input and queries `schedule_tokens` + `scheduled_tasks` in a single join.

---

### `ScheduleStore` interface change

Add to `internal/api/interfaces.go` (or handlers_schedule.go):

```go
GenerateToken(ctx context.Context, scheduleID string, userID uint) (string, *db.ScheduleToken, error)
RevokeToken(ctx context.Context, scheduleID string, userID uint) error
LookupScheduleByToken(ctx context.Context, rawToken string) (*db.ScheduledTask, error)
```

---

### `runner.go`: shared fire helper accepts optional extra text

Refactor `runFire` signature to accept an optional context string:

```go
func runFire(ctx context.Context, gormDB *gorm.DB, taskSvc TaskService, schedID string, extraText string) error
```

The combined prompt:

```go
prompt := rec.Prompt
if extraText != "" {
    prompt = rec.Prompt + "\n\n---\nContext from trigger:\n" + extraText
}
```

Update existing callers (`scheduler.go` and the manual `RunScheduleNow` handler) to pass `""`.

---

## Part 2: Run Outcome

### Motivation

The existing `state` field on tasks tracks sandbox infrastructure state (`pending / provisioning / running / error`). A green `running` state means the session started without an infrastructure failure — it says nothing about whether the agent completed its task. `StateError` is set only on provisioning failures, not on agent-level errors or timeouts.

We need a separate field that the runner writes after the agent goroutine finishes.

---

### DB: `run_outcome` column on `tasks`

GORM field in `internal/db/task.go`:

```go
RunOutcome string `gorm:"column:run_outcome;size:20;default:null"`
```

Valid values:

| Value | Meaning |
|---|---|
| `""` (empty) | Run is in progress, or outcome not yet captured (e.g. manually created tasks) |
| `"completed"` | `StreamMessage` returned `nil` — agent finished normally |
| `"failed"` | Provision failed, or `StreamMessage` returned a non-deadline error |
| `"timeout"` | Context deadline exceeded (hit `TimeoutSecs`) |

`AutoMigrate` handles the column addition.

---

### `task.Task` and `task.Repository` changes

Add to `task.Task`:

```go
func (t *Task) SetRunOutcome(outcome string)
```

Persists via `taskOps` (same pattern as `SetError`, `SetTitle`). For `MemoryRepository` this updates the in-memory field; for `MySQLRepository` it issues a `UPDATE tasks SET run_outcome = ? WHERE id = ?`.

Add `RunOutcome string` to `TaskSummary`.

---

### `runner.go` changes

The goroutine in `runFire` currently:

```go
go func() {
    if err := taskSvc.EnsureProvisioned(runCtx, t); err != nil {
        t.SetError(err.Error())
        return
    }
    taskSvc.StreamMessage(runCtx, t, prompt)
}()
```

Becomes:

```go
go func() {
    if err := taskSvc.EnsureProvisioned(runCtx, t); err != nil {
        t.SetError(err.Error())
        t.SetRunOutcome("failed")
        return
    }
    err := taskSvc.StreamMessage(runCtx, t, prompt)
    switch {
    case err == nil:
        t.SetRunOutcome("completed")
    case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
        t.SetRunOutcome("timeout")
    default:
        t.SetRunOutcome("failed")
    }
}()
```

The `RunScheduleNow` handler in `handlers_schedule.go` has an identical inline goroutine — apply the same change there.

---

### API surface

`runListItem` in `internal/api/types.go` gains:

```go
RunOutcome string `json:"run_outcome"`
```

Populated from `TaskSummary.RunOutcome` in `ListScheduleRuns`.

`GET /api/tasks` and `GET /api/tasks/:id` already return `TaskSummary` fields — `run_outcome` will appear there automatically once `TaskSummary` is updated.

---

## Frontend changes

### `src/api/client.ts`

- `Schedule` type gains no new fields (tokens are ephemeral, not stored client-side).
- New functions: `generateScheduleToken(id)` → `{ token_id, raw_token, created_at }`, `revokeScheduleToken(id)`.
- `ScheduleRun` (used in `listScheduleRuns` response) gains `run_outcome: string`.

### `ScheduleDetailPage`

**Token management section** (below schedule metadata):
- Shows whether a fire token exists (without revealing it).
- **Generate token** button → calls `generateScheduleToken` → displays raw token in a one-time copyable dialog ("Store this token — it won't be shown again").
- **Revoke** button (shown if token exists) → calls `revokeScheduleToken`.
- Example curl snippet (pre-filled with the endpoint URL).

**Run history table** gains a `Outcome` column:
- `completed` → green badge
- `failed` → red badge
- `timeout` → yellow badge
- `""` (in progress) → show existing state badge only

---

## Affected files summary

| File | Change |
|---|---|
| `internal/db/schedule_token.go` | New model |
| `internal/db/task.go` | Add `RunOutcome` field |
| `internal/db/mysql.go` | Add `AutoMigrate(&ScheduleToken{})` |
| `internal/task/store.go` | Add `SetRunOutcome`, update `TaskSummary` |
| `internal/task/mysql_repository.go` | Persist `RunOutcome` |
| `internal/task/memory_repository.go` | Update in-memory `RunOutcome` |
| `internal/schedule/service.go` | Add `GenerateToken`, `RevokeToken`, `LookupScheduleByToken` |
| `internal/schedule/runner.go` | `runFire` accepts `extraText`; set `RunOutcome` in goroutine |
| `internal/api/handlers_schedule.go` | Add `FireSchedule`, `GenerateToken`, `RevokeToken` handlers; update goroutine in `RunScheduleNow` |
| `internal/api/middleware.go` | Add `ScheduleTokenAuthMiddleware` |
| `internal/api/router.go` | Register `/public` group + 3 new routes |
| `internal/api/types.go` | Add `RunOutcome` to `runListItem` |
| `frontend/src/api/client.ts` | Token functions, `run_outcome` on `ScheduleRun` |
| `frontend/src/pages/ScheduleDetailPage.tsx` | Token management UI, outcome badge in run history |
| `backend/docs/specs/scheduled-tasks.md` | Update to reflect new endpoints and fields |

---

## Key invariants

| # | Invariant |
|---|---|
| 1 | A schedule token is scoped to exactly one schedule. The fire endpoint verifies both the token hash and the schedule ID in the path. |
| 2 | Raw tokens are never stored; only `sha256(token)` is persisted. |
| 3 | Only one active (non-revoked) token per schedule at a time. `GenerateToken` revokes any existing active token before creating a new one. |
| 4 | `run_outcome` is write-once per task: once set, it is never overwritten. The first terminal state wins. |
| 5 | Manually created tasks (from chat UI) never have `run_outcome` set; the field stays `""`. Only runner-spawned tasks (cron fire, manual "run now", API fire) set it. |
| 6 | The `/public/schedules/:id/fire` endpoint respects the schedule's `concurrency` policy — it does not bypass the skip-if-running check. |
| 7 | Disabled schedules reject fire requests with `409 Conflict`, not `404`, so callers can distinguish "schedule exists but is paused" from "schedule not found". |
