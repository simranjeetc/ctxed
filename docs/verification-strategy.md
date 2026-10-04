# Verification strategy

Status: **decided** (2026-10-04). This says what "verified" means for ctxed, so
anyone adding a feature knows what proof is required and anyone who checks out
the repo can run it.

## The rule

A feature is verified when a **stranger can run one command and watch it work
against a real harness**. Code-green checks are a pre-filter, not the gate.

- **Functional tests are the gate.** They spawn a real Claude Code or OpenCode
  session, run the real flow, and assert on observable outcomes.
- **Offline tests are a pre-filter.** `scripts/verify.sh` is fast and offline; it
  runs first so a trivial breakage does not burn a live run. It is not proof.
- **Nothing asserts on internals.** Assertions are on outcomes a user can see:
  the session got smaller, the session id is unchanged, stored history is
  untouched, the session continues.

## What each layer proves

| Layer | Script | Proves | Cost | Gate |
| --- | --- | --- | --- | --- |
| Offline | `scripts/verify.sh` | code compiles, unit behavior, specs valid | seconds, no network | pre-filter |
| Functional | `scripts/verify-functionally.sh` | the feature works in a real session | one small model call per scenario | **the gate** |

## Functional scenarios (the ones that matter)

### OpenCode — `scripts/verify-functionally.sh --opencode`

Proves the whole workflow, in-session, on the live messages:

1. Create a throwaway session with `opencode run` and a canned prompt that
   produces tool traffic.
2. Run the in-session ctxed command: it categorizes the **live** messages and
   presents buckets. Select a bucket to drop.
3. Dispatch again.
4. Observe the outgoing request and assert:
   - the selected bucket's messages are **absent** from the request;
   - messages added **after** the selection, in the same dropped bucket, are
     **also absent** (the anti-drift requirement);
   - the stored session is **unchanged** and the session **continues**.
5. Assert **id parity**: the ids ctxed categorizes over are the ids the dispatch
   hook filters on. If this fails, the plugin must match on content/tool-id
   instead — it is the top risk in the change.

### Claude Code — `scripts/verify-functionally.sh --claude`

Proves the compaction-based live prune:

1. Create a throwaway session with `claude -p --output-format json` and a prompt
   that produces a few topics.
2. `ctxed categorize` → pick buckets → `ctxed compact-instruction` → the
   instruction sentence.
3. Drive the compaction headlessly (`claude -p --resume <id> "/compact
   <instruction>"`).
4. Assert the stored transcript **shrank**, the session id is **unchanged**, and
   the session **continues**.
5. When the D3b mod lands (drop buckets exactly via `session.compact`), add the
   same assertions against it.

## Running it — including from a fresh checkout

`scripts/verify-functionally.sh` is self-describing: it checks prerequisites and
fails with a clear message naming what is missing.

Prerequisites, and how to satisfy each on a fresh machine:

- `opencode` on `PATH` and authenticated (`opencode auth`), with at least one
  cheap model available.
- `claude` on `PATH` and authenticated (or `CLAUDE_CODE_OAUTH_TOKEN`).
- `ctxed` buildable (`go build ./cmd/ctxed`); the script builds it.
- A model for categorization, cheap and repeatable.

Configuration, all optional with defaults:

```sh
CTXED_TEST_OPENCODE_MODEL=opencode-go/<cheap-model>   # categorizer + session model
CTXED_TEST_CLAUDE_MODEL=haiku                         # Claude Code session model
CTXED_TEST_KEEP=1                                     # keep scratch sessions on failure
```

Isolation, so a run never touches real work:

- Scratch sessions are created in a temp directory and deleted afterwards.
- Claude Code runs against a **throwaway session id** under the real config dir
  (credentials live there); `CTXED_TEST_KEEP=1` leaves it only on failure.
- OpenCode scratch sessions are deleted with `opencode session delete`.
- Nothing is written to a stored session by ctxed; the only harness-side
  mutation is Claude Code's own compaction, which is the feature under test.

## Repeatability across changes

Functional scenarios assert **outcomes**, not code, so:

- an existing scenario keeps protecting old behavior after a new feature lands;
- a new feature adds a new scenario (and its offline pre-filter check);
- a scenario that starts failing is a regression in behavior, which is the thing
  we actually care about.

## Known limits (stated honestly)

- The categorizer's *quality* (are the buckets good?) is not asserted; only that
  the flow runs and the selection is honored.
- Claude Code's `/compact` is model-mediated: the scenario asserts the session
  shrank and continued, not that a specific message survived verbatim. The D3b
  mod, once built, removes that model dependency.
- Live tests spend tokens; the offline suite stays the default thing run on every
  save, and the functional suite is run before a merge or a release.
