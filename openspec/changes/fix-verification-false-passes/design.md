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
