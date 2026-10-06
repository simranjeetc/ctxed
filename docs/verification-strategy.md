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
- **Assertions are on outcomes** a user can see: the session shrank, the session
  id is unchanged, stored history is untouched, the session continues.

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

### OpenCode — `scripts/verify-functionally.sh --opencode`

1. Create a throwaway session with `opencode run` and a canned prompt.
2. Export it; `ctxed inspect` and `categorize` it (offline categorizer: the test
   feeds ctxed the real entry ids, so no model call is needed for this half).
3. Resolve a bucket selection to dropped ids via `prune --ids-only`.
4. Run the in-session command and the dispatch hook, and assert:
   - **the buckets are visible in the session** — asserted on the session's
     messages, not the server log. A command's `console.log` lands in the server
     log, so asserting there passes even when the user sees nothing;
   - the selected bucket's messages are absent from the request (by id, from
     the plugin's dispatch log);
   - the message carrying a file attachment is in the dropped bucket and absent
     from the request;
   - a message added **after** the selection is kept, even on the dropped
     topic, while the selected messages stay dropped in the same dispatch (by
     id). A selection covers only the messages it was made over;
   - stored history is unchanged and the session continues;
   - **id parity** — the ids ctxed categorizes over are the ids the hook filters.
     If this fails, the plugin must match on content/tool-id — the top risk.
5. **Negative control** — the exact-drop check runs again against the same
   dispatch with no selection applied (the transcript the hook received) and
   must fail there. A check that cannot fail is not a check.
6. Model-recall questions ("what was in the attached file?", "what is the code
   word?") are **soft** (see Reports). Each uses its own `NONE-<n>` token and
   reads only the reply to that question.
7. The categorizer stub sleeps ~3 s, like a real model, so the plugin's command
   timeout is genuinely exercised.
8. Every turn waits for the session to go idle (OpenCode's active-session and
   inbox state), with a hard timeout that names the step. No fixed sleeps.

### OpenCode self-configuration — `--opencode` (second scenario)

A second scenario starts the server the way a real install does: **minimal PATH,
no `CTXED_PLUGIN_*` at all**. OpenCode's server runs as a launchd daemon with a
minimal PATH, and a local plugin cannot take options from `opencode.json`. The
plugin must resolve `ctxed` and a categorizer on its own, and its output must be
visible in the session. Without this scenario a plugin that only works when the
test supplies its configuration passes while the real install fails.

To make the plugin run this checkout's `ctxed`, the scenario puts a shim for the
freshly built binary at the first location the plugin searches (`~/go/bin/ctxed`,
read from `resolveCtxedPath`), moves any existing file aside, and restores it on
exit, including on failure and Ctrl-C. The shim records each call, so the
scenario asserts the plugin resolved that path.

### Claude Code — `scripts/verify-functionally.sh --claude`

1. Create a throwaway session with `claude -p --output-format json`.
2. Locate its transcript; `inspect` it.
3. `categorize` → `compact-instruction` → the instruction sentence.
4. Drive compaction headlessly (`claude -p --resume <id> "/compact <instr>"`).
5. Assert compaction happened, on what it writes: exactly one new
   `compact_boundary` line whose `compactMetadata.postTokens < preTokens`,
   followed by an `isCompactSummary` entry. (Compaction appends, so an entry
   count always changes and cannot be the check.) Then assert `ctxed inspect`
   reads the live context (summary first), the session id is unchanged, and the
   session continues.
6. When the D3b mod lands, add the same assertions against an exact bucket drop.

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

- Categorizer **quality** (are the buckets good?) is not asserted — only that the
  flow runs and the selection is honored.
- Claude Code's `/compact` is model-mediated: the scenario asserts the session
  changed and continued, not that a specific message survived verbatim. The D3b
  mod removes that dependency once built.
- The OpenCode dispatch assertions (4.1–4.4) are pending the in-session command.
