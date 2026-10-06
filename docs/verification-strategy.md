# Verification strategy

Status: **decided** (2026-10-04). This defines what "verified" means for ctxed and
how the two verification agents run. It is a **contract**, not a runbook: the
running lives in `scripts/`, so the capability is in the repo and works from a
bare clone, with no dependency on any agent setup.

## The rule

A feature is verified when a **stranger can clone the repo, run one command, and
watch the feature work against a real harness**. Code-green checks are a
pre-filter, not the gate.

- **Functional tests are the gate** — `scripts/verify-functionally.sh`.
- **Offline tests are a pre-filter** — `scripts/verify.sh`. Fast, offline; not proof.
- **Assertions are on outcomes** a user can see: the overview finds the session
  it runs in, its numbers add up and match `ctxed inspect`, the session is
  untouched, and after a compaction only the live context is counted.

## Two verification agents, one per harness

The two harnesses need different prerequisites and models, and an incident in
one must not touch the other. So there are **two independent
verifiers**, each owning one harness and runnable in parallel.

| | OpenCode verifier | Claude Code verifier |
| --- | --- | --- |
| Entry point | `scripts/verify-functionally.sh --opencode` | `scripts/verify-functionally.sh --claude` |
| Harness | `opencode` (OpenCode Go entitled) | `claude` (Haiku) |
| Default model | `opencode-go/deepseek-v4-flash` | `haiku` |
| Report | `--report <path>` writes JSON | `--report <path>` writes JSON |

The script is harness-agnostic: it is the source of truth. An **agent brief** is
a thin wrapper that says which flag to run and where to put the report — nothing
more. Losing the agent setup loses convenience, not capability.

The coordinator (main session) does three things and nothing more:

1. Run the offline pre-filter: `scripts/verify.sh`.
2. Dispatch the two verifiers (in parallel): each runs its flag with `--report`.
3. Read the two JSON reports and act on `ok` / the failing checks.

## Functional scenarios (the ones that matter)

Both scenarios run the same steps against a real session in their harness,
created by the script and deleted afterwards:

1. **Plant two topics.** TOPIC-PELICAN is asked for, then confirmed finished.
   TOPIC-TURBINE ends on an open TODO naming a unique item (`WIDGET-<pid>`).
2. **Run `ctxed overview --json` as the skill does**: no arguments, with the
   session id in `CLAUDE_CODE_SESSION_ID` / `OPENCODE_SESSION_ID`, and a stub
   categorizer that groups by topic marker.
3. **Totals (hard):** the rows add up to the header, no entry is in two rows,
   and the header equals `ctxed inspect` on the same session.
4. **Read-only (hard):** the Claude Code transcript is byte-identical; the
   OpenCode export's stored messages have the same digest.
5. **Real model (hard):** the overview again, with the harness's own cheap model
   (`claude -p --model haiku`, `opencode run` with the OpenCode Go model). The
   row holding the PELICAN message is `done`; the row holding the TURBINE
   message is `in_progress`, and is a different row. **Soft:** the open item (or
   "URL") appears under pending.
6. **Compaction (hard):** compact the session (`claude -p --resume <id>
   "/compact"`; OpenCode `POST /api/session/<id>/compact`), run the overview
   again: earlier messages are reported as not counted, the last row is the
   compaction, and the totals still equal `inspect`. **Negative control:** the
   compacted overview checked against the pre-compaction `inspect` must fail.

| | `--opencode` | `--claude` |
|---|---|---|
| Session | scratch `opencode serve`, no plugins, real store | `claude -p` in a scratch dir |
| Cleanup | scratch session deleted | scratch project dir deleted |
| Last run (2026-10-06) | 9 hard pass, 1 soft pass, ~25 s | 9 hard pass, 1 soft pass, ~75 s |

The old prune scenarios are parked with the pruning code, in
`parked/verify-prune-functionally.sh`; they cannot run against the current
binary.

## Reports (the verifier's contract)

Each verifier writes JSON via `--report <path>`:

```json
{ "suite": "opencode", "model": "opencode-go/deepseek-v4-flash",
  "passed": 5, "failed": 0, "softPassed": 1, "softFailed": 1, "ok": true,
  "checks": [ { "status": "pass", "name": "opencode:prune --ids-only", "detail": "" },
              { "status": "soft-fail", "name": "opencode:post-selection recall", "detail": "reply: …" } ] }
```

A check's `status` is `pass` or `fail` (hard: asserted on ids, files, or exit
codes) or `soft-pass` or `soft-fail` (model-mediated: what a model recalls).
`passed`/`failed` count hard checks only, and `ok` is `failed == 0`: a soft
failure never makes a run fail. Read it as a signal next to the hard id-level
check it accompanies.

The coordinator acts on `ok` and, when false, on the `checks` with `status:
"fail"`. `--keep` retains scratch sessions and logs for a failing run.

## Running it — including from a fresh checkout

Prerequisites, each reported by name if missing:

- `opencode` on `PATH`, authenticated, with an **OpenCode Go** model entitled;
  `node` and `npm` to bundle the plugin; `lsof`.
- `claude` on `PATH`, authenticated (or `CLAUDE_CODE_OAUTH_TOKEN`).
- Go toolchain, `python3`, `curl`; the script builds `ctxed` from the checkout.

Configuration (optional, with defaults):

```sh
CTXED_TEST_OPENCODE_MODEL=opencode-go/deepseek-v4-flash
CTXED_TEST_CLAUDE_MODEL=haiku
CTXED_TEST_KEEP=1        # keep scratch on failure
CTXED_TEST_EXTRA_PATH=   # prepended to PATH, for tools installed off-PATH
```

Nothing machine-specific is assumed: no hardcoded install paths, and no default
credentials. The OpenCode scenarios start their own scratch server and give it a
random password per run.

Isolation: every session a scenario creates is deleted afterwards (OpenCode:
the scratch server's sessions and the `opencode run` session; Claude Code: the
throwaway session's project directory under `~/.claude/projects`, unless
`--keep` and a failure). Nothing stored is mutated by ctxed; the only
harness-side mutations are Claude Code's own compaction, which is the feature
under test, and the self-config shim described above, which is restored on exit.

A scenario exercises only the checkout it runs from. **An absent feature fails**:
no check probes for a feature and skips, and nothing falls back to another
worktree or branch. A branch that genuinely lacks a feature edits the check.

## Repeatability across changes

Scenarios assert **outcomes**, so an existing scenario keeps protecting old
behavior after a new feature lands; a new feature adds a new scenario and its
offline pre-filter check. A scenario that starts failing is a behavior
regression — which is what we actually care about.

## Known limits (stated honestly)

- Topic **labels** and **grouping** are not asserted beyond the two planted
  topics landing in different rows; status is asserted on those two only.
- Token counts are estimates (four runes per token); the check is that they
  are consistent with `inspect`, not that they match the provider's count.
- The `ctxed-overview` skill itself (an agent choosing to run the command) is
  not driven by the suite; the suite runs the command the skill runs, in the
  environment the skill runs it in.
- A live Claude Code transcript is written as the session runs; the newest
  turn can be missing from an overview taken mid-turn.
