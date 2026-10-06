// ctxed OpenCode plugin: a silent filter.
//
// The prune itself happens outside the plugin: the `ctxed-prune` skill runs
// `ctxed opencode categorize`, asks the user, and runs `ctxed opencode drop`,
// which writes the session's drop list. Before each request this plugin reads
// that list and removes the listed messages, including from the request
// that writes a compaction summary. It posts nothing, registers no
// command, runs nothing, and keeps no state; any error sends the request as is.

import { appendFileSync } from "node:fs"
import { Plugin } from "@opencode/plugin"
import { filterDropped, readDropList, stateDir, type LiveMessage } from "./core.ts"

export const CtxedPrunePlugin = Plugin.define({
  id: "ctxed.prune",
  async setup(ctx) {
    const dir = stateDir(process.env)
    // Verifier-only: when set, record each request's ids before and after the
    // filter, so a test can check the drop without asking a model.
    const debugLog = process.env.CTXED_PLUGIN_DEBUG_LOG ?? ""

    // The same filter runs on the agent's requests ("context") and on the
    // request that writes a compaction summary ("compaction"); without the
    // second, a compaction summarises the dropped messages back in.
    const filter = (hook: string) => async (event: { sessionID?: unknown; messages: unknown }) => {
      try {
        const messages = event.messages as LiveMessage[]
        const list = readDropList(dir, String(event.sessionID ?? ""))
        const kept = list === undefined ? messages : filterDropped(messages, list)
        if (debugLog !== "") logDecision(debugLog, hook, messages, kept)
        if (kept.length !== messages.length) event.messages = kept
      } catch (error) {
        console.error(`ctxed-opencode: ${hook} filter failed; request sent unchanged`, error)
      }
    }
    await ctx.session.hook("context", filter("context"))
    await ctx.session.hook("compaction", filter("compaction"))
  },
})

function logDecision(path: string, hook: string, before: readonly LiveMessage[], after: readonly LiveMessage[]): void {
  try {
    const ids = (list: readonly LiveMessage[]) => list.map((m) => (typeof m.id === "string" ? m.id : null))
    appendFileSync(path, JSON.stringify({ hook, before: ids(before), after: ids(after) }) + "\n")
  } catch {
    // A debug log must never break a turn.
  }
}

export default CtxedPrunePlugin
