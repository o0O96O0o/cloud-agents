# Platform Plans

New product platform built on top of OpenSandbox. First feature: chat with a Claude agent running inside a sandbox.

## Active

- [session-store-interface.md](./session-store-interface.md) — SessionStore interface abstracting history retrieval; OFS S3 client as first implementation

- [stream-input-and-file-upload.md](./stream-input-and-file-upload.md) — Steering messages into active runs + image file attachments while prompting
- [overview.md](./overview.md) — Architecture, flow, repo layout, shared API contract
- [frontend.md](./frontend.md) — Vite+React+shadcn frontend: step-by-step
- [frontend-resources.md](./frontend-resources.md) — Resources page (skills + MCP management UI)
- [ssh-key-management.md](./ssh-key-management.md) — Per-user SSH key storage + sandbox injection
- [git-task-integration.md](./git-task-integration.md) — Optional git URL per task; clone on provision (depends on SSH plan)
- [scheduled-tasks.md](./scheduled-tasks.md) — User-created scheduled tasks; cron/one-shot triggers; per-run history

## Archived (implemented)

See [archived/](./archived/) for completed plans:
- `schedule-api-trigger-and-run-outcome.md` — Per-schedule fire tokens (API trigger) + run outcome tracking (completed/failed/timeout)
- `handler-decomposition.md` — Monolithic Handler split into TaskHandler, ResourceHandler, WorkspaceHandler, UserHandler + UserRepository interface
- `gin-migration.md` — net/http → Gin migration
- `backend.md` — Go backend reference
- `redis-task-store.md` — Redis task store (superseded by MySQL+Redis split)
- `sso-integration.md` — SSO/OIDC authentication
- `history-replay.md` — History replay API
- `resource-mapping.md` — MCP/skill resource injection
