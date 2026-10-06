import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const here = dirname(fileURLToPath(import.meta.url))

// Source with comments removed: the checks are about what the code does.
function source(relative: string): string {
  return readFileSync(join(here, "..", relative), "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^\s*\/\/.*$/gm, "")
}

test("the plugin only filters: it decides nothing and says nothing", () => {
  const plugin = source("src/plugin.ts")
  const core = source("src/core.ts")
  const code = plugin + core

  // ctxed decides what to drop; the plugin must not sort, select or repair.
  for (const pattern of [/orphan/i, /classif/i, /categoriz/i, /bucket/i]) {
    assert.ok(!pattern.test(code), `plugin source must not contain ${pattern}`)
  }
  // Every message the plugin posted started a model turn; it must post none.
  for (const pattern of [/synthetic/, /command\.transform/, /session\.prompt/, /hook\("prompt"/]) {
    assert.ok(!pattern.test(code), `plugin source must not contain ${pattern}`)
  }
  // It runs nothing.
  assert.ok(!/child_process|spawn|execFile/.test(code), "the plugin must not run a binary")

  // The one seam it needs.
  assert.ok(plugin.includes('session.hook("context"'), "plugin must bind the context hook")
  // OpenCode loads only a default export; without it the plugin silently does nothing.
  assert.ok(/^export default CtxedPrunePlugin$/m.test(plugin), "plugin must have a default export")
})
