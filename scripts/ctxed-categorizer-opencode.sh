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

# `opencode run` takes the message as an argument, not on stdin, and its default
# output carries a banner. Ask for NDJSON (--format json) and keep only the text
# parts, so just the model's response reaches ctxed.
opencode run --format json --model "$model" "$prompt" |
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
