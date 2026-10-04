import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const here = dirname(fileURLToPath(import.meta.url))

function source(relative: string): string {
  return readFileSync(join(here, "..", relative), "utf8")
}

test("the plugin delegates: it carries no prune policy of its own", () => {
  const plugin = source("src/plugin.ts")
  const core = source("src/core.ts")

  // The plugin must not DECIDE anything prune-related: no grouping into buckets,
  // no selection resolution, no orphan/validity repair, no pairing of a tool call
  // with its result. Naming tool-call/tool-result as a shape it matches ids
  // against is fine — ctxed made the decision, the plugin only executes it.
  const forbidden = [
    /orphan/i,
    /validity/i,
    /classif/i,
    /toolCalls?\s*\(/, // a pairing/validation helper, not an id match
    /resultFor|callFor|pairTool/i,
  ]
  for (const pattern of forbidden) {
    assert.ok(!pattern.test(plugin), `plugin.ts must not contain ${pattern}`)
    assert.ok(!pattern.test(core), `core.ts must not contain ${pattern}`)
  }
  // It may only ever ask ctxed what to drop.
  assert.ok(!/\bcategoriz/i.test(plugin) || plugin.includes("categorizeLive"), "plugin.ts must not categorize itself")

  // The two seams the change requires.
  assert.ok(plugin.includes('session.hook("context"'), "plugin must bind the context hook")
  assert.ok(plugin.includes("command.transform"), "plugin must register the in-session command")
  assert.ok(core.includes("--ids-only"), "core must ask ctxed for the dropped id set")
  assert.ok(core.includes("droppedIds"), "core must parse droppedIds")
  // The tool-call ids come FROM ctxed, not from the plugin deciding anything.
  assert.ok(core.includes("droppedToolCallIds"), "core must consume ctxed's droppedToolCallIds")
})

test("the transcript module is a pure shape translation", () => {
  const transcript = source("src/transcript.ts")

  // It maps two live encodings to ctxed's document. It must make no prune
  // decision: no grouping into buckets, no orphan resolution, no validity
  // repair. Mentions of tool-call/tool-result are shape names, not policy.
  const forbidden = [/orphan/i, /validity/i, /bucket/i, /categoryIds/i, /droppedIds/i]
  for (const pattern of forbidden) {
    assert.ok(!pattern.test(transcript), `transcript.ts must not contain ${pattern}`)
  }
  // It really does the two mappings.
  assert.ok(transcript.includes("serializeHookMessages"), "must map the hook encoding")
  assert.ok(transcript.includes("serializeContextMessages"), "must map the context encoding")
})
