// Live-transcript serialization.
//
// ctxed reads a session *document* (`{messages:[{id,type,content:[…]}]}`).
// OpenCode hands the plugin live messages in one of two encodings, depending on
// which window the plugin looks through:
//
//   - the dispatch hook (`session.hook("context")`): `{id, role, content:[…]}`
//     with content parts tagged `text | reasoning | tool-call | tool-result`.
//   - the session read (`session.context`): `{id, type, text}` for user/system
//     style messages, and `{id, type:"assistant", content:[…]}` with parts
//     tagged `text | reasoning | tool`.
//
// This module is the adapter between those surfaces and ctxed's document. It is
// a shape translation and nothing else: it does not classify, group, or decide
// anything — every prune decision stays in ctxed.

/** A content part in either live encoding. Only the fields we render appear. */
export interface LiveContentPart {
  type?: string
  text?: string
  id?: string
  name?: string
  /** Tool shape varies: hook parts carry `input`/`result`; context parts `state`. */
  input?: unknown
  result?: unknown
  state?: unknown
  [key: string]: unknown
}

/** A live message as seen by the dispatch hook. */
export interface HookMessage {
  id?: string
  role?: string
  content?: LiveContentPart[]
  [key: string]: unknown
}

/**
 * A live message as returned by `session.context`. `type` is the discriminant;
 * user/system-style messages carry a top-level `text`, assistant messages carry
 * `content` parts.
 */
export interface ContextMessage {
  id?: string
  type?: string
  text?: string
  content?: LiveContentPart[]
  [key: string]: unknown
}

/** One message in the OpenCode-export shape ctxed parses. */
export interface TranscriptMessage {
  id: string
  type: string
  content: Array<{ type: string; text?: string; id?: string; name?: string; state?: unknown }>
}

/** The document handed to ctxed on stdin. */
export interface Transcript {
  messages: TranscriptMessage[]
}

/**
 * Message types ctxed's OpenCode adapter treats as categorizable entries.
 * Everything else (idle, shell, skill, agent-switched, …) is carried so its id
 * round-trips but is not classified.
 */
const ENTRY_TYPES = new Set(["user", "assistant", "system", "tool"])

/**
 * Passes a live role/type through to the document's `type` field. ctxed's
 * adapter classifies only `user|assistant|system`; every other type (idle,
 * shell, skill, …) is carried as a non-entry, so its id round-trips without
 * being categorized. An absent type becomes `system`, which the adapter treats
 * as a plain entry.
 */
function entryType(value: unknown): string {
  if (typeof value === "string" && value !== "") return value
  return "system"
}

/** True when a role/type names an entry the adapter will classify. */
export function isEntryType(value: unknown): boolean {
  return typeof value === "string" && ENTRY_TYPES.has(value)
}

/**
 * Renders live content parts to ctxed's part shape. Text and reasoning carry
 * the words ctxed classifies; tool parts carry an id so a tool entry's id
 * round-trips. Parts with neither (media, compaction, effort) are dropped.
 */
function renderParts(parts: readonly LiveContentPart[] | undefined): TranscriptMessage["content"] {
  const out: TranscriptMessage["content"] = []
  for (const part of parts ?? []) {
    switch (part.type) {
      case "text":
        out.push({ type: "text", text: part.text ?? "" })
        break
      case "reasoning":
        out.push({ type: "reasoning", text: part.text ?? "" })
        break
      case "tool-call":
        out.push({ type: "tool", id: part.id ?? "", name: part.name ?? "", state: part.input ?? part.state ?? {} })
        break
      case "tool-result":
        // A result and its call share an id, and ctxed's tool part carries both
        // together; keep it so a lone result's id still round-trips.
        out.push({ type: "tool", id: part.id ?? "", name: part.name ?? "", state: part.result ?? part.state ?? {} })
        break
      case "tool":
        out.push({ type: "tool", id: part.id ?? "", name: part.name ?? "", state: part.state ?? {} })
        break
      default:
        break
    }
  }
  return out
}

/**
 * A message with a top-level `text` and no content parts is rendered as a
 * single text part, so a user/system message classifies like any other. An
 * empty text is dropped rather than rendered as a blank part.
 */
function textPart(text: unknown): TranscriptMessage["content"] {
  return typeof text === "string" && text !== "" ? [{ type: "text", text }] : []
}

/**
 * Serializes dispatch-hook messages (`role` + `content` parts) into ctxed's
 * document shape.
 */
export function serializeHookMessages(messages: readonly HookMessage[]): Transcript {
  return {
    messages: messages.map((message) => ({
      id: typeof message.id === "string" ? message.id : "",
      type: entryType(message.role),
      content: renderParts(message.content),
    })),
  }
}

/**
 * Serializes `session.context` messages (`type` + top-level `text` for
 * user/system-style messages, `content` parts for assistant messages) into
 * ctxed's document shape.
 */
export function serializeContextMessages(messages: readonly ContextMessage[]): Transcript {
  return {
    messages: messages.map((message) => {
      const parts = renderParts(message.content)
      // user/system/synthetic/skill style messages carry `text` directly; use
      // it when there are no content parts.
      const content = parts.length > 0 ? parts : textPart(message.text)
      return {
        id: typeof message.id === "string" ? message.id : "",
        type: entryType(message.type),
        content,
      }
    }),
  }
}

/** The JSON string handed to ctxed on stdin. */
export function transcriptJson(messages: readonly HookMessage[]): string {
  return JSON.stringify(serializeHookMessages(messages))
}
