import { test } from "node:test"
import assert from "node:assert/strict"
import { chmodSync, mkdtempSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import {
  applyPrune,
  buildArgs,
  filterMessages,
  loadConfig,
  parseDroppedIds,
  PruneCache,
  runCtxed,
  sessionRevision,
  type PluginConfig,
  type Runner,
} from "../src/core.ts"

/** Writes an executable stub that stands in for the real ctxed binary. */
function stubCtxed(script: string): string {
  const dir = mkdtempSync(join(tmpdir(), "ctxed-stub-"))
  const file = join(dir, "ctxed")
  writeFileSync(file, script, "utf8")
  chmodSync(file, 0o755)
  return file
}

function config(overrides: Partial<PluginConfig> = {}): PluginConfig {
  return {
    enabled: true,
    ctxedPath: "ctxed",
    sessionExport: "/tmp/session.json",
    categoriesFile: "/tmp/cats.json",
    categories: "1",
    ids: "",
    timeoutMs: 1000,
    ...overrides,
  }
}

function countingRunner(output = '{"droppedIds":["b"]}'): { run: Runner; calls: () => number } {
  let calls = 0
  const run: Runner = async () => {
    calls += 1
    return { code: 0, stdout: output, stderr: "" }
  }
  return { run, calls: () => calls }
}

test("buildArgs invokes ctxed in id-only mode for a category selection", () => {
  assert.deepEqual(buildArgs(config()), [
    "ctxed",
    "prune",
    "/tmp/session.json",
    "--categories-file",
    "/tmp/cats.json",
    "--categories",
    "1",
    "--ids-only",
  ])
})

test("buildArgs invokes ctxed in id-only mode for explicit ids", () => {
  const args = buildArgs(config({ categoriesFile: "", categories: "", ids: "msg_a,msg_b" }))
  assert.deepEqual(args, ["ctxed", "prune", "/tmp/session.json", "--ids", "msg_a,msg_b", "--ids-only"])
})

test("filterMessages removes only dropped ids and preserves order", () => {
  const messages = [{ id: "a" }, { id: "b" }, { id: "c" }, { id: "b" }]
  const out = filterMessages(messages, new Set(["b"]))
  assert.deepEqual(
    out.map((m) => m.id),
    ["a", "c"],
  )
  assert.deepEqual(
    messages.map((m) => m.id),
    ["a", "b", "c", "b"],
    "input must not be mutated",
  )
})

test("filterMessages keeps messages without a string id", () => {
  const messages = [{ id: "a" }, {}, { id: 7 }]
  const out = filterMessages(messages, new Set(["a"]))
  assert.equal(out.length, 2)
})

test("parseDroppedIds accepts the exact id-only shape", () => {
  assert.deepEqual(parseDroppedIds('{"droppedIds":["msg_a","msg_b"]}'), ["msg_a", "msg_b"])
  assert.deepEqual(parseDroppedIds('{"droppedIds":[]}'), [])
})

test("parseDroppedIds rejects malformed output", () => {
  for (const bad of ["not json", "[]", "{}", '{"droppedIds":"x"}', '{"droppedIds":[1]}']) {
    assert.throws(() => parseDroppedIds(bad), /ctxed output/, `should reject: ${bad}`)
  }
})

test("loadConfig reads configuration from env", () => {
  const cfg = loadConfig(
    {},
    {
      CTXED_PLUGIN_CTXED_PATH: "/usr/local/bin/ctxed",
      CTXED_PLUGIN_SESSION_EXPORT: "/sessions/s.json",
      CTXED_PLUGIN_CATEGORIES_FILE: "/sessions/s.categories.json",
      CTXED_PLUGIN_CATEGORIES: "2,3",
      CTXED_PLUGIN_TIMEOUT_MS: "250",
    },
  )
  assert.equal(cfg.enabled, true)
  assert.equal(cfg.ctxedPath, "/usr/local/bin/ctxed")
  assert.equal(cfg.sessionExport, "/sessions/s.json")
  assert.equal(cfg.categoriesFile, "/sessions/s.categories.json")
  assert.equal(cfg.categories, "2,3")
  assert.equal(cfg.timeoutMs, 250)
})

test("loadConfig lets options override env and detects missing selection", () => {
  const cfg = loadConfig(
    { categories: "9", timeoutMs: 50 },
    {
      CTXED_PLUGIN_SESSION_EXPORT: "/s.json",
      CTXED_PLUGIN_CATEGORIES_FILE: "/c.json",
      CTXED_PLUGIN_CATEGORIES: "2",
      CTXED_PLUGIN_TIMEOUT_MS: "900",
    },
  )
  assert.equal(cfg.categories, "9")
  assert.equal(cfg.timeoutMs, 50)
  assert.equal(cfg.ctxedPath, "ctxed", "binary defaults to PATH lookup")

  assert.equal(loadConfig({}, { CTXED_PLUGIN_SESSION_EXPORT: "/s.json" }).enabled, false)
  assert.equal(loadConfig({}, {}).enabled, false)
})

test("sessionRevision tracks the message id set", () => {
  assert.notEqual(sessionRevision([{ id: "a" }, { id: "b" }]), sessionRevision([{ id: "a" }, { id: "b" }, { id: "c" }]))
  assert.equal(sessionRevision([{ id: "a" }, { id: "b" }]), sessionRevision([{ id: "a" }, { id: "b" }]))
})

test("applyPrune filters messages using a stub ctxed", async () => {
  const path = stubCtxed('#!/bin/sh\necho \'{"droppedIds":["b"]}\'\n')
  const cfg = config({ ctxedPath: path })
  const result = await applyPrune({
    messages: [{ id: "a" }, { id: "b" }, { id: "c" }],
    config: cfg,
    revision: "a|b|c",
    cache: new PruneCache(),
    run: (args) => runCtxed(args, cfg.timeoutMs),
    report: () => {},
  })
  assert.deepEqual(
    result.messages.map((m) => m.id),
    ["a", "c"],
  )
  assert.equal(result.dropped, 1)
  assert.equal(result.error, undefined)
})

test("applyPrune is a no-op when no selection is configured", async () => {
  const { run, calls } = countingRunner()
  const result = await applyPrune({
    messages: [{ id: "a" }],
    config: config({ enabled: false }),
    revision: "a",
    cache: new PruneCache(),
    run,
    report: () => {},
  })
  assert.equal(calls(), 0)
  assert.deepEqual(
    result.messages.map((m) => m.id),
    ["a"],
  )
})

test("applyPrune is fail-open on a non-zero exit", async () => {
  const path = stubCtxed('#!/bin/sh\necho "boom" >&2\nexit 1\n')
  const cfg = config({ ctxedPath: path })
  const reports: string[] = []
  const result = await applyPrune({
    messages: [{ id: "a" }, { id: "b" }],
    config: cfg,
    revision: "a|b",
    cache: new PruneCache(),
    run: (args) => runCtxed(args, cfg.timeoutMs),
    report: (message) => reports.push(message),
  })
  assert.match(result.error ?? "", /exited 1/)
  assert.deepEqual(
    result.messages.map((m) => m.id),
    ["a", "b"],
  )
  assert.equal(reports.length, 1)
})

test("applyPrune is fail-open on unparseable output", async () => {
  const path = stubCtxed('#!/bin/sh\necho "not json"\n')
  const cfg = config({ ctxedPath: path })
  const reports: string[] = []
  const result = await applyPrune({
    messages: [{ id: "a" }],
    config: cfg,
    revision: "a",
    cache: new PruneCache(),
    run: (args) => runCtxed(args, cfg.timeoutMs),
    report: (message) => reports.push(message),
  })
  assert.match(result.error ?? "", /could not be parsed/)
  assert.deepEqual(
    result.messages.map((m) => m.id),
    ["a"],
  )
  assert.equal(reports.length, 1)
})

test("applyPrune is fail-open when ctxed hangs", async () => {
  const path = stubCtxed("#!/bin/sh\nexec sleep 5\n")
  const cfg = config({ ctxedPath: path, timeoutMs: 150 })
  const reports: string[] = []
  const result = await applyPrune({
    messages: [{ id: "a" }],
    config: cfg,
    revision: "a",
    cache: new PruneCache(),
    run: (args) => runCtxed(args, cfg.timeoutMs),
    report: (message) => reports.push(message),
  })
  assert.match(result.error ?? "", /timed out/)
  assert.deepEqual(
    result.messages.map((m) => m.id),
    ["a"],
  )
  assert.equal(reports.length, 1)
})

test("applyPrune caches by selection and revision", async () => {
  const { run, calls } = countingRunner()
  const cache = new PruneCache()
  const cfg = config()
  const opts = { config: cfg, cache, run, report: () => {} }
  const first = await applyPrune({ messages: [{ id: "a" }, { id: "b" }], revision: "a|b", ...opts })
  assert.equal(first.cached, false)
  assert.equal(calls(), 1)

  const second = await applyPrune({ messages: [{ id: "a" }, { id: "b" }], revision: "a|b", ...opts })
  assert.equal(second.cached, true, "unchanged selection and revision must reuse the result")
  assert.equal(calls(), 1)

  await applyPrune({ messages: [{ id: "a" }, { id: "b" }, { id: "c" }], revision: "a|b|c", ...opts })
  assert.equal(calls(), 2, "a changed revision must refresh")

  await applyPrune({
    messages: [{ id: "a" }],
    revision: "a",
    config: config({ categories: "2" }),
    cache,
    run,
    report: () => {},
  })
  assert.equal(calls(), 3, "a changed selection must refresh")
})

test("runCtxed returns stdout and exit code", async () => {
  const path = stubCtxed('#!/bin/sh\necho \'{"droppedIds":["z"]}\'\n')
  const result = await runCtxed([path, "prune", "/s.json", "--ids", "z", "--ids-only"], 500)
  assert.equal(result.code, 0)
  assert.match(result.stdout, /droppedIds/)
})
