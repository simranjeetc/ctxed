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

The two harnesses need different prerequisites, models, and branches, and an
incident in one must not touch the other. So there are **two independent
verifiers**, each owning one harness and runnable in parallel.

| | OpenCode verifier | Claude Code verifier |
| --- | --- | --- |
| Entry point | `scripts/verify-functionally.sh --opencode` | `scripts/verify-functionally.sh --claude` |
| Harness | `opencode` (OpenCode Go entitled) | `claude` (Haiku) |
| Branch with the feature | `feat/opencode-dispatch-plugin` | `feat/claude-code-live-prune` |
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
   - the selected bucket's messages are absent from the request;
   - a message added **after** selection, in a dropped bucket, is also absent
     (anti-drift);
   - stored history is unchanged and the session continues;
   - **id parity** — the ids ctxed categorizes over are the ids the hook filters.
     If this fails, the plugin must match on content/tool-id — the top risk.
5. The categorizer stub sleeps ~3 s, like a real model, so the plugin's command
   timeout is genuinely exercised.

### OpenCode self-configuration — `--opencode` (second scenario)

A second scenario starts the server the way a real install does: **minimal PATH,
no `CTXED_PLUGIN_*` at all**. OpenCode's server runs as a launchd daemon with a
minimal PATH, and a local plugin cannot take options from `opencode.json`. The
plugin must resolve `ctxed` and a categorizer on its own, and its output must be
visible in the session. Without this scenario a plugin that only works when the
test supplies its configuration passes while the real install fails.

### Claude Code — `scripts/verify-functionally.sh --claude`

1. Create a throwaway session with `claude -p --output-format json`.
2. Locate its transcript; `inspect` it.
3. `categorize` → `compact-instruction` → the instruction sentence.
4. Drive compaction headlessly (`claude -p --resume <id> "/compact <instr>"`).
5. Assert the stored transcript changed, the session id is unchanged, and the
   session continues.
6. When the D3b mod lands, add the same assertions against an exact bucket drop.

## Reports (the verifier's contract)

Each verifier writes JSON via `--report <path>`:

```json
{ "suite": "opencode", "model": "opencode-go/deepseek-v4-flash",
  "passed": 5, "failed": 0, "ok": true,
  "checks": [ { "status": "pass", "name": "opencode:prune --ids-only", "detail": "" } ] }
```

The coordinator acts on `ok` and, when false, on the `checks` with `status:
"fail"`. `--keep` retains scratch sessions and logs for a failing run.

## Running it — including from a fresh checkout

Prerequisites, each reported by name if missing:

- `opencode` on `PATH`, authenticated, with an **OpenCode Go** model entitled.
- `claude` on `PATH`, authenticated (or `CLAUDE_CODE_OAUTH_TOKEN`).
- Go toolchain; the script builds `ctxed` from the checkout.

Configuration (optional, with defaults):

```sh
CTXED_TEST_OPENCODE_MODEL=opencode-go/deepseek-v4-flash
CTXED_TEST_CLAUDE_MODEL=haiku
CTXED_TEST_KEEP=1        # keep scratch on failure
```

Isolation: scratch sessions are created in a temp dir and deleted afterwards
(OpenCode: `opencode session delete`; Claude Code: a throwaway session id under
the real config dir, since credentials live there). Nothing stored is mutated by
ctxed; the only harness-side mutation is Claude Code's own compaction, which is
the feature under test.

If a checkout lacks the feature (e.g. `compact-instruction` on `main`), the
scenario auto-runs against the sibling worktree when present, else reports SKIP —
so the suite never gives a false failure.

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
