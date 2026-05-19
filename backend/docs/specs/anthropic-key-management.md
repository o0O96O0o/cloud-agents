# Anthropic API Key Management

Per-user Anthropic API keys stored encrypted in MySQL and injected into sandboxes at provision time. Mirrors the SSH key management design; both secrets share the same AES-256-GCM encryption key (`security.ssh_key_secret`).

**Depends on:** `ssh-key-management.md` — same encryption helper, same config field, same `GET`/`PUT /api/user/settings` endpoints.

---

## Database

`users.anthropic_api_key_enc` — existing column added alongside `ssh_private_key_enc`:

```go
AnthropicAPIKeyEnc string `gorm:"column:anthropic_api_key_enc;type:text"`
```

Empty string means no key is stored. Never contains plaintext.

---

## Encryption

Uses `internal/crypto.Encrypt` / `Decrypt` (AES-256-GCM, base64url output) with the key from `config.Security.SSHKeySecret`. The same 32-byte hex key protects both secrets.

---

## API

Both fields are managed through the same user settings endpoints:

### `GET /api/user/settings`

```json
{
  "has_ssh_key":       true,
  "has_anthropic_key": false
}
```

Returns boolean presence flags. Key material is never returned.

### `PUT /api/user/settings`

```json
{
  "anthropic_api_key": "sk-ant-..."
}
```

- Non-empty value: validated (must be a non-empty string), encrypted, stored.
- Empty string `""`: clears the stored key (`anthropic_api_key_enc = ""`).
- `null` / field absent: no change to the Anthropic key.

Both `ssh_private_key` and `anthropic_api_key` may be sent in the same request body; either, both, or neither can be included.

**Response:** `200` with the updated `UserSettingsResponse`.

---

## Sandbox Injection

At provision time (`internal/sandbox/manager.go`), after decrypting the user's SSH key and injecting it, the manager also decrypts `AnthropicAPIKeyEnc` and injects it as the `ANTHROPIC_API_KEY` environment variable into the sandbox. This overrides any global key that might otherwise be configured.

The injection is conditional: if `AnthropicAPIKeyEnc` is empty, `ANTHROPIC_API_KEY` is not set (the sandbox may fall back to a default or fail at runtime depending on deployment config).

---

## Configuration

Uses the same `security.ssh_key_secret` field as SSH keys:

```yaml
security:
  ssh_key_secret: ""  # 32-byte hex; generate with: openssl rand -hex 32
```

**Startup behaviour:**
- If `ssh_key_secret` is blank and no user has a stored Anthropic key: server starts normally.
- If `ssh_key_secret` is blank and any user has a stored Anthropic key: server exits with an error (prevents silently failing decryption after a key rotation or misconfiguration).

---

## Key Invariants

1. The same `ssh_key_secret` config field encrypts both SSH keys and Anthropic API keys.
2. Key material is never returned by any API endpoint; only boolean presence is exposed.
3. An empty `anthropic_api_key` in a PUT request clears the stored key (explicit opt-out).
4. Sandbox injection is best-effort: no key stored → no env var injected (no error).

---

## Related Documents

- [`ssh-key-management.md`](ssh-key-management.md) — per-user SSH key; same encryption infrastructure
- [`configuration.md`](configuration.md) — `security.ssh_key_secret` config field
- [`data-management.md`](data-management.md) — `users` table schema
