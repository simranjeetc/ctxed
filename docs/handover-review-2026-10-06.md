# Handover — review of 2026-10-06

Read this first if you are implementing the review's follow-ups in a fresh
session. Everything needed is on disk; no prior session history is required.

## Snapshot

- Reviewed: `main` at `022a826`. Line numbers in the changes refer to that
  commit; when they drift, find the code by function name.
- Both feature branches (`feat/opencode-dispatch-plugin`,
  `feat/claude-code-live-prune`) are fully merged into `main`. The worktrees
  `~/codebase/ctxed-oc` and `~/codebase/ctxed-cc` are stale; do not use them.

## What the review concluded

1. **Both harnesses are implemented, unequally.** OpenCode: an exact,
   in-session prune via `plugin/opencode/`. Claude Code: a best-effort flow
   via the `ctxed-prune-context` skill plus a pasted `/compact` sentence. The
   exact Claude Code path (D3b mod) is specified but not built; the spike shows
   it is buildable on a GA API.
2. **The verification strategy is right; the scripts don't fully honor it.**
   Several checks cannot fail, the Claude Code scenario asserts almost
   nothing, and the hard OpenCode evidence is the plugin's own log.
3. **The shared binary is shared only for policy.** Format translation,
   selection state and binary discovery leaked into the shims; the Claude flow
   depends on an OpenCode categorizer.

## Changes, in build order

| # | Change (`openspec/changes/…`) | Size | Depends on |
|---|---|---|---|
| 1 | `fix-verification-false-passes` | small | – |
| 2 | `claude-adapter-compaction-aware` | small | – |
| 3 | `add-wire-level-verification` | medium | 1 |
| 4 | `move-harness-formats-into-ctxed` | medium | 3 |
| 5 | `add-claude-code-live-prune` §2 (D3b, revised) | large | 2, 4 |
| 6 | `unify-ux-and-install` | medium | 4, 5 |

1 and 2 are independent and can run in parallel. 3 must land before 4 so the
refactor is guarded at the wire.

## How to work each change

```sh
openspec instructions apply --change <name> --json   # or /opsx-apply <name>
```

- Read the change's `proposal.md`, `design.md`, `tasks.md` and `specs/`.
- Tasks marked as investigation or feasibility come first; if one fails, stop
  and record the finding in the change's `design.md` instead of improvising.
- Each design lists **Open Questions**. Answer them from evidence (a real
  session, generated `.claude-plugin/types/`, the code) before building on them.
- Gate: `scripts/verify.sh`, then the relevant `scripts/verify-functionally.sh`
  flag, then `openspec validate <name> --strict`.

## Read before writing code

1. This file.
2. `docs/verification-strategy.md`: the verification contract.
3. `docs/claude-code-mods-spike.md`: the verified mod API (for #5 and #6).
4. `docs/plugin-contract.md`: the shim contract (rewritten by #4).
5. `docs/live-context-prune-decisions.md`: locked decisions; do not reopen D4.
