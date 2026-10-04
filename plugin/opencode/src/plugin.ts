// OpenCode dispatch plugin: apply a ctxed prune to the outbound transcript.
//
// This file is deliberately thin. It registers two surfaces:
//
//   - an in-session command that categorizes the live conversation via ctxed,
//     presents the buckets, and records the user's bucket selection; and
//   - a dispatch hook on OpenCode's `context` seam that re-derives the dropped
//     set over the live transcript and removes matching messages by id.
//
// It never decides what to drop — that is ctxed's job. Its only job beyond
// exec/present/substitute is translating the two live message encodings into
// ctxed's document shape (see transcript.ts).

import { appendFileSync, mkdirSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"

import { Plugin } from "@opencode/plugin"

import {
  applyPrune,
  categorizeLive,
  defaultRunner,
  formatBuckets,
  loadConfig,
  parseSelection,
  parseSelectionInput,
  PruneCache,
  selectionKey,
  sessionRevision,
  type LiveMessage,
} from "./core.ts"
import { serializeContextMessages, transcriptJson, type ContextMessage } from "./transcript.ts"

export const COMMAND_NAME = "ctxed-prune"

export const CtxedPrunePlugin = Plugin.define({
  id: "ctxed.prune",
  async setup(ctx) {
    const config = loadConfig(ctx.options as Record<string, unknown>, process.env)

    const cache = new PruneCache()
    const run = defaultRunner(config)

    const report = (message: string, error?: unknown): void => {
      if (error === undefined) console.error(message)
      else console.error(message, error)
    }

    // In-session command: `/ctxed-prune` lists buckets; `/ctxed-prune 1,3`
    // records a selection. A ctxed failure reports and leaves the session
    // unchanged (fail-open).
    await ctx.command.transform((editor) => {
      editor.add({
        name: COMMAND_NAME,
        description: "Prune the session's context by topic bucket (ctxed).",
        async execute({ sessionID, prompt }) {
          const messages = await sessionMessages(ctx as never, sessionID)
          if (messages.length === 0) {
            console.log(`No session messages to categorize yet; send a message, then /${COMMAND_NAME}.`)
            return
          }

          const result = await categorizeLive({
            transcript: JSON.stringify(serializeContextMessages(messages)),
            config,
            run,
          })
          if (result.error !== undefined) {
            report(`ctxed-opencode: categorize failed: ${result.error}`)
            return
          }

          const selected = parseSelectionInput(commandText(prompt))
          if (selected.length === 0) {
            console.log(formatBuckets(result.buckets))
            console.log(`Select buckets with /${COMMAND_NAME} <ids> (e.g. /${COMMAND_NAME} 1,3).`)
            return
          }

          const categoriesFile = writeCategoriesFile(sessionID, result.categoriesJson)
          const categoryIds = selected.join(",")
          await ctx.storage.set(selectionKey(sessionID), {
            categoriesFile,
            categoryIds,
            selectedAt: new Date().toISOString(),
          })
          cache.clear()
          console.log(`Selected buckets ${categoryIds}; dropped on every subsequent dispatch.`)
        },
      })
    })

    // Dispatch hook: apply the recorded selection to the live transcript.
    await ctx.session.hook("context", async (event) => {
      const messages = event.messages as unknown as LiveMessage[]
      if (messages.length === 0) return

      const sessionID = String((event as { sessionID?: unknown }).sessionID ?? "")
      const selection = parseSelection(await ctx.storage.get(selectionKey(sessionID)))
      if (selection === undefined) return

      const result = await applyPrune({
        messages,
        config,
        selection,
        revision: sessionRevision(messages),
        cache,
        run,
        report,
      })
      // An opt-in decision log for verification: when set, record what the hook
      // decided so a test can assert on the drop deterministically instead of
      // asking a model what it remembers. Off unless the env var is set.
      if (config.debugLog !== "") {
        debugDecide(config.debugLog, messages, result)
      }
      if (result.dropped > 0) {
        event.messages = result.messages as typeof event.messages
      }
    })
  },
})

/**
 * Appends one JSON line describing what the hook decided: the ids it saw, the
 * ids it kept, and the ids it dropped. Opt-in (set CTXED_PLUGIN_DEBUG_LOG), used
 * by the functional verifier so a drop can be asserted deterministically rather
 * than by asking a model what it remembers.
 */
function debugDecide(logPath: string, messages: readonly LiveMessage[], result: { messages: LiveMessage[] }): void {
  try {
    const ids = (list: readonly LiveMessage[]) => list.map((m) => (typeof m.id === "string" ? m.id : null))
    appendFileSync(logPath, JSON.stringify({ before: ids(messages), after: ids(result.messages) }) + "\n")
  } catch {
    // A debug log must never break a turn.
  }
}

/**
 * Reads the command arguments. OpenCode hands them as a `Prompt` object with a
 * `text` field; a bare string is accepted too, so the helper is total.
 */
function commandText(prompt: unknown): string {
  if (typeof prompt === "string") return prompt
  if (prompt !== null && typeof prompt === "object") {
    const text = (prompt as { text?: unknown }).text
    if (typeof text === "string") return text
  }
  return ""
}

/**
 * Reads a session's messages through the plugin API. Returns an empty list on
 * any failure; the command path treats that as "nothing to categorize", not an
 * error.
 */
async function sessionMessages(
  ctx: { session: { context: (input: { sessionID: string }) => Promise<unknown> } },
  sessionID: string,
): Promise<ContextMessage[]> {
  try {
    const messages = await ctx.session.context({ sessionID })
    return Array.isArray(messages) ? (messages as ContextMessage[]) : []
  } catch {
    return []
  }
}

/**
 * Writes the categories file ctxed produced to a real path, so the dispatch
 * hook can pass it to `ctxed prune --categories-file`. Kept under the OS temp
 * dir, keyed by session, and overwritten on each new selection.
 */
function writeCategoriesFile(sessionID: string, categoriesJson: string): string {
  const dir = join(tmpdir(), "ctxed-opencode")
  mkdirSync(dir, { recursive: true })
  const path = join(dir, `${sessionID}.categories.json`)
  writeFileSync(path, categoriesJson, "utf8")
  return path
}

export default CtxedPrunePlugin
