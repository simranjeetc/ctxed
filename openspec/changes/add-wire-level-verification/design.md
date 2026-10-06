# Design

## Context

Both harnesses accept a custom model endpoint:

- OpenCode: a provider entry in `opencode.json` with
  `npm: "@ai-sdk/openai-compatible"` and `options.baseURL`, selected per session
  via `POST /api/session/{id}/model` (already used by the scenario).
- Claude Code: `ANTHROPIC_BASE_URL` (and a dummy `ANTHROPIC_API_KEY`) in the
  environment of the `claude` process.

Verify both claims on the installed versions before building (task 1.1).

## Decisions

### One Go binary, two protocols

`scripts/fakeprovider` listens on a free port and serves:

- `POST /v1/chat/completions` (OpenAI-compatible), streaming (SSE) and
  non-streaming.
- `POST /v1/messages` (Anthropic), streaming (SSE) and non-streaming.
- Any other path: 404 and a log line, so an unexpected endpoint (e.g. a token
  counting or model-list call) is visible rather than silently failing. Add
  handlers as the harnesses require them.

Each request body is appended, unmodified, as one line to `$LOG` with a
monotonic sequence number and the path.

Replies: a scripted reply when the last user text matches a rule file
(`--rules rules.json`, regex → reply), otherwise `ok`. A tool-call reply is
supported so the "tool result does not accumulate" check stays meaningful.

Go is chosen because the repo already requires the toolchain; no Python or Node
dependency is added.

### Sentinels are the assertion unit

Every prompt in a scenario embeds a unique sentinel (`S-ALPHA-1-<pid>` …). The
assertion "bucket dropped" is: no request after the selection contains any
dropped sentinel anywhere in its body; every kept sentinel is present in the
next request. This is format-agnostic, so it survives harness message-shape
changes.

### Claude Code compaction under a fake model

Compaction itself is a model call. The fake provider recognizes it (the request
contains the ctxed instruction text) and replies with a fixed summary that
contains only the kept sentinels. The hard assertions are about mechanics:

1. The compaction request contains the exact `compact-instruction` sentence.
2. The first request after compaction does not contain the dropped turns
   verbatim (it contains the fake summary instead).
3. The session continues under the same id.

Steering quality (does a *real* model drop the topic?) is measured only with
`--with-model` and reported as a metric (`dropped sentinel present in summary:
yes/no`), never a gate.

### Debug log demoted

`CTXED_PLUGIN_DEBUG_LOG` stays as a diagnostic printed on failure. It is no
longer the evidence for any hard check.

## Open Questions

- Does Claude Code send any request to the base URL other than `/v1/messages`
  in `-p` mode (e.g. count_tokens)? Handle what the 404 log reveals.
- Does OpenCode's `@ai-sdk/openai-compatible` provider need a models endpoint?
- Does `claude -p --resume <id> "/compact …"` run compaction through the same
  base URL? (Expected yes.)

## Non-Goals

- Testing model quality.
- Replacing offline unit tests.
