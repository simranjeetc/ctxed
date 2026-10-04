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

  // No grouping, selection, or structural-validity logic may live here; that
  // belongs to ctxed, which the plugin only calls.
  const forbidden = [/orphan/i, /validity/i, /classif/i, /tool[-_]?call/i, /tool[-_]?result/i]
  for (const pattern of forbidden) {
    assert.ok(!pattern.test(plugin), `plugin.ts must not contain ${pattern}`)
    assert.ok(!pattern.test(core), `core.ts must not contain ${pattern}`)
  }

  // The two seams the change requires.
  assert.ok(plugin.includes('session.hook("context"'), "plugin must bind the context hook")
  assert.ok(plugin.includes("command.transform"), "plugin must register the in-session command")
  assert.ok(core.includes("--ids-only"), "core must ask ctxed for the dropped id set")
  assert.ok(core.includes("droppedIds"), "core must parse droppedIds")
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
