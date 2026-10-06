// OpenCode dispatch plugin: apply a ctxed prune to the outbound transcript.
//
// This file is deliberately thin. It registers three surfaces:
//
//   - an in-session command that categorizes the live conversation via ctxed,
//     presents the buckets, and records the user's bucket selection;
//   - a `prompt` hook that lets the user answer the listed buckets with just
//     their numbers, so a second command invocation is not needed; and
//   - a dispatch hook on OpenCode's `context` seam that re-derives the dropped
//     set over the live transcript and removes matching messages by id.
//
// It never decides what to drop — that is ctxed's job. Its only job beyond
// exec/present/substitute is translating the two live message encodings into
// ctxed's document shape (see transcript.ts).

import { appendFileSync, existsSync, mkdirSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { fileURLToPath } from "node:url"

import { Plugin } from "@opencode/plugin"

import {
  applyPrune,
  categorizeLive,
  commandRunner,
  defaultRunner,
  filterBucketIds,
  formatBuckets,
  formatSelectedLabels,
  loadConfig,
  parseBareSelection,
  parseSelection,
  parseSelectionInput,
  PruneCache,
  resolveCtxedPath,
  selectionKey,
  sessionRevision,
  type Bucket,
  type CategorizeResult,
  type LiveMessage,
} from "./core.ts"
import { serializeContextMessages, transcriptJson, type ContextMessage } from "./transcript.ts"

export const COMMAND_NAME = "ctxed-prune"

export const CtxedPrunePlugin = Plugin.define({
  id: "ctxed.prune",
  async setup(ctx) {
    const config = loadConfig(ctx.options as Record<string, unknown>, process.env)

    // Self-configure for a machine where OpenCode's server has a minimal PATH
    // and no plugin env: resolve the ctxed binary to a real location, and fall
    // back to the categorizer script shipped beside this plugin.
    config.ctxedPath = resolveCtxedPath(config.ctxedPath)
    if (config.categorizerCmd === "" && config.categorizerModel === "") {
      const bundled = resolveBundledCategorizer()
      if (bundled !== "") config.categorizerCmd = bundled
    }

    const cache = new PruneCache()
    const run = defaultRunner(config)
    // The command calls a model to categorize; give it its own longer budget.
    const runCommand = commandRunner(config)

    const report = (message: string, error?: unknown): void => {
      if (error === undefined) console.error(message)
      else console.error(message, error)
    }

    // A command's execute returns void, so console.log is invisible to the user
    // — it lands in the server's stdout, not the session. Surface anything the
    // user must see as a synthetic session message instead. Fall back to the log
    // if that call fails, so output is never silently lost.
    const say = async (sessionID: string, text: string): Promise<void> => {
      try {
        await ctx.session.synthetic({ sessionID, text })
      } catch (error) {
        report(`ctxed-opencode: could not surface output: ${text}`, error)
      }
    }

    // Buckets from the last `/ctxed-prune` listing, per session, so the user can
    // answer with just the numbers instead of retyping the command. Held in
    // memory (not storage): a pending choice is about the conversation on screen
    // right now, and should not survive a restart.
    const pending = new Map<string, PendingBuckets>()
    const PENDING_TTL_MS = 10 * 60 * 1000

    const rememberPending = (sessionID: string, result: CategorizeResult): void => {
      pending.set(sessionID, {
        categoriesJson: result.categoriesJson,
        buckets: result.buckets,
        createdAt: Date.now(),
      })
    }

    // The offer stands until it is used or expires. It deliberately survives
    // other messages: a queued prompt admitted late, or a question asked before
    // answering, must not silently discard the listing the user is looking at.
    // `createdAt` bounds how long the offer stands.
    const pendingFor = (sessionID: string): PendingBuckets | undefined => {
      const entry = pending.get(sessionID)
      if (entry === undefined) return undefined
      if (Date.now() - entry.createdAt > PENDING_TTL_MS) {
        pending.delete(sessionID)
        return undefined
      }
      return entry
    }

    // Reading to consume: used when the user explicitly answers with ids.
    const takePending = (sessionID: string): PendingBuckets | undefined => {
      const entry = pendingFor(sessionID)
      if (entry !== undefined) pending.delete(sessionID)
      return entry
    }

    // Records a selection and makes the dispatch hook apply it. Returns the
    // human label of what was selected, or undefined when no id names a bucket.
    const applySelection = async (
      sessionID: string,
      categoriesJson: string,
      buckets: readonly Bucket[],
      ids: readonly string[],
    ): Promise<string | undefined> => {
      const valid = filterBucketIds(buckets, ids)
      if (valid.length === 0) return undefined
      const categoriesFile = writeCategoriesFile(sessionID, categoriesJson)
      await ctx.storage.set(selectionKey(sessionID), {
        categoriesFile,
        categoryIds: valid.join(","),
        selectedAt: new Date().toISOString(),
      })
      cache.clear()
      pending.delete(sessionID)
      return formatSelectedLabels(buckets, valid)
    }

    // In-session command: `/ctxed-prune` lists buckets; `/ctxed-prune 1,3`
    // records a selection. A ctxed failure reports and leaves the session
    // unchanged (fail-open).
    await ctx.command.transform((editor) => {
      editor.add({
        name: COMMAND_NAME,
        description: "Prune the session's context by topic bucket (ctxed).",
        async execute({ sessionID, prompt }) {
          const requested = parseSelectionInput(commandText(prompt))

          // Fast path: the buckets were already listed and the user is answering
          // with ids (`/ctxed-prune 3`). Reuse them instead of calling the model
          // a second time.
          if (requested.length > 0) {
            const carried = takePending(sessionID)
            if (carried !== undefined) {
              const carriedLabels = await applySelection(sessionID, carried.categoriesJson, carried.buckets, requested)
              if (carriedLabels !== undefined) {
                await say(sessionID, `Selected ${carriedLabels}; dropped on every subsequent dispatch.`)
                return
              }
            }
          }

          const messages = await sessionMessages(ctx as never, sessionID)
          if (messages.length === 0) {
            await say(sessionID, `No session messages to categorize yet; send a message, then /${COMMAND_NAME}.`)
            return
          }

          // Say what is happening before the slow part. Categorizing calls a
          // model and can take many seconds; a command that shows nothing until
          // it finishes reads as "nothing happened".
          await say(sessionID, `Categorizing this session… (a model call; a long session can take ~30s)`)

          const result = await categorizeLive({
            transcript: JSON.stringify(serializeContextMessages(messages)),
            config,
            run: runCommand,
          })
          if (result.error !== undefined) {
            await say(sessionID, `ctxed-opencode: categorize failed: ${result.error}`)
            return
          }

          if (requested.length === 0) {
            rememberPending(sessionID, result)
            await say(
              sessionID,
              `${formatBuckets(result.buckets)}\nReply with the bucket numbers to drop (e.g. 3 or 3,4), or run /${COMMAND_NAME} 3.`,
            )
            return
          }

          const labels = await applySelection(sessionID, result.categoriesJson, result.buckets, requested)
          await say(
            sessionID,
            labels === undefined
              ? `No bucket matched ${requested.join(",")}; nothing dropped.`
              : `Selected ${labels}; dropped on every subsequent dispatch.`,
          )
        },
      })
    })

    // Bare-number reply: the user saw the buckets from `/ctxed-prune` and answers
    // with just the numbers. Apply the pending buckets (no second model call) and
    // rewrite the message so the model sees what was dropped, not a stray "3".
    await ctx.session.hook("prompt", async (event) => {
      try {
        const sessionID = String((event as { sessionID?: unknown }).sessionID ?? "")
        if (sessionID === "") return

        // Peek, do not consume: a message that is not a selection leaves the
        // offer standing, so the user can ask something first and answer after.
        const carried = pendingFor(sessionID)
        if (carried === undefined) return

        const ids = parseBareSelection(commandText(event.prompt))
        if (ids.length === 0) return

        const labels = await applySelection(sessionID, carried.categoriesJson, carried.buckets, ids)
        if (labels === undefined) return
        event.prompt.text = `ctxed dropped ${labels} from context. Continue.`
      } catch (error) {
        // A hook must never break admission; fail open and report.
        report("ctxed-opencode: selection hook failed", error)
      }
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
 * The categorizer script that ships beside this plugin. An install at
 * `<repo>/.opencode/plugins/ctxed-prune.js` finds `<repo>/scripts/…` two levels
 * up; a copy elsewhere may not, in which case the caller configures one.
 * Returns "" when no candidate exists, so the caller can fall back to a model.
 */
function resolveBundledCategorizer(): string {
  const candidates = [
    "../../scripts/ctxed-categorizer-opencode.sh",
    "../../../scripts/ctxed-categorizer-opencode.sh",
  ]
  for (const relative of candidates) {
    try {
      const path = fileURLToPath(new URL(relative, import.meta.url))
      if (existsSync(path)) return path
    } catch {
      // Try the next candidate.
    }
  }
  return ""
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
 * Buckets listed by the last `/ctxed-prune`, held until the user answers with
 * the numbers (or moves on). `createdAt` bounds how long that offer stands.
 */
interface PendingBuckets {
  categoriesJson: string
  buckets: Bucket[]
  createdAt: number
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
