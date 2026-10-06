#!/bin/sh
# ctxed categorizer: read the prompt on stdin, ask an OpenCode Go model, print
# the response on stdout. ctxed's --categorizer-cmd contract is exactly this:
# prompt in on stdin, model response out on stdout, nothing else.
#
# Override the model with CTXED_CATEGORIZER_MODEL (default: an OpenCode Go model
# the machine is entitled to, the same one the functional gate pins).
set -eu

model="${CTXED_CATEGORIZER_MODEL:-opencode-go/deepseek-v4-flash}"
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
# parts, so just the model's response reaches ctxed.
"$oc_bin" run --format json --model "$model" "$prompt" |
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
'
