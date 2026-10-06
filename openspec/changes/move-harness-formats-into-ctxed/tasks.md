# Tasks

## 1. Live formats

- [ ] 1.1 Answer the design's anti-drift open question by reading `plugin/opencode/src/` and the verifier; record the answer in design.md before coding
- [ ] 1.2 Port `plugin/opencode/src/transcript.ts` to Go adapters `opencode-hook` and `opencode-context`; move its test fixtures to `testdata/live/` and assert identical output
- [ ] 1.3 Add `claude-mod` adapter (`handle` as id); fixtures from the spike's `SessionMessage` shape
- [ ] 1.4 `--from` on `categorize -` and `prune -`; usage error on an unknown value

## 2. Selection state

- [ ] 2.1 `internal/state`: atomic read/write of `categories.json` and `selection.json` under `$XDG_STATE_HOME/ctxed/<harness>/<session-id>/` (0700)
- [ ] 2.2 `ctxed select <harness> <session-id> --categories 1,3 | --clear | --show`; unknown category id refused with exit 3 (as `prune`)
- [ ] 2.3 `categorize --session-key` stores output; `prune --session-key` reads the stored selection; no selection → empty drop set, exit 0
- [ ] 2.4 `ctxed state gc --older-than <dur>`

## 3. Long-lived mode and versioning

- [ ] 3.1 `ctxed serve --stdio` with `categorize`, `select`, `prune` ops sharing the CLI handlers; Go tests for framing and errors
- [ ] 3.2 `ctxed version --json` with an integer `protocol`

## 4. Slim the OpenCode plugin

- [ ] 4.1 Plugin forwards live messages with `--from`, uses `--session-key opencode/<sessionID>`; delete `transcript.ts` and `writeCategoriesFile`
- [ ] 4.2 Plugin uses `serve --stdio` when available, CLI otherwise; checks `protocol` at load and reports a mismatch visibly in the session
- [ ] 4.3 Keep `plugin/opencode/test/no-policy.test.ts` green; extend its forbidden-pattern list with format/state terms
- [ ] 4.4 Rebuild `.opencode/plugins/ctxed-prune.js` and keep the bundle-sync check (add one to `scripts/verify.sh` if absent)

## 5. Verification and docs

- [ ] 5.1 Verifier reads selection via `ctxed select opencode <sid> --show` instead of globbing `$TMPDIR/ctxed-opencode`
- [ ] 5.2 Rewrite `docs/plugin-contract.md` around `--from`, `--session-key`, `serve`, and the protocol version
- [ ] 5.3 `go test ./...`, `scripts/verify.sh`, `scripts/verify-functionally.sh --opencode` (wire-level) pass; `openspec validate move-harness-formats-into-ctxed --strict` passes
