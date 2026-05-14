# Handler Decomposition Plan

## Problem

`Handler` in `internal/api/handlers.go` aggregates 13 fields across 5 unrelated domains:

| Domain | Fields |
|---|---|
| Tasks | `store`, `manager`, `proxy`, `fileStore` |
| Resources | `kindsRepo`, `ofsWriter`, `ofsReader` |
| Workspace / Execd | `workspaceReader`, `serverURL`, `sandboxAPIKey`, `httpClient` |
| User settings | `gormDB`, `sshKeySecret` |
| (shared) | — |

This creates invisible coupling: adding a workspace feature requires touching the same struct as user SSH-key logic. It also leaks `*gorm.DB` directly into handler code — `UpdateUserSettings` calls `h.gormDB.Model(...)` inline, and auth handlers (`PasswordLoginHandler`, `RegisterHandler`) receive `*gorm.DB` as a parameter.

The principle: **handlers must never import or hold `*gorm.DB`**. All storage access goes through repository interfaces. `KindsRepository` (in `internal/db`) already follows this pattern; this plan extends it to user operations.

---

## Target Structure

### `internal/db/` — add `UserRepository`

`user.go` currently has standalone functions that take `*gorm.DB`. Replace with an interface + MySQL implementation following the same pattern as `KindsRepository`.

```go
// internal/db/user_repository.go

type UserRepository interface {
    FindByCredentials(ctx context.Context, userName, password string) (*User, error)
    CreateWithPassword(ctx context.Context, userName, email, password string) (*User, error)
    UpdateSSHKey(ctx context.Context, userName, encryptedKey string) error
}

type MySQLUserRepository struct{ db *gorm.DB }

func NewUserRepository(db *gorm.DB) *MySQLUserRepository
```

The existing package-level functions (`FindByCredentials`, `CreateWithPassword`, `FindOrCreate`) are kept for now to avoid breaking OIDC/SSO callers (which still take `*gorm.DB`). They become thin wrappers or are deprecated in a follow-up. `FindOrCreate` is not included in the interface because OIDC/SSO is out of scope here.

`RouterDeps` replaces `DB *gorm.DB` with `UserRepo db.UserRepository` for handler wiring. The raw `*gorm.DB` stays in `main.go` (where the connection is created) and is used only to construct repository implementations and wire auth middleware (`auth.BearerAuth` still takes `*gorm.DB` — updating the auth middleware is out of scope).

### `internal/api/` — split into domain handler files

```
internal/api/
  router.go             # wires four handlers; RouterDeps updated
  interfaces.go         # TaskStore, SandboxManager, FileStore, MessageProxy,
                        # ResourceWriter, ResourceReader, WorkspaceReader
  handlers_tasks.go     # TaskHandler + task-domain types
  handlers_resources.go # ResourceHandler + resource-domain types
  handlers_workspace.go # WorkspaceHandler + workspace/execd types
  handlers_user.go      # UserHandler + user-settings types
  handlers_auth.go      # PasswordLoginHandler, RegisterHandler
  types.go              # shared types: errorResponse, tokenResponse, runtimeConfigResponse
  middleware.go         # corsMiddleware
  smoke_test.go         # unchanged
  handlers_test.go      # update wiring if needed
  resources_test.go     # unchanged
  router_test.go        # unchanged
```

`handlers.go` is deleted once all methods are migrated.

---

## Handler Structs

### `TaskHandler`

```go
type TaskHandler struct {
    store     TaskStore
    manager   SandboxManager
    proxy     MessageProxy
    fileStore FileStore
}

func NewTaskHandler(store TaskStore, mgr SandboxManager, proxy MessageProxy, fs FileStore) *TaskHandler
```

Methods: `CreateTask`, `GetTask`, `ListTasks`, `DeleteTask`, `SendMessage`, `GetTaskHistory`, `RespondToPermission`, `RespondToQuestion`

No DB dependency. Task ownership check uses `auth.GetUser(c)` from context — no repository needed.

### `ResourceHandler`

```go
type ResourceHandler struct {
    kindsRepo db.KindsRepository
    ofsWriter ResourceWriter
    ofsReader ResourceReader
}

func NewResourceHandler(repo db.KindsRepository, w ResourceWriter, r ResourceReader) *ResourceHandler
```

Methods: `CreateResource`, `CreateSkillFromZip`, `ListResources`, `UpdateResource`, `DeleteResource`, `GetSkillContent`, `UpsertSkillFile`, `DeleteSkillFile`

`db.KindsRepository` is already an interface — no change to the storage layer here. The nil-guard pattern (`if h.kindsRepo == nil`) moves inside handler methods, exactly as today.

### `WorkspaceHandler`

```go
type WorkspaceHandler struct {
    store           TaskStore
    workspaceReader WorkspaceReader
    serverURL       string
    sandboxAPIKey   string
    httpClient      *http.Client
}

func NewWorkspaceHandler(store TaskStore, wr WorkspaceReader, serverURL, apiKey string) *WorkspaceHandler
```

Methods: `WorkspaceFiles`, `WorkspaceFile`, `ExecdProxy`

Plain config values (`serverURL`, `sandboxAPIKey`) are injected as strings — no `*config.Config` in the handler.

### `UserHandler`

```go
type UserHandler struct {
    userRepo     db.UserRepository  // interface — no *gorm.DB
    sshKeySecret string
}

func NewUserHandler(repo db.UserRepository, sshKeySecret string) *UserHandler
```

Methods: `GetUserSettings`, `UpdateUserSettings`

`UpdateUserSettings` calls `h.userRepo.UpdateSSHKey(ctx, u.UserName, encKey)` instead of the inline GORM call. `GetUserSettings` reads `u.SSHPrivateKeyEnc` from the context user (already loaded by `auth.BearerAuth`) — no repository call needed for the GET.

