#!/bin/sh
# ctxed categorizer: read the prompt on stdin, ask an OpenCode Go model, print
# the response on stdout. ctxed's --categorizer-cmd contract is exactly this:
# prompt in on stdin, model response out on stdout, nothing else.
#
# No literal model pin here. ctxed resolves the categorizer model itself
# (--model, $CTXED_CATEGORIZER_MODEL, $CTXED_MODEL, the session's own model, the
# harness's configured default), so a hardcoded provider/model here could
# outlive the provider's catalog. When the caller sets CTXED_CATEGORIZER_MODEL
# we pass it through; when it is unset, `opencode run` falls back to the
# machine's configured default model.
set -eu

prompt="$(cat)"

# Resolve the opencode binary. OpenCode's own server runs with a minimal PATH
# (/usr/bin:/bin:…) that omits Homebrew and ~/.opencode, so a bare `opencode`
# lookup fails when this script is spawned from a plugin. Fall back to the
# usual install locations.
oc_bin="$(command -v opencode 2>/dev/null || true)"
if [ -z "$oc_bin" ]; then
  for candidate in /opt/homebrew/bin/opencode "$HOME/.opencode/bin/opencode" /usr/local/bin/opencode; do
    if [ -x "$candidate" ]; then oc_bin="$candidate"; break; fi
  done
fi
if [ -z "$oc_bin" ]; then
  echo "ctxed-categorizer: opencode binary not found on PATH" >&2
  exit 127
fi

# `opencode run` takes the message as an argument, not on stdin, and its default
# output carries a banner. Ask for NDJSON (--format json) and keep only the text
# parts, so just the model's response reaches ctxed. Pass --model only when the
# caller set CTXED_CATEGORIZER_MODEL; otherwise let OpenCode use its default.
set -- run --format json
if [ -n "${CTXED_CATEGORIZER_MODEL:-}" ]; then
  set -- "$@" --model "$CTXED_CATEGORIZER_MODEL"
fi

# Run the harness first, with its stdout and stderr captured to files, so we can
# observe the command's real exit status. A pipeline (`opencode … | filter`)
# would hide it: plain POSIX sh has no `pipefail`, so a failed `opencode run`
# (e.g. an unknown model) would be masked by the successful filter and the
# script would "succeed" with empty output.
raw_out="$(mktemp "${TMPDIR:-/tmp}/ctxed-cat-out.XXXXXX")" || {
  echo "ctxed-categorizer: cannot create temporary file" >&2
  exit 1
}
raw_err="$(mktemp "${TMPDIR:-/tmp}/ctxed-cat-err.XXXXXX")" || {
  rm -f "$raw_out"
  echo "ctxed-categorizer: cannot create temporary file" >&2
  exit 1
}
text_out="$(mktemp "${TMPDIR:-/tmp}/ctxed-cat-txt.XXXXXX")" || {
  rm -f "$raw_out" "$raw_err"
  echo "ctxed-categorizer: cannot create temporary file" >&2
  exit 1
}
trap 'rm -f "$raw_out" "$raw_err" "$text_out"' 0

status=0
"$oc_bin" "$@" "$prompt" >"$raw_out" 2>"$raw_err" || status=$?
if [ "$status" -ne 0 ]; then
  echo "ctxed-categorizer: opencode run failed (exit $status)" >&2
  # Surface whatever the harness said so the failure is diagnosable. The
  # harness reports some errors (e.g. an unknown model) on stdout.
  if [ -s "$raw_err" ]; then cat "$raw_err" >&2; fi
  if [ -s "$raw_out" ]; then cat "$raw_out" >&2; fi
  exit "$status"
fi

# Keep only the model's text parts. Write to a file so the response reaches
# stdout byte-for-byte.
python3 -c '
import sys, json
out = []
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        ev = json.loads(line)
    except Exception:
        continue
    part = ev.get("part") or {}
    if ev.get("type") == "text" and part.get("type") == "text":
        out.append(part.get("text", ""))
sys.stdout.write("".join(out))
' <"$raw_out" >"$text_out"

# No text back at all is also a failure, not an empty success.
if [ ! -s "$text_out" ]; then
  echo "ctxed-categorizer: model returned no text" >&2
  if [ -s "$raw_err" ]; then cat "$raw_err" >&2; fi
  if [ -s "$raw_out" ]; then cat "$raw_out" >&2; fi
  exit 1
fi

cat "$text_out"
