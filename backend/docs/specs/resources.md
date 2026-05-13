# User Resources: Skills and MCP Servers

## Overview

Users can register two kinds of reusable resources that are automatically injected into every sandbox at provision time:

| Kind | Description | OFS path |
|------|-------------|----------|
| `skill` | Markdown instruction file; discovered by Claude Code as a project-level skill | `{username}/resources/skills/{name}/SKILL.md` |
| `mcp` | MCP server configuration; written into `.mcp.json` at workspace root | `{username}/resources/mcp/{name}.json` |

Resources are stored in two places:

- **MySQL `kinds` table** — owns the registry record (kind, name, OFS path, metadata, active flag)
- **OFS S3** — owns the content (skill markdown text or MCP config JSON)

---

## DB Schema

### `kinds` table

```sql
CREATE TABLE kinds (
    id           INT          NOT NULL AUTO_INCREMENT,
    user_id      INT UNSIGNED NOT NULL,
    kind         VARCHAR(50)  NOT NULL,           -- "skill" | "mcp"
    name         VARCHAR(100) NOT NULL,
    ofs_path     VARCHAR(512) NOT NULL,           -- S3 key prefix or full key
    meta         JSON         NOT NULL DEFAULT '{}',
    is_active    TINYINT(1)   NOT NULL DEFAULT 1,
    created_at   DATETIME(3),
    updated_at   DATETIME(3),

    PRIMARY KEY (id),
    UNIQUE  KEY uq_kinds_user_kind_name (user_id, kind, name),  -- no duplicates per user
    INDEX   ix_kinds_user_active (user_id, is_active),          -- list-active fast path

    CONSTRAINT fk_kinds_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
```

The unique index `(user_id, kind, name)` prevents a user from registering the same resource name twice for the same kind. Different users may use the same name.

### Go model (`internal/db/kind.go`)

```go
type Kind struct {
    ID       int    `gorm:"primaryKey;autoIncrement"`
    UserID   uint   `gorm:"not null;uniqueIndex:uq_kinds_user_kind_name;index:ix_kinds_user_active"`
    Kind     string `gorm:"size:50;not null;uniqueIndex:uq_kinds_user_kind_name"`
    Name     string `gorm:"size:100;not null;uniqueIndex:uq_kinds_user_kind_name"`
    OFSPath  string `gorm:"size:512;not null"`
    Meta     string `gorm:"type:json;not null"`
    IsActive bool   `gorm:"default:true;index:ix_kinds_user_active"`
    CreatedAt time.Time
    UpdatedAt time.Time
    User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
}
```

`Meta` is stored as a JSON column and exposed as `json.RawMessage` in `KindRecord`. AutoMigrate registers this model alongside `User` and `Task`.

#### Skill `meta` schema

For `skill` resources, `meta` carries the file manifest:

```json
{ "files": ["SKILL.md", "scripts/helper.py", "data/config.yaml"] }
```

`files` is a list of relative paths from the skill OFS prefix. `SKILL.md` is always the first entry and cannot be removed. Records with `meta = {}` (created before multi-file support) default to `["SKILL.md"]` at inject time.

```go
type SkillMeta struct {
    Files []string `json:"files"`
}

// SkillFiles() on KindRecord returns meta.Files, falling back to ["SKILL.md"].
```

---

## Repository Interface (`internal/db/kinds_repository.go`)

```go
type KindsRepository interface {
    Create(ctx, userID uint, kind, name, ofsPath string, meta json.RawMessage) (*KindRecord, error)
    Get(ctx, id int, userID uint) (*KindRecord, error)
    List(ctx, userID uint) ([]*KindRecord, error)       // all records (active + inactive)
    ListActive(ctx, userID uint) ([]*KindRecord, error) // is_active=true only; used at inject time
    Update(ctx, id int, userID uint, KindUpdate) (*KindRecord, error)
    Delete(ctx, id int, userID uint) error
}
```

All write operations (`Create`, `Update`, `Delete`) are scoped to `(id, userID)` so a user cannot modify another user's resource. `Update` and `Delete` return an error when no row is matched (not-found or wrong user).

`KindUpdate` is a struct — not a `map[string]any` — to avoid accidental overwrites:

```go
type KindUpdate struct {
    Meta     json.RawMessage // nil = no change
    IsActive *bool           // nil = no change
}
```

---

## REST API

All routes are under `/api/resources`, protected by `auth.BearerAuth`.

### `POST /api/resources` — Create a resource

**Request body:**

```json
{
    "kind":    "skill",          // required; "skill" | "mcp"
    "name":    "my-search",      // required; [a-zA-Z0-9_-]+, no spaces or slashes
    "content": "# Skill text",   // skill: SKILL.md body (required); mcp: raw JSON string
    "meta":    { }               // optional; for mcp, overrides content
}
```

