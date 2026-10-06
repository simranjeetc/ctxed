import { test } from "node:test"
import assert from "node:assert/strict"
import { mkdtempSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { carriesDroppedToolCall, filterDropped, readDropList, stateDir, type DropList } from "../src/core.ts"

function stateFile(dropped: unknown, session = "ses_abc"): string {
  const dir = mkdtempSync(join(tmpdir(), "ctxed-plugin-"))
  writeFileSync(join(dir, `${session}.json`), JSON.stringify({ session, dropped }))
  return dir
}

const list = (ids: string[], toolCallIds: string[] = []): DropList => ({ ids: new Set(ids), toolCallIds: new Set(toolCallIds) })

test("stateDir matches ctxed: env override, else ~/.local/state/ctxed/opencode", () => {
  assert.equal(stateDir({ CTXED_STATE_DIR: "/x" }, "/home/u"), "/x")
  assert.equal(stateDir({}, "/home/u"), "/home/u/.local/state/ctxed/opencode")
  assert.equal(stateDir({ CTXED_STATE_DIR: "" }, "/home/u"), "/home/u/.local/state/ctxed/opencode")
})

test("readDropList reads ids and tool-call ids", () => {
  const dir = stateFile({ ids: ["m1", "m2"], toolCallIds: ["c1"] })
  const got = readDropList(dir, "ses_abc")
  assert.ok(got)
  assert.deepEqual([...got.ids], ["m1", "m2"])
  assert.deepEqual([...got.toolCallIds], ["c1"])
})

test("readDropList is undefined when there is nothing to apply", () => {
  const empty = stateFile({ ids: [], toolCallIds: [] })
  assert.equal(readDropList(empty, "ses_abc"), undefined)
  assert.equal(readDropList(empty, "ses_other"), undefined, "missing file")
})

test("readDropList fails open on a corrupt file", () => {
  const dir = mkdtempSync(join(tmpdir(), "ctxed-plugin-"))
  writeFileSync(join(dir, "ses_abc.json"), "{not json")
  assert.equal(readDropList(dir, "ses_abc"), undefined)
})

test("readDropList refuses a session id that is not a plain OpenCode id", () => {
  const dir = stateFile({ ids: ["m1"] })
  for (const bad of ["../ses_abc", "ses_abc/..", "abc", ""]) {
    assert.equal(readDropList(dir, bad), undefined, bad)
  }
})

test("readDropList ignores non-string entries", () => {
  const dir = stateFile({ ids: ["m1", 7, null], toolCallIds: "nope" })
  const got = readDropList(dir, "ses_abc")
  assert.ok(got)
  assert.deepEqual([...got.ids], ["m1"])
  assert.equal(got.toolCallIds.size, 0)
})

test("filterDropped removes listed ids, keeps order, ignores ids not present", () => {
  const messages = [{ id: "a" }, { id: "b" }, { id: "c" }, { id: "d" }]
  const kept = filterDropped(messages, list(["b", "d", "not-in-request"]))
  assert.deepEqual(kept.map((m) => m.id), ["a", "c"])
  assert.equal(messages.length, 4, "input not mutated")
})

test("filterDropped keeps messages without a string id", () => {
  const messages = [{ id: undefined }, { id: 3 }, { id: "a" }]
  assert.equal(filterDropped(messages, list(["a"])).length, 2)
})

test("carriesDroppedToolCall matches tool parts by call id", () => {
  assert.ok(carriesDroppedToolCall({ content: [{ type: "tool-result", id: "c1" }] }, new Set(["c1"])))
  assert.ok(carriesDroppedToolCall({ content: [{ type: "tool-call", id: "c1" }] }, new Set(["c1"])))
  assert.ok(!carriesDroppedToolCall({ content: [{ type: "text", id: "c1" }] }, new Set(["c1"])))
  assert.ok(!carriesDroppedToolCall({ content: "text" }, new Set(["c1"])))
})

test("filterDropped drops an id-less tool result for a dropped call", () => {
  const messages = [
    { id: "a", content: [{ type: "tool-call", id: "c1" }] },
    { content: [{ type: "tool-result", id: "c1" }] },
    { content: [{ type: "tool-result", id: "c2" }] },
  ]
  const kept = filterDropped(messages, list(["a"], ["c1"]))
  assert.equal(kept.length, 1)
  assert.deepEqual(kept[0].content, [{ type: "tool-result", id: "c2" }])
})
