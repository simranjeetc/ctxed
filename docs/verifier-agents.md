# Verifier agent briefs

Thin wrappers over `scripts/verify-functionally.sh`. The script is the source of
truth; these briefs only say which flag to run and where to put the report, so a
checkout without any agent setup still has the full capability.

Dispatch one verifier per harness. They are independent and can run in parallel.

---

## Verifier: OpenCode

```
Goal: functionally verify the OpenCode dispatch prune against a live session.

Run from the checkout under test. The script builds and exercises only that
checkout; a feature missing from it is a failure, not a skip:

    scripts/verify-functionally.sh --opencode --report /tmp/verify-opencode.json

Prerequisites (the script reports any that are missing, by name):
  - `opencode` on PATH, authenticated, with an OpenCode Go model entitled
  - node and npm (to bundle the plugin), Go toolchain, python3, curl, lsof
Model default: opencode-go/deepseek-v4-flash (override: CTXED_TEST_OPENCODE_MODEL)
Tools off PATH: set CTXED_TEST_EXTRA_PATH.

Then: read /tmp/verify-opencode.json and report `ok`, the model used, and any
check with status "fail" (name + detail). List "soft-fail" checks separately:
they are model-recall signals and do not affect `ok`.

Do not modify repo files. Do not merge branches. On failure, re-run with
CTXED_TEST_KEEP=1 and report the kept scratch paths.
```

---

## Verifier: Claude Code

```
Goal: functionally verify the Claude Code /compact live prune against a real
session.

Run:

    scripts/verify-functionally.sh --claude --report /tmp/verify-claude.json

Prerequisites (reported by name if missing):
  - `claude` on PATH, authenticated (or CLAUDE_CODE_OAUTH_TOKEN)
  - Go toolchain, python3, curl
Model default: haiku (override: CTXED_TEST_CLAUDE_MODEL)
Tools off PATH: set CTXED_TEST_EXTRA_PATH.

Then: read /tmp/verify-claude.json and report `ok`, the model used, and any
check with status "fail" (name + detail).

Do not modify repo files. Do not merge branches. On failure, re-run with
CTXED_TEST_KEEP=1 and report the kept scratch paths.
```

---

## Coordinator checklist

1. `scripts/verify.sh` — offline pre-filter. Stop if it fails.
2. Dispatch both verifiers (parallel).
3. Read both reports; require `ok: true` on the suite(s) a change touches.
4. Record the model and the checks that ran, so a later regression is comparable.