For `skill`: `content` is written verbatim to OFS as `SKILL.md`. **`content` is required** — omitting it returns 400. `meta` is initialized as `{"files": ["SKILL.md"]}`.

For `mcp`: the configuration is taken from `meta` (preferred) or parsed from `content` (fallback). Must be valid JSON. Written to OFS and also stored in `kinds.meta` for injection at provision time.

**Validation:**
- `kind` must be `"skill"` or `"mcp"` (other values → 400)
- `name` must match `^[a-zA-Z0-9_-]+$` (spaces or slashes → 400)
- For `skill`: `content` must be non-empty (→ 400 otherwise)
- For `mcp`: content/meta must be valid JSON (→ 400 otherwise)

**OFS key assignment:**

| Kind | OFS key for content | DB `ofs_path` |
|------|---------------------|---------------|
| `skill` | `{username}/resources/skills/{name}/SKILL.md` | `{username}/resources/skills/{name}/` (prefix) |
| `mcp`   | `{username}/resources/mcp/{name}.json`         | `{username}/resources/mcp/{name}.json` (full key) |

**Response:** `201 Created` with the resource record as JSON.

---

### `GET /api/resources` — List resources

Returns all resources (active and inactive) owned by the authenticated user. Empty array when none exist.

**Response:** `200 OK`, array of resource objects.

---

### `POST /api/resources/zip` — Create a skill from a zip archive

Accepts a `multipart/form-data` request with two fields:

| Field | Type   | Description |
|-------|--------|-------------|
| `name` | string | Skill name — must match `^[a-zA-Z0-9_-]+$` |
| `file` | file   | A `.zip` archive |

**Zip requirements:**
- `SKILL.md` must exist at the zip root (→ 400 if missing)
- All file paths must match `^[a-zA-Z0-9_./-]+$` with no empty, `.`, or `..` segments (→ 400)
- Total files ≤ 20 (→ 422)
- Each file ≤ 1 MiB (→ 413)
- Total zip size ≤ 20 MiB (→ 413)

**Behavior:**
1. Parses and validates the zip server-side using `archive/zip`
2. Writes `SKILL.md` to OFS at `{username}/resources/skills/{name}/SKILL.md`
3. Creates the `kinds` DB record with `meta = {"files": ["SKILL.md"]}`
4. For each companion file: writes to OFS and appends to `meta.files`
5. Returns the final resource record with the complete `meta.files` list

**Response:** `201 Created` with the resource record, or:
- `400` — invalid name, missing SKILL.md, bad zip, or invalid file paths
- `413` — zip or individual file exceeds size limit
- `422` — more than 20 files in the zip
- `503` — OFS not configured

---

### `PUT /api/resources/:id` — Update a resource

**Request body** (all fields optional):

```json
{
    "content":   "...",       // write new content to OFS
    "meta":      { },         // update kinds.meta directly (does not touch OFS)
    "is_active": false        // toggle active flag
}
```

- If `content` is set:
  - For `skill`: writes to `{ofs_path}SKILL.md`, does **not** update `kinds.meta`.
  - For `mcp`: validates JSON, writes to `{ofs_path}`, and sets `kinds.meta` to the new JSON (so injection always reads the latest config from DB, not OFS).
- If only `meta` is set: updates DB record; does not touch OFS.
- If only `is_active` is set: updates DB record; does not touch OFS.

Requires fetching the existing record (to determine kind and OFS path) only when `content` is present.

**Response:** `200 OK` with updated record, or `404` if not found / wrong user.

---

### `DELETE /api/resources/:id` — Delete a resource

Removes the DB record. OFS content is **not** deleted (OFS cleanup is out of scope).

**Response:** `204 No Content`, or `404` if not found / wrong user.

---

### `PUT /api/resources/:id/files/*filepath` — Upload a skill file

Uploads or overwrites a single companion file inside a skill resource (kind must be `skill`).

**Path parameter:** `*filepath` is a relative path like `scripts/helper.py` or `data/config.yaml`.

**Request body:** Raw file content (binary-safe). No JSON wrapping.

**Limits:**
- Per-file size: 1 MiB (→ 413 if exceeded)
- Max files per skill: 20 (→ 422 if a new file would exceed this)

**Validation:**
- `filepath` must match `^[a-zA-Z0-9_./-]+$` with no empty, `.`, or `..` segments (→ 400)
- Resource must exist, be owned by user, and be of kind `skill` (→ 404 / 400)

**Behavior:**
- Writes content to OFS at `{ofs_path}{filepath}`
- If `filepath` is new: appends it to `meta.files` and saves
- If `filepath` already exists in `meta.files`: overwrites OFS content only, no DB update

**Response:** `200 OK` with updated resource record.

---

### `DELETE /api/resources/:id/files/*filepath` — Remove a skill file from the manifest

Removes a companion file from the skill's `meta.files` list. `SKILL.md` cannot be removed (→ 400). OFS content is **not** deleted.

