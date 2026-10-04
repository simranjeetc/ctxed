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
  const forbidden = [/orphan/i, /validity/i, /classify/i, /tool[-_]?call/i, /tool[-_]?result/i]
  for (const pattern of forbidden) {
    assert.ok(!pattern.test(plugin), `plugin.ts must not contain ${pattern}`)
    assert.ok(!pattern.test(core), `core.ts must not contain ${pattern}`)
  }
  assert.ok(!/categoriz/i.test(plugin), "plugin.ts must not categorize")

  // The two seams the change requires.
  assert.ok(plugin.includes('session.hook("context"'), "plugin must bind the context hook")
  assert.ok(core.includes("--ids-only"), "core must ask ctxed for the dropped id set")
  assert.ok(core.includes("droppedIds"), "core must parse droppedIds")
})
