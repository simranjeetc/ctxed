# Adapters

An adapter maps one harness's session document onto ctxed's canonical entry
list, and back. v1 ships two: `opencode` and `claude-code`.

## The boundary is bytes, not structs

Each adapter implements three operations over raw bytes:

```go
type Adapter interface {
	Name() string
	Detect(data []byte) bool
	Parse(data []byte) (*session.Document, error)
	Write(doc *session.Document) ([]byte, error)
}
```

`Parse` produces a `Document` holding an ordered `[]Entry` plus the opaque
source material needed to write back. An entry carries its original serialized
form, and the document keeps the surrounding structure — the JSON top-level
object and its entries array, or the JSONL lines. `Write` clones that structure,
omits only the entries an operation dropped, and re-serializes.

The consequence: ctxed never needs to understand a field it does not edit, so
unknown fields and non-entry lines survive a round-trip. Formatting and key order
are not preserved; that is a stated non-goal.

## Detection

Adapters are selected by `Detect` over the registered set, first match wins.
- **opencode**: a single JSON object with a `messages` array, plus either an
  `info` object or typed message items.
- **claude-code**: line-oriented JSON whose first non-empty line is an object
  with a `type` and a Claude envelope field (`message`, `sessionId`, or `uuid`).

If nothing matches, ctxed exits non-zero and lists the supported formats, which
come from the registry — never a hard-coded list.

## Harness notes

### Claude Code — direct file

Transcripts are JSONL files:

```
~/.claude/projects/<project-slug>/<session-uuid>.jsonl
```

Entries are the `type: "user"` and `type: "assistant"` lines that carry a
`message`. Metadata lines (`ai-title`, `file-history-snapshot`, and so on) are
not entries and are preserved verbatim on write. Tool calls (`tool_use`) and
results (`tool_result`) live in separate lines, so dropping one without the other
is detected as an orphan.

```sh
ctxed inspect  ~/.claude/projects/<project>/<session>.jsonl
ctxed drop     ~/.claude/projects/<project>/<session>.jsonl --indices 3,7
```

### OpenCode — export, edit

OpenCode's source of truth is a SQLite database
(`~/.local/share/opencode/opencode.db`); sessions are not stored as files. The
adapter targets the JSON produced by `opencode session export`. ctxed never opens
the database.

```sh
opencode session list
opencode session export <session-id> > session.json
ctxed inspect session.json
ctxed drop    session.json --indices 3,7,9   # writes session.edited.json
```

**Returning an edit to OpenCode is not supported in place.** `opencode session
import` rejects an edited export of a session that already exists locally
(`UNIQUE constraint failed: session_message.id` — the message ids collide), and
regenerating ids would create a *new* session rather than continue the existing
one. So the edited document is a portable artifact, not an in-place write.
Continuing a live OpenCode session with pruned context is done non-destructively
at dispatch instead; see the `add-category-prune` change and
`docs/plugin-contract.md`.

`opencode session export --sanitize` redacts transcript and file data before
export, which is useful when the session may contain secrets.

## Adding an adapter

1. Create `internal/adapter/<name>/<name>.go` implementing the interface.
2. Register it from `init()`: `adapter.Register(&Adapter{})`.
3. Blank-import the package from `internal/adapter/builtin/builtin.go`.
4. Add a trimmed, schema-faithful fixture to `testdata/` and tests that prove:
   detection, parse shape, a parse→write→parse round-trip, and that a
   non-entry field/line survives a write.

Registry behavior is covered by `internal/adapter/adapter_test.go`, which
registers a fake adapter and asserts that adding one changes no existing
behavior and appears in the supported-format list.

## Fixtures

`testdata/opencode_session.json` and `testdata/claude_session.jsonl` are trimmed
from real sessions. They keep the real schema but shorten long strings, so they
stay small. If a harness changes its layout, regenerate a fixture from a real
session and update the adapter; the capability specs are layout-agnostic and do
not change.
