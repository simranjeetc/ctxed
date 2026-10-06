# Tasks

## 1. Feasibility (stop and report if any fails)

- [ ] 1.1 Confirm OpenCode routes a session's requests to a custom `@ai-sdk/openai-compatible` provider `baseURL`, and Claude Code routes `claude -p` (including `/compact`) to `ANTHROPIC_BASE_URL`; record versions and findings in `docs/verification-strategy.md`

## 2. Fake provider

- [ ] 2.1 `scripts/fakeprovider/main.go`: OpenAI chat completions + Anthropic messages, streaming and non-streaming; log every request body as JSONL; 404 + log for unknown paths
- [ ] 2.2 Rule file: regex on last user text → text reply or tool-call reply; default `ok`
- [ ] 2.3 Go unit tests for both protocols (shape of responses, SSE framing, logging)

## 3. OpenCode scenarios on the wire

- [ ] 3.1 Scenario project config registers the fake provider; sessions select it
- [ ] 3.2 Every prompt carries a unique sentinel; helper `wire_after <seq>` returns request bodies after a sequence number
- [ ] 3.3 Hard checks: dropped sentinels absent and kept sentinels present in the next request; post-selection message present, even on the dropped topic; attachment content absent; tool result does not accumulate across requests
- [ ] 3.4 Self-config scenario uses the fake provider too
- [ ] 3.5 Debug log printed only as diagnostics on failure

## 4. Claude Code scenario on the wire

- [ ] 4.1 Run `claude` with `ANTHROPIC_BASE_URL` set to the fake provider (dummy API key; isolate from the user's OAuth if needed and record how)
- [ ] 4.2 Hard checks: the compaction request contains the exact instruction sentence; the first post-compact request has none of the pre-compact turns verbatim; the session continues under the same id
- [ ] 4.3 `--with-model`: run against the real model and report the steering metric (dropped sentinel present in summary) as a soft check

## 5. Acceptance

- [ ] 5.1 `scripts/verify-functionally.sh --all` passes with no model entitlement and no network beyond localhost
- [ ] 5.2 Break the plugin's filter (e.g. return messages unchanged) and confirm the OpenCode wire checks FAIL; revert
- [ ] 5.3 Update `docs/verification-strategy.md` (wire is the gate; prerequisites; `--with-model`)
- [ ] 5.4 `openspec validate add-wire-level-verification --strict` passes
