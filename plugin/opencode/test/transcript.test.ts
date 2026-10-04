import { test } from "node:test"
import assert from "node:assert/strict"
import {
  isEntryType,
  serializeContextMessages,
  serializeHookMessages,
  transcriptJson,
} from "../src/transcript.ts"

test("hook messages become a ctxed document with ids and types", () => {
  const doc = serializeHookMessages([
    { id: "msg_a", role: "user", content: [{ type: "text", text: "the sky is blue" }] },
    { id: "msg_b", role: "assistant", content: [{ type: "reasoning", text: "thinking" }, { type: "text", text: "water is wet" }] },
  ])
  assert.equal(doc.messages.length, 2)
  assert.deepEqual(doc.messages[0], { id: "msg_a", type: "user", content: [{ type: "text", text: "the sky is blue" }] })
  assert.deepEqual(doc.messages[1].content, [
    { type: "reasoning", text: "thinking" },
    { type: "text", text: "water is wet" },
  ])
})

test("hook tool parts become ctxed tool parts carrying the id", () => {
  const doc = serializeHookMessages([
    {
      id: "msg_t",
      role: "assistant",
      content: [
        { type: "tool-call", id: "call_1", name: "read", input: { path: "/x" } },
        { type: "tool-result", id: "call_1", name: "read", result: { type: "text", value: "ok" } },
      ],
    },
  ])
  assert.deepEqual(doc.messages[0].content, [
    { type: "tool", id: "call_1", name: "read", state: { path: "/x" } },
    { type: "tool", id: "call_1", name: "read", state: { type: "text", value: "ok" } },
  ])
})

test("context messages carry a top-level text as one text part", () => {
  const doc = serializeContextMessages([
    { id: "msg_u", type: "user", text: "hello there" },
    { id: "msg_s", type: "system", text: "be terse" },
  ])
  assert.deepEqual(doc.messages, [
    { id: "msg_u", type: "user", content: [{ type: "text", text: "hello there" }] },
    { id: "msg_s", type: "system", content: [{ type: "text", text: "be terse" }] },
  ])
})

test("context assistant messages keep their content parts", () => {
  const doc = serializeContextMessages([
    { id: "msg_a", type: "assistant", content: [{ type: "text", text: "hi" }, { type: "reasoning", text: "hmm" }] },
  ])
  assert.deepEqual(doc.messages[0], {
    id: "msg_a",
    type: "assistant",
    content: [
      { type: "text", text: "hi" },
      { type: "reasoning", text: "hmm" },
    ],
  })
})

test("non-entry messages keep their id and type so ids round-trip", () => {
  const doc = serializeContextMessages([
    { id: "msg_u", type: "user", text: "hi" },
    { id: "msg_idle", type: "idle" },
    { id: "msg_shell", type: "shell" },
  ])
  assert.deepEqual(
    doc.messages.map((m) => [m.id, m.type]),
    [
      ["msg_u", "user"],
      ["msg_idle", "idle"],
      ["msg_shell", "shell"],
    ],
  )
  assert.ok(!isEntryType("idle"))
  assert.ok(isEntryType("user"))
})

test("both encodings produce the same document for the same conversation", () => {
  // A user turn and an assistant text turn, expressed in each live encoding,
  // must serialize identically — that is the whole point of the two adapters.
  const hook = serializeHookMessages([
    { id: "msg_u", role: "user", content: [{ type: "text", text: "hi" }] },
    { id: "msg_a", role: "assistant", content: [{ type: "text", text: "yo" }] },
  ])
  const context = serializeContextMessages([
    { id: "msg_u", type: "user", text: "hi" },
    { id: "msg_a", type: "assistant", content: [{ type: "text", text: "yo" }] },
  ])
  assert.deepEqual(hook, context)
})

test("transcriptJson is the hook encoding's JSON", () => {
  const json = transcriptJson([{ id: "msg_a", role: "user", content: [{ type: "text", text: "x" }] }])
  assert.deepEqual(JSON.parse(json), serializeHookMessages([{ id: "msg_a", role: "user", content: [{ type: "text", text: "x" }] }]))
})

test("messages with no id still serialize so the array stays aligned", () => {
  const doc = serializeHookMessages([{ role: "user", content: [{ type: "text", text: "x" }] }])
  assert.equal(doc.messages.length, 1)
  assert.equal(doc.messages[0].id, "")
})