### Auth handlers

`PasswordLoginHandler` and `RegisterHandler` are standalone `gin.HandlerFunc` factories. They currently take `*gorm.DB` — update to take `db.UserRepository`:

```go
func PasswordLoginHandler(repo db.UserRepository, authCfg config.AuthConfig) gin.HandlerFunc
func RegisterHandler(repo db.UserRepository, authCfg config.AuthConfig) gin.HandlerFunc
```

No GORM import in `handlers_auth.go`.

---

## `RouterDeps` Changes

```go
type RouterDeps struct {
    Store           TaskStore
    Manager         SandboxManager
    FileStore       FileStore
    KindsRepo       db.KindsRepository
    OFSWriter       ResourceWriter
    OFSReader       ResourceReader
    WorkspaceReader WorkspaceReader
    UserRepo        db.UserRepository  // replaces DB *gorm.DB for handler wiring
    DB              *gorm.DB           // kept for auth.BearerAuth and OIDC/SSO (out of scope)
    CORSOrigin      string
    Redis           *redis.Client
    Cfg             *config.Config
    OIDCService     *oidcpkg.Service
    SSOService      *ssopkg.Service
}
```

`main.go` creates `db.NewUserRepository(gormDB)` and puts it in `UserRepo`. The raw `DB` field stays for the auth middleware and OIDC/SSO wiring (those are not changing in this plan).

`NewRouter` wiring:

```go
func NewRouter(deps RouterDeps) http.Handler {
    taskH := NewTaskHandler(deps.Store, deps.Manager, sandbox.NewProxy(), deps.FileStore)

    var resourceH *ResourceHandler
    if deps.KindsRepo != nil {
        resourceH = NewResourceHandler(deps.KindsRepo, deps.OFSWriter, deps.OFSReader)
    }

    workspaceH := NewWorkspaceHandler(
        deps.Store, deps.WorkspaceReader,
        deps.Cfg.Sandbox.ServerURL, deps.Cfg.Sandbox.APIKey,
    )

    var userH *UserHandler
    if deps.UserRepo != nil {
        sshSecret := ""
        if deps.Cfg != nil { sshSecret = deps.Cfg.Security.SSHKeySecret }
        userH = NewUserHandler(deps.UserRepo, sshSecret)
    }

    authH_login    := PasswordLoginHandler(deps.UserRepo, deps.Cfg.Auth)
    authH_register := RegisterHandler(deps.UserRepo, deps.Cfg.Auth)
    // ...
}
```

---

## What Is NOT Changing

- **No service layer added.** Handlers do direct repository/proxy calls. A service layer adds indirection with no benefit at this codebase size. Introduce per-domain services if transactional logic grows (e.g. resource creation + OFS write in one unit).
- **Interface definitions stay in `internal/api` and `internal/db`.** `TaskStore`, `SandboxManager`, etc. are consumed only by the API layer. `UserRepository` / `KindsRepository` live in `internal/db` where the GORM models are defined.
- **`auth.BearerAuth` still takes `*gorm.DB`** — updating the auth middleware's user-loading to go through a repository is a separate task.
- **`FindOrCreate` not in `UserRepository`** — used only by OIDC/SSO packages which already take `*gorm.DB`. Updating those is out of scope.
- **Swagger annotations stay with each handler method.**
- **`RouterDeps` stays in `router.go`.**

---

## Migration Steps

1. **`internal/db/user_repository.go`** — define `UserRepository` interface + `MySQLUserRepository` struct implementing `FindByCredentials`, `CreateWithPassword`, `UpdateSSHKey`. Add `NewUserRepository(db *gorm.DB) *MySQLUserRepository`.
2. **`internal/api/interfaces.go`** — move `TaskStore`, `SandboxManager`, `FileStore`, `MessageProxy`, `ResourceWriter`, `ResourceReader`, `WorkspaceReader` out of `handlers.go`.
3. **`internal/api/handlers_tasks.go`** — `TaskHandler` struct + all task handler methods.
4. **`internal/api/handlers_resources.go`** — `ResourceHandler` + resource methods + resource-domain types.
5. **`internal/api/handlers_workspace.go`** — `WorkspaceHandler` + workspace/execd methods + `FileInfo` type.
6. **`internal/api/handlers_user.go`** — `UserHandler` (takes `db.UserRepository`) + user-settings methods + user types.
7. **`internal/api/handlers_auth.go`** — `PasswordLoginHandler` and `RegisterHandler` updated to take `db.UserRepository` instead of `*gorm.DB`.
8. **`internal/api/middleware.go`** — extract `corsMiddleware` from `router.go`.
9. **`types.go`** — retain shared types; move domain-specific types to their handler files.
10. **`router.go`** — update `RouterDeps` (add `UserRepo`), update `NewRouter` to wire four handlers, update auth handler calls.
11. **`main.go`** — construct `db.NewUserRepository(gormDB)`, populate `deps.UserRepo`.
12. Delete `handlers.go`.
13. `go test -race ./...` + `go build ./...` — fix any compilation errors.
14. `swag init -g cmd/server/main.go --output docs --parseDependency --parseInternal`.

---

## Non-Goals / Future Work

- Splitting `internal/api` into sub-packages (`internal/api/task`, etc.) — deferred; requires careful interface placement to avoid import cycles.
- `auth.BearerAuth` taking `UserRepository` instead of `*gorm.DB`.
- `FindOrCreate` added to `UserRepository` (unblocks removing `*gorm.DB` from OIDC/SSO handlers).
- Extracting service layer per domain.
