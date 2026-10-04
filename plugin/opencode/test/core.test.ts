import { test } from "node:test"
import assert from "node:assert/strict"
import { chmodSync, mkdtempSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import {
  applyPrune,
  buildArgs,
  buildCategorizeArgs,
  categorizeLive,
  extractJsonObject,
  filterMessages,
  formatBuckets,
  loadConfig,
  parseBuckets,
  parseDroppedIds,
  parseSelection,
  parseSelectionInput,
  PruneCache,
  runCtxed,
  selectionKey,
  sessionRevision,
  type PluginConfig,
  type Runner,
  type Selection,
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
    categorizerCmd: "",
    categorizerModel: "",
    maxCategories: "",
    debugLog: "",
    timeoutMs: 1000,
    ...overrides,
  }
}

function selection(overrides: Partial<Selection> = {}): Selection {
  return {
    categoriesFile: "/tmp/cats.json",
    categoryIds: "1",
    selectedAt: "2026-10-04T00:00:00.000Z",
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

test("buildArgs invokes prune in id-only mode over stdin with the selection", () => {
  assert.deepEqual(buildArgs(config(), selection()), [
    "ctxed",
    "prune",
    "-",
    "--categories-file",
    "/tmp/cats.json",
    "--categories",
    "1",
    "--ids-only",
  ])
})

test("buildCategorizeArgs categorizes stdin and prints the file to stdout", () => {
  assert.deepEqual(buildCategorizeArgs(config()), ["ctxed", "categorize", "-", "--out", "-"])
  assert.deepEqual(
    buildCategorizeArgs(config({ categorizerCmd: "llm -m x", categorizerModel: "gpt", maxCategories: "4" })),
    ["ctxed", "categorize", "-", "--categorizer-cmd", "llm -m x", "--model", "gpt", "--max-categories", "4", "--out", "-"],
  )
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
      CTXED_PLUGIN_CATEGORIZER_CMD: "llm -m gpt",
      CTXED_PLUGIN_CATEGORIZER_MODEL: "gpt-4o",
      CTXED_PLUGIN_MAX_CATEGORIES: "4",
      CTXED_PLUGIN_TIMEOUT_MS: "250",
      CTXED_PLUGIN_DEBUG_LOG: "/tmp/ctxed-debug.log",
    },
  )
  assert.equal(cfg.enabled, true)
  assert.equal(cfg.ctxedPath, "/usr/local/bin/ctxed")
  assert.equal(cfg.categorizerCmd, "llm -m gpt")
  assert.equal(cfg.categorizerModel, "gpt-4o")
  assert.equal(cfg.maxCategories, "4")
  assert.equal(cfg.timeoutMs, 250)
  assert.equal(cfg.debugLog, "/tmp/ctxed-debug.log")
})

test("loadConfig lets options override env and defaults the binary to PATH", () => {
  const cfg = loadConfig(
    { categorizerModel: "haiku", timeoutMs: 50 },
    {
      CTXED_PLUGIN_CATEGORIZER_MODEL: "gpt-4o",
      CTXED_PLUGIN_TIMEOUT_MS: "900",
    },
  )
  assert.equal(cfg.categorizerModel, "haiku")
  assert.equal(cfg.timeoutMs, 50)
  assert.equal(cfg.ctxedPath, "ctxed", "binary defaults to PATH lookup")
  assert.equal(loadConfig({}, {}).enabled, true, "the plugin is always available")
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
    selection: selection(),
    revision: "a|b|c",
    cache: new PruneCache(),
    run: (args, stdin) => runCtxed(args, cfg.timeoutMs, stdin),
    report: () => {},
  })
  assert.deepEqual(
    result.messages.map((m) => m.id),
    ["a", "c"],
  )
  assert.equal(result.dropped, 1)
  assert.equal(result.error, undefined)
})

test("applyPrune sends the live transcript to ctxed on stdin", async () => {
  // The stub echoes its stdin, which proves the transcript is piped through and
  // carries the live message ids ctxed needs to resolve.
  const path = stubCtxed('#!/bin/sh\ntranscript=$(cat)\ncase "$transcript" in *msg_a*) echo \'{"droppedIds":["msg_a"]}\';; *) echo \'{"droppedIds":[]}\';; esac\n')
  const cfg = config({ ctxedPath: path })
  const result = await applyPrune({
    messages: [
      { id: "msg_a", role: "user", content: [{ type: "text", text: "drop me" }] },
      { id: "msg_b", role: "assistant", content: [{ type: "text", text: "keep me" }] },
    ],
    config: cfg,
    selection: selection(),
    revision: "msg_a|msg_b",
    cache: new PruneCache(),
    run: (args, stdin) => runCtxed(args, cfg.timeoutMs, stdin),
    report: () => {},
  })
  assert.deepEqual(
    result.messages.map((m) => m.id),
    ["msg_b"],
  )
})

