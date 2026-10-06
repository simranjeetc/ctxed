# Design

## Goals

- A harness shim is under ~150 lines and holds no format or state logic.
- The same Go code serves OpenCode today and the Claude Code mod next.
- No behavior change visible to the user; the wire-level suite stays green.

## Decisions

### `--from` names a live encoding; files stay auto-detected

File inputs keep detection (`context-session-format`). Live encodings are
ambiguous by shape (both OpenCode encodings are JSON arrays of messages), so the
caller names them: `ctxed categorize - --from opencode-hook`. The adapters are
ported from `transcript.ts` one-for-one; the existing Node tests become Go
fixture tests under `testdata/live/`.

`claude-mod` reads `SessionMessage[]` as documented in
`docs/claude-code-mods-spike.md`: `role`, `text`, `toolUses`, `toolResults`,
`handle`. The entry id is `handle`. If a message has no handle it is
categorizable but cannot be dropped by id; it is reported in stderr.

### State layout

```text
$XDG_STATE_HOME/ctxed/<harness>/<session-id>/
  categories.json      # last categorize output for this session
  selection.json       # {"categoryIds":[1,3],"selectedAt":"…"}
```

Writes are atomic (temp file + rename). The directory is created 0700. ctxed
still never touches a harness's own store.

Anti-drift is unchanged in substance: at each `prune`, ctxed re-resolves the
dropped set over the transcript it is given. New messages that arrived after
categorization are not in any bucket, so they are kept. (Re-bucketing new
messages is out of scope; see Open Questions.)

### `serve --stdio`

One JSON object per line in each direction:

```json
{"id":1,"op":"prune","sessionKey":"opencode/ses_x","from":"opencode-hook","messages":[…]}
{"id":1,"ok":true,"droppedIds":[…],"droppedToolCallIds":[…]}
```

Same functions as the CLI; the CLI is a single-request client of the same
handler. A shim that cannot keep a child process uses the CLI.

### Protocol version

An integer bumped on any breaking change to `--from` encodings, state layout, or
`serve` messages. Shims embed the protocol they were built against and refuse to
run (visible message in the session) on mismatch.

## Risks

- Moving translation can change which ids are entries. Mitigation: port the
  Node tests' fixtures verbatim and assert identical `droppedIds` before deleting
  `transcript.ts`; the wire-level suite guards the end-to-end result.

## Open Questions

- Should new post-selection messages be auto-assigned to buckets (so a dropped
  topic stays dropped as it grows)? The current verifier's "anti-drift" scenario
  expects a post-selection **alpha** message to be absent; confirm how the
  current plugin achieves that (re-categorize? label match?) before porting, and
  preserve that behavior exactly.
- State retention: when is a session's state directory removed? Proposed:
  `ctxed select … --clear`, plus `ctxed state gc --older-than 30d`.

## Non-Goals

- Any UX change (that is `unify-ux-and-install`).
- Building the Claude Code mod.
