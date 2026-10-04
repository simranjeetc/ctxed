// OpenCode dispatch plugin: apply a ctxed prune to the outbound transcript.
//
// This file is deliberately thin. It registers one hook on OpenCode's
// `context` seam, hands the live messages to the core module, and writes the
// filtered list back. It never decides what to drop — that is ctxed's job.

import { Plugin } from "@opencode/plugin"
import {
  applyPrune,
  defaultRunner,
  loadConfig,
  PruneCache,
  sessionRevision,
  type LiveMessage,
} from "./core.ts"

export const CtxedPrunePlugin = Plugin.define({
  id: "ctxed.prune",
  async setup(ctx) {
    const config = loadConfig(ctx.options as Record<string, unknown>, process.env)
    if (!config.enabled) return

    const cache = new PruneCache()
    const run = defaultRunner(config)

    await ctx.session.hook("context", async (event) => {
      const messages = event.messages as unknown as LiveMessage[]
      const result = await applyPrune({
        messages,
        config,
        revision: sessionRevision(messages),
        cache,
        run,
        report: (message, error) => {
          if (error === undefined) console.error(message)
          else console.error(message, error)
        },
      })
      if (result.dropped > 0) {
        event.messages = result.messages as typeof event.messages
      }
    })
  },
})

export default CtxedPrunePlugin