test("applyPrune is a no-op when the plugin is disabled", async () => {
  const { run, calls } = countingRunner()
  const result = await applyPrune({
    messages: [{ id: "a" }],
    config: config({ enabled: false }),
    selection: selection(),
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
    selection: selection(),
    revision: "a|b",
    cache: new PruneCache(),
    run: (args, stdin) => runCtxed(args, cfg.timeoutMs, stdin),
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
    selection: selection(),
    revision: "a",
    cache: new PruneCache(),
    run: (args, stdin) => runCtxed(args, cfg.timeoutMs, stdin),
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
    selection: selection(),
    revision: "a",
    cache: new PruneCache(),
    run: (args, stdin) => runCtxed(args, cfg.timeoutMs, stdin),
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
  const opts = { config: cfg, selection: selection(), cache, run, report: () => {} }
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
    config: cfg,
    selection: selection({ categoryIds: "2" }),
    cache,
    run,
    report: () => {},
  })
  assert.equal(calls(), 3, "a changed selection must refresh")
})

test("PruneCache.clear drops cached entries", () => {
  const cache = new PruneCache()
  cache.set("k", ["a"])
  assert.equal(cache.size, 1)
  cache.clear()
  assert.equal(cache.size, 0)
  assert.equal(cache.get("k"), undefined)
})

test("runCtxed returns stdout and exit code", async () => {
  const path = stubCtxed('#!/bin/sh\necho \'{"droppedIds":["z"]}\'\n')
  const result = await runCtxed([path, "prune", "-", "--ids", "z", "--ids-only"], 500)
  assert.equal(result.code, 0)
  assert.match(result.stdout, /droppedIds/)
})

// --- command path: buckets, selection, categorize ---------------------------

test("parseBuckets reads only what a human sees", () => {
  const buckets = parseBuckets(
    JSON.stringify({
      categories: [
        { id: 1, label: "Design", entryIds: ["msg_secret"], entryCount: 2, tokens: 10 },
        { id: 2, label: "Impl", entryIds: ["msg_other"], entryCount: 1, tokens: 20 },
      ],
    }),
  )
  assert.deepEqual(buckets, [
    { id: 1, label: "Design", entryCount: 2, tokens: 10 },
    { id: 2, label: "Impl", entryCount: 1, tokens: 20 },
  ])
})

test("parseBuckets rejects a non-categories document", () => {
  for (const bad of ["not json", "[]", "{}"]) {
    assert.throws(() => parseBuckets(bad), /categor/i, `should reject: ${bad}`)
  }
})

test("formatBuckets never prints entry ids", () => {
  const text = formatBuckets([{ id: 1, label: "Design", entryCount: 2, tokens: 10 }])
  assert.match(text, /Design/)
  assert.ok(!text.includes("msg_"), "no message id may appear in the presentation")
})

test("categorizeLive runs ctxed and parses the buckets from stdout", async () => {
  const path = stubCtxed(
    '#!/bin/sh\ncat >/dev/null\necho \'{"categories":[{"id":1,"label":"A","entryCount":1,"tokens":3},{"id":2,"label":"B","entryCount":1,"tokens":4}]}\'\n',
  )
  const cfg = config({ ctxedPath: path })
  const result = await categorizeLive({
    transcript: JSON.stringify({ messages: [{ id: "msg_a", type: "user", content: [{ type: "text", text: "hi" }] }] }),
    config: cfg,
    run: (args, stdin) => runCtxed(args, cfg.timeoutMs, stdin),
  })
  assert.equal(result.error, undefined)
  assert.deepEqual(
    result.buckets.map((b) => b.label),
    ["A", "B"],
  )
})

test("categorizeLive is fail-open on a non-zero exit", async () => {
  const path = stubCtxed("#!/bin/sh\ncat >/dev/null\necho 'nope' >&2\nexit 3\n")
  const cfg = config({ ctxedPath: path })
  const result = await categorizeLive({
    transcript: "{}",
    config: cfg,
    run: (args, stdin) => runCtxed(args, cfg.timeoutMs, stdin),
  })
  assert.match(result.error ?? "", /exited 3/)
  assert.deepEqual(result.buckets, [])
})

test("extractJsonObject pulls the JSON document out of mixed output", () => {
  assert.equal(extractJsonObject('table text\n{"a":1}\n'), '{"a":1}')
  assert.equal(extractJsonObject("no json here"), undefined)
})

test("selection round-trips through parseSelection", () => {
  const key = selectionKey("ses_abc")
  assert.match(key, /ses_abc/)
  const parsed = parseSelection({ categoriesFile: "/tmp/c.json", categoryIds: "1,3", selectedAt: "t" })
  assert.deepEqual(parsed, { categoriesFile: "/tmp/c.json", categoryIds: "1,3", selectedAt: "t" })
  assert.equal(parseSelection(undefined), undefined)
  assert.equal(parseSelection({ categoriesFile: "/tmp/c.json" }), undefined, "a selection needs category ids")
})

test("parseSelectionInput accepts bucket ids and nothing else", () => {
  assert.deepEqual(parseSelectionInput("1,3"), ["1", "3"])
  assert.deepEqual(parseSelectionInput("1 3 5"), ["1", "3", "5"])
  assert.deepEqual(parseSelectionInput(""), [])
  assert.deepEqual(parseSelectionInput("msg_abc"), [], "message ids are not selectable")
})
