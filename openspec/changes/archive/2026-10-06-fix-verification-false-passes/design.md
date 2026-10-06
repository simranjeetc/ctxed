# Design

## Context

The verification contract in `docs/verification-strategy.md` is sound: functional
tests are the gate, assertions are on user-visible outcomes. This change makes the
scripts honor it. It deliberately does not change *what* is verified at the
wire level; that is `add-wire-level-verification`.

## Decisions

### Absent feature is a failure

Both live-prune features are on `main`. A check SHALL NOT probe for a feature and
skip. If a future branch genuinely lacks a feature, that branch edits the check;
the default protects `main`.

### Distinct sentinels per question

Each recall question uses a unique sentinel and reads **only the assistant reply
to that question** (the last assistant message after the prompt), never all
assistant text in the session. The "do you know X" protocol becomes: ask
"what is <SENTINEL-n>? reply with only the value or NONE-<n>", then assert the
reply contains `NONE-<n>`. A reply to a different question cannot satisfy it.

These recall checks remain **soft signals** (model-mediated). The hard gate for a
drop is id-level (and, after `add-wire-level-verification`, wire-level).
Report soft checks with a distinct status (`soft-pass` / `soft-fail`) so the
report's `ok` is driven by hard checks only.

### Attachment check identifies the attachment message

Find the message whose parts include the attached file (by file name or file
part type in `/api/session/{id}/message`), take its id, and assert that id is in
the alpha bucket and absent from the last outbound list.

### Wait for idle, not for time

Add `oc_wait_idle <port> <sid> [timeout]` that polls the session status endpoint
(or the message list until the last assistant message is complete) with a hard
timeout. Every `sleep N` after a prompt becomes `oc_wait_idle`. If OpenCode
exposes no status field, poll until the message count is stable across two reads
500 ms apart.

### Self-config uses the built binary

Constraints: a scratch `HOME` is not possible (harness credentials live in the
real HOME), and passing the path through env or options would weaken the
self-configuration claim. The plugin searches fixed locations for a file named
`ctxed` (`resolveCtxedPath` in `plugin/opencode/src/core.ts`).

Decided (user, 2026-10-06): **swap the built binary into the plugin's first
search location.** That is the first entry of `resolveCtxedPath`'s list
(`~/go/bin/ctxed` at `022a826`; read the list rather than hardcoding it, or
assert the two agree). A lower-priority location such as `~/.local/bin` would
lose to an installed `~/go/bin/ctxed`. Before the scenario, if the file exists,
move it aside to a backup; install the freshly built binary there; restore the
original in the EXIT trap (also on failure and on interrupt). If no original
existed, delete the installed copy on exit. State this mutation in the script's
header comment. Assert the plugin resolved that path (e.g. from the debug log),
or fail naming the location it actually used.

### Portability

- `PATH` additions become `CTXED_TEST_EXTRA_PATH` (default empty); the script
  still prepends nothing machine-specific.
- `OPENCODE_PASSWORD` has no default; if unset and the server requires one, fail
  with a named prerequisite.

## Non-Goals

- Adding new scenarios beyond fixing existing ones.
- Changing product behavior.
- Wire-level request capture (separate change).

## Implementation Notes

Recorded while applying the change (2026-10-06).

- **The offline checks were vacuous twice over.** `testdata/*.categorize.json`
  are categorizer responses (`ids`, no bucket `id`), not categories files, so
  `--categories N` never resolved; and the feature probes skipped inside the
  check's subshell, so the skip was lost and the check returned 0. The checks
  now build a real categories file with `categorize --categorizer-cmd 'cat
  <fixture>'` and compare exact golden output.
- **Negative control uses the hook's `before` list.** The plugin logs a dispatch
  decision only while a selection is recorded, so there is no "no selection"
  dispatch to read. Each log line carries `before` (the transcript the hook
  received, which is exactly what goes out with no selection) beside `after`.
  The control runs the same exact-drop check against `before` of the same
  dispatch and requires it to fail. It needs no extra model turn
  and no product change.
- **Anti-drift was never implemented, and is now dropped.** The fixed
  id-level check showed that a post-selection message on a dropped topic is
  sent, and the model recalled its code word. Decided (user, 2026-10-06): a
  selection covers only the messages it was made over; later messages are kept.
  The plugin's spec (`add-opencode-dispatch-plugin`) now says so. The check is
  reversed: in one dispatch, the post-selection message is present and every
  selected message is still absent. The recall question is soft and now
  expects the code word.
- **Self-config installs a shim, not a copy.** The file at the plugin's first
  search location is a two-line script that appends its path to a log and
  `exec`s the freshly built binary. The plugin still runs only this checkout's
  ctxed, and the log is the evidence that the plugin resolved that path, with
  no plugin change. The location is read from `resolveCtxedPath` in
  `plugin/opencode/src/core.ts`.
- **No password default.** The scenarios start their own scratch server, so they
  generate its password per run; no credential is read or defaulted.
- **Idle detection.** `GET /api/session/active` (a session absent from it is
  inactive) plus an empty `GET /api/session/{id}/inbox`, with an unchanged
  message list across two reads 500 ms apart.
- **Fixed sleeps were hiding a blocked session.** The plugin's bucket listing is
  a synthetic message, and OpenCode starts an agent turn on it; the model may
  call the interactive `question` tool, which waits for a human forever. Under
  `sleep N` the queued "drain" prompts were never delivered. The scratch
  project's `opencode.json` now denies `question`. A file read outside the
  project likewise blocks on a permission prompt, so the file-read turn reads a
  file inside the scratch project.
