---
name: ctxed-prune
description: Drop whole topics from this session's context so the conversation continues without them. Use when the user says "prune", "ctxed prune", "/ctxed-prune", "drop topics", or asks to clean up or shrink the context of the current session.
---

# ctxed-prune

Removes topics the user picks from what the model sees in **this** session.
`ctxed` sorts the conversation into topics and records the user's choice; the
ctxed OpenCode plugin then leaves those messages out of every later request.
The saved session is never changed.

## Steps

1. Run, with no arguments (OpenCode sets `OPENCODE_SESSION_ID` for you):

   ```sh
   ctxed opencode categorize
   ```

   It calls a model and can take up to a minute on a long session. If `ctxed`
   is not found, use `~/go/bin/ctxed`.
   - If it prints "Nothing to prune", tell the user that and stop.
   - If it fails, show the error in one line and stop.

2. Ask the user which topics to drop. Use the `question` tool with
   `multiple: true`: one option per listed topic, labelled `<number>. <label>`,
   with the message and token counts as the description. Ask: "Which topics
   should be dropped from the context?" If the `question` tool is not available,
   show the list and ask for the numbers.

3. If the user picks nothing, say that nothing was dropped and stop.

4. Run with the chosen numbers, comma-separated:

   ```sh
   ctxed opencode drop 1,3
   ```

   Report its first line to the user, then continue with whatever they ask next.

## Rules

- Never pick, recommend, or rank topics yourself; the choice is the user's.
- Do not summarise or repeat the content of a dropped topic.
- Run only these two commands; do not edit any file.
- Messages sent after the prune are kept, even on a dropped topic. To drop
  them too, the user prunes again; already-dropped messages are not listed.
