---
name: ctxed-overview
description: Show what this session's context is made of — topics with message counts, estimated tokens, share of context, done or in progress, and what is still pending. Read-only. Use when the user says "ctxed overview", "/ctxed-overview", "what's in my context", "what is taking context", "context breakdown", or asks how full the session is.
allowed-tools: Bash(ctxed:*), Bash(~/go/bin/ctxed:*)
license: MIT
compatibility: Claude Code or OpenCode, with the ctxed CLI on PATH (or at ~/go/bin/ctxed).
metadata:
  author: ctxed
  version: "1.0"
---

# ctxed overview

Shows the user what the model sees in **this** session, split into topics. It
changes nothing.

## Steps

1. Run, with no arguments (ctxed finds this session itself):

   ```sh
   ctxed overview
   ```

   If `ctxed` is not found, run `~/go/bin/ctxed overview`. It takes from a few
   seconds to about a minute: one cheap model call names the topics.

2. Show the output to the user **exactly as printed**, in a code block. Do not
   reword, re-sort, merge or summarise it.

3. After the block, add at most one short line, only if useful: for example
   that the largest row is the compaction summary, or that the counts are
   estimates. Then stop.

## Rules

- Run only `ctxed overview`. Do not run other ctxed commands, `/compact`, or
  anything that changes the session.
- Do not suggest or carry out dropping topics; deciding what to do next is the
  user's call.
- If ctxed exits with an error, show the error as printed and stop. If it prints
  "topics unavailable", the sizes are still correct; say so in one line.
- The transcript can lag the conversation by a turn; the newest message may be
  missing from the counts.