**Response:** `200 OK` with updated resource record, `404` if file not in manifest / resource not found, `400` for invalid path or wrong kind.

---

## Sandbox Injection

At sandbox provision time, after the health check passes and before `task.SetRunning()` is called, `Manager.injectResources` is invoked.

### Trigger condition

```go
if m.kindsRepo != nil && m.ofsReader != nil && t.UserID != 0 {
    m.injectResources(ctx, t.UserID, t.Username, t.ID, sandboxID)
}
```

Injection is skipped (silently) when:
- `WithResources` was never called (OFS not configured)
- `t.UserID == 0` (task not backed by a DB user)

Injection failures are **non-fatal**: a log line is emitted and provisioning continues. A misconfigured resource does not block task creation.

### Skill injection

For each active `skill` record, iterate over `meta.files` (defaults to `["SKILL.md"]` for old records with empty meta):

For each file `relPath` in `meta.files`:
1. Compute `targetDir = {taskCWD}/.claude/skills/{name}/{dir(relPath)}` — if `relPath` has no subdirectory, `targetDir` is `{taskCWD}/.claude/skills/{name}/`.
2. Create `targetDir` via execd `POST /directories` (mkdir -p semantics).
3. Fetch content from OFS: `{ofs_path}{relPath}`
4. Write to sandbox via execd: `{taskCWD}/.claude/skills/{name}/{relPath}`

Example for a skill with `meta.files = ["SKILL.md", "scripts/helper.py"]`:
- Creates `.claude/skills/{name}/` and writes `SKILL.md`
- Creates `.claude/skills/{name}/scripts/` and writes `scripts/helper.py`

Backward compatibility: records with `meta = {}` (created before multi-file support) default to `["SKILL.md"]` — same single-file behavior as before.

Claude Code discovers skills at `{taskCWD}/.claude/skills/{name}/SKILL.md` automatically with `setting_sources=["project"]`.

### MCP injection

All active `mcp` records are composed into a single `.mcp.json` file:

```json
{
    "mcpServers": {
        "{name}": { /* kinds.meta content */ }
    }
}
```

Written to `{taskCWD}/.mcp.json` via execd. Claude Code reads this file to register MCP server connections.

### File write protocol

`writeFile(ctx, sandboxID, absPath, content)` sends a multipart `POST` to the execd upload API inside the sandbox:

```
POST {serverURL}/sandboxes/{sandboxID}/proxy/44772/files/upload
X-OPEN-SANDBOX-API-KEY: {apiKey}
Content-Type: multipart/form-data; boundary=...

--boundary
Content-Disposition: form-data; name="metadata"
{"path":"{absPath}","mode":755}
--boundary
Content-Disposition: form-data; name="file"; filename="{basename}"
<binary content>
--boundary--
```

The `serverURL` is the OpenSandbox server URL configured on the `Manager`. The file path is embedded in the `metadata` JSON part, not in the URL.

---

## OFS Paths Reference

| Resource | S3 content key | DB `ofs_path` value |
|----------|----------------|---------------------|
| Skill `my-sk` (user `alice`) | `alice/resources/skills/my-sk/SKILL.md` | `alice/resources/skills/my-sk/` |
| MCP `gh` (user `alice`) | `alice/resources/mcp/gh.json` | `alice/resources/mcp/gh.json` |

The `ofs_path` column in the DB is a prefix for skills (the SKILL.md filename is appended at inject time) and a full key for MCP (content and the DB ofs_path are the same file).

---

## Interface Contracts

### `ResourceWriter` (api package)

```go
type ResourceWriter interface {
    PutObject(ctx context.Context, key string, data []byte) error
}
```

Implemented by `*storage.Client`. Injected into `Handler` via `withResources`.

### `ofsReader` (sandbox package)

```go
type ofsReader interface {
    GetObjectBytes(ctx context.Context, key string) ([]byte, error)
}
```

Implemented by `*storage.Client`. Injected into `Manager` via `WithResources`.

Both interfaces are narrow by design — the concrete `*storage.Client` type is not exposed beyond `cmd/server/main.go`.

---

## Wiring (`cmd/server/main.go`)

```go
kindsRepo := db.NewKindsRepository(gormDB)
if ofsClient != nil {
    mgr.WithResources(kindsRepo, ofsClient)
}
router := api.NewRouter(api.RouterDeps{
    ...
    KindsRepo: kindsRepo,
    OFSWriter: ofsClient,   // nil when OFS not configured → resource API returns 503
})
```

When `OFSWriter` (or `KindsRepo`) is nil, all resource API endpoints respond with `503 Service Unavailable`.

---

## Related Documents

- [`data-management.md`](data-management.md) — full DB schema including `kinds` table
- [`ofsspec.md`](ofsspec.md) — OFS layout; `PutObject`/`GetObjectBytes` on `*storage.Client`
- [`resource-mapping.md`](resource-mapping.md) — Task / Sandbox / Session lifecycle
