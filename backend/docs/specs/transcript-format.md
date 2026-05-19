# Transcript Format

This document describes how the Claude Agent SDK writes conversation transcripts to S3-compatible storage (OrangeFS), the structure of each entry type, and how the backend reads them back.

## Part File Layout

The agent server writes transcripts as NDJSON part files under:

```
{username}/history/{encoded_cwd}/{session_id}/part-{epochMs13}-{rand6}.ndjson
```

Part file names are lexicographically sortable in chronological order because the epoch prefix is fixed-width (13 decimal digits). A random 6-character hex suffix disambiguates concurrent writes within the same millisecond.

### Batching and Flush Triggers

The SDK accumulates transcript frames in a batcher and flushes them in two situations:

| Trigger | Threshold |
|---------|-----------|
| End of turn | `result` message received from the agent |
| Eager flush | > 500 pending entries **or** > 1 MiB pending bytes |

Because a normal turn (one user prompt → one assistant response) never exceeds those eager thresholds, **every turn produces exactly two part files**:

| File | Contents | Timing |
|------|----------|--------|
| Content part (`part-{ts_A}-{rand}.ndjson`) | `queue-operation` × 2, `user`, [`attachment`], `assistant` | Flushed on `result` (end of turn) |
| Meta part (`part-{ts_B}-{rand}.ndjson`) | `last-prompt` only | Flushed immediately after; `ts_B - ts_A` ≈ 200–300 ms |

Example from observed data (3 turns → 6 part files):

```
part-1778587171934-af0c9c.ndjson  ← turn 1 content
part-1778587172243-4f4791.ndjson  ← turn 1 meta (last-prompt)
part-1778587182775-a528b0.ndjson  ← turn 2 content
part-1778587183014-9aa897.ndjson  ← turn 2 meta (last-prompt)
part-1778587193228-ef2e44.ndjson  ← turn 3 content
part-1778587193464-81a002.ndjson  ← turn 3 meta (last-prompt)
```

---

## Entry Types

Every entry in the NDJSON stream is a JSON object. The `type` field is the discriminator.

### Envelope Fields (all chainable entries)

| Field | Type | Notes |
|-------|------|-------|
| `type` | string | See table below |
| `uuid` | string | Unique ID for this entry; used as parent reference |
| `parentUuid` | string \| null | UUID of the parent entry in the conversation tree |
| `sessionId` | string | Agent session UUID |
| `timestamp` | string | ISO-8601 wall-clock time |
| `isSidechain` | bool | `true` for subagent entries; `false` for main agent |
| `isMeta` | bool | `true` for internal bookkeeping entries excluded by the storage client |

### Chainable Entry Types

These carry `uuid` and `parentUuid` and participate in conversation-tree reconstruction.

| `type` | Description |
|--------|-------------|
| `user` | User turn: `message.role = "user"`, content is text or `tool_result` blocks |
| `assistant` | Assistant turn: `message.role = "assistant"`, content is text / thinking / tool_use blocks |
| `attachment` | File attachment metadata (no message content) |
| `progress` | In-progress tool activity update; carries `uuid`/`parentUuid` |
| `system` | System prompt snapshot; written once per session |

### Metadata Entry Types

These do **not** carry `uuid`/`parentUuid` and are not part of the conversation chain. They convey session-level metadata that follows a "last-wins" update strategy.

| `type` | Key field | Description |
|--------|-----------|-------------|
| `last-prompt` | `lastPrompt` | Plaintext summary of the most recent user prompt; written after every turn |
| `tag` | `tag` | User-applied session tag |
| `custom-title` | `customTitle` | User-set conversation title |
| `ai-title` | `aiTitle` | AI-generated conversation title |
| `summary` | `summary` | Session summary hint (used for context compaction) |
| `queue-operation` | `operation` | Agent queue bookkeeping (`enqueue` / `dequeue`); not displayed in UI |
| `agent_metadata` | various | Subagent descriptor; written to `.meta.json` sidecar, not the main transcript |

`isMeta: true` entries are additional internal bookkeeping entries; the storage client excludes them before returning entries to callers.

---

## Conversation Tree Reconstruction

Entries form a directed tree via `parentUuid`. The frontend's `chainBuilder.ts` reconstructs the visible conversation as follows:

1. **Index** all entries by `uuid`.
2. **Find leaves**: entries whose `uuid` is not referenced by any other entry's `parentUuid`.
3. **Filter** sidechains (`isSidechain: true`), team entries, and metadata (`isMeta: true`).
4. **Select** the newest main-chain leaf (by file order).
5. **Walk backward** from the leaf via `parentUuid` to the root.
6. **Reverse** to get chronological order.

Subagent entries (`isSidechain: true`) form a separate linear chain attached to the corresponding `Agent` tool-use block.

---

## History Pagination

Since a task always has a single session, pagination is by **part file** rather than by session.

`GET /api/tasks/:id/history?cursor=<partFileKey>` returns at most `historyPageSize` part files (default 20, ≈ 10 turns):

- `cursor=""` → the newest 2 part files (1 turn).
- `cursor=<key>` → the 2 part files strictly older than `<key>` (1 more turn).
- `nextCursor=""` means no older files exist.

The cursor is the S3 key of the **oldest** part file in the current page. Clients request older turns by passing the `nextCursor` value back as `cursor`.

This means:
- `ListObjectsV2` is called once per request (metadata only — no file downloads).
- Only 2 part files are downloaded per page, regardless of total history size.

See [`ofsspec.md`](ofsspec.md) for the full S3 key reference and storage client interface.
