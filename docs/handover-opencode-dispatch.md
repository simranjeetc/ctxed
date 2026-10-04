# Handover — `add-opencode-dispatch-plugin`

Read this first if you are picking up the OpenCode live-prune work in a fresh
session. Everything needed is on disk; no prior session history is required.

## Where the work lives

- Worktree: `/Users/simran/codebase/ctxed-oc`
- Branch: `feat/opencode-dispatch-plugin`
- Head: `f010be5` (working tree clean)
- Change of record: `openspec/changes/add-opencode-dispatch-plugin/`
- Progress: **4/20 tasks** (1.1–1.4 done) — run
  `openspec instructions apply --change add-opencode-dispatch-plugin --json`

Nothing is merged or pushed. Sibling worktrees: `ctxed` (main),
`ctxed-cc` (`feat/claude-code-live-prune`, the Claude Code half).

## Read these before writing code

1. `docs/handover-opencode-dispatch.md` — this file.
2. `docs/opencode-plugin-spike.md` — **verified** v2 API surface, live-message
   id parity, and the operational gotchas. Do not re-derive; the spike ran a
   probe plugin against a real session.
3. `docs/live-context-prune-decisions.md` — locked decisions D1–D6 and evidence
   R1–R4. The design is settled; do not re-open it.
4. `docs/verification-strategy.md` + `docs/verifier-agents.md` — the verification
   contract and the two-verifier model.
5. `openspec/changes/add-opencode-dispatch-plugin/tasks.md` — tasks 2.x–4.x.

## What is already done

- **1.1–1.4** — `ctxed prune --ids-only` (resolved `{"droppedIds":[…]}`),
  `categorize`/`prune` read a transcript on **stdin** (`<session>` = `-`), docs
  updated. Commits `c052301`.
- **Spike** — `fd06fa6`. Proved: `session.hook("context")` fires; live message
  id == export `msg_…` id; the promise API (`@opencode/plugin`, pinned
  `2.0.22`) exposes `ctx.command.transform`, `ctx.storage`, and a mutable
  `event.messages`. Gotchas: local plugin needs its own `node_modules`, must
  default-export `{id, setup}`, and load failures are **silent** without
  `--print-logs --log-level debug`.
- **Lockfile** — `f010be5`. `plugin/opencode/package-lock.json` pins the tree so
  the plugin's deps are reproducible (`npm ci`).

## The remaining work (2.x–4.x)

Build, in order:

1. **2.1–2.4** — the in-session command. Use `ctx.command.transform(editor =>
   editor.add({ name, description, execute }))`. `execute({sessionID})` should:
   serialize live messages → `ctxed categorize -` → show **bucket labels** →
   read the selection → persist it via `ctx.storage.set(key, …)` keyed by
   session. Fail-open: a ctxed failure leaves the session unchanged and reports
   the error. Ids stay internal; the user never sees them.
2. **3.1–3.7** — the dispatch hook. On `session.hook("context")`: re-derive the
   dropped set over the **live** transcript via `ctxed prune - --ids-only` with
   the active selection, parse `droppedIds`, filter `event.messages` by id
   (order-preserving), fail-open on non-zero/hang/invalid JSON, cache keyed by
   selection + session revision, config from env/config, add the concrete
   example to `docs/plugin-contract.md`, and keep the plugin policy-free.
3. **4.1–4.5** — turn the `PENDING` block in `scripts/verify-functionally.sh
   --opencode` into real assertions: dropped bucket absent from the outgoing
   request, a post-selection message in a dropped bucket also absent
   (anti-drift), stored session unchanged, id parity, and
   `openspec validate … --strict`.

## Task 4.4 is already answered

The spike proved live ids equal export ids, so 4.4's fallback (content/tool-id
matching) is **not needed**. Mark 4.4 done with a pointer to
`docs/opencode-plugin-spike.md` after wiring the assertion, rather than
re-investigating.

## Gates — keep all three green

```sh
scripts/verify.sh          # offline pre-filter (build/vet/test/openspec/CLI/plugin)
scripts/verify-functionally.sh --opencode --report /tmp/oc.json
scripts/verify-functionally.sh --claude   --report /tmp/cc.json
```

`--opencode` pins `opencode-go/deepseek-v4-flash` and validates the model is
entitled before running. Both scripts must stay green on `main` and both feature
branches; feature-absent checks self-skip.

Run the plugin's load check with debug logging — a silent load failure otherwise
looks like success (spike §3).

## Environment notes

- OpenCode 2.0.19 at `/opt/homebrew/bin/opencode`; `node` at `/opt/homebrew/bin`;
  **`bun` is NOT installed** and `npx` is **not on PATH** — use `node`/`npm`.
- `claude` at `~/.local/bin/claude`. Go 1.27.1.
- OpenCode Go entitled models: `deepseek-v4-flash`, `glm-5.3-flash`,
  `mimo-v2.6-flash`. Copilot is not usable in OpenCode here.
- Do not touch the user's three real OpenCode sessions; scratch sessions are
  deleted after runs.

## Conventions

- `gofmt -w` new/most-changed `.go` files; keep `go vet ./...` clean.
- Write plausibly real commits; never `git add -A` (use paths).
- Keep responses terse (caveman mode is on).
