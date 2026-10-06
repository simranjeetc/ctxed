// The plugin's whole job: before each request, remove the messages on the
// session's drop list. ctxed writes the list (`ctxed opencode drop`); this
// module only reads it and filters. It decides nothing.

import { readFileSync } from "node:fs"
import { homedir } from "node:os"
import { join } from "node:path"

/** A message as the dispatch hook sees it. Only the fields we match on. */
export interface LiveMessage {
  id?: unknown
  content?: unknown
  [key: string]: unknown
}

/** The part of ctxed's state file the plugin uses. */
export interface DropList {
  ids: ReadonlySet<string>
  toolCallIds: ReadonlySet<string>
}

const SESSION_ID = /^ses_[A-Za-z0-9]+$/

/**
 * The state directory, resolved exactly as ctxed resolves it: $CTXED_STATE_DIR,
 * else ~/.local/state/ctxed/opencode. Resolving from the home directory keeps a
 * server and a shell with different environments in agreement.
 */
export function stateDir(env: NodeJS.ProcessEnv, home: string = homedir()): string {
  const configured = env.CTXED_STATE_DIR ?? ""
  return configured !== "" ? configured : join(home, ".local", "state", "ctxed", "opencode")
}

/**
 * Reads a session's drop list. Returns undefined when there is none, when the
 * session id is not a plain OpenCode id, or when the file cannot be read or
 * parsed: the caller then sends the request unchanged.
 */
export function readDropList(dir: string, sessionID: string): DropList | undefined {
  if (!SESSION_ID.test(sessionID)) return undefined
  let raw: string
  try {
    raw = readFileSync(join(dir, `${sessionID}.json`), "utf8")
  } catch {
    return undefined
  }
  try {
    const dropped = (JSON.parse(raw) as { dropped?: { ids?: unknown; toolCallIds?: unknown } }).dropped
    const ids = strings(dropped?.ids)
    const toolCallIds = strings(dropped?.toolCallIds)
    if (ids.length === 0 && toolCallIds.length === 0) return undefined
    return { ids: new Set(ids), toolCallIds: new Set(toolCallIds) }
  } catch {
    return undefined
  }
}

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((x): x is string => typeof x === "string") : []
}

/**
 * True when a message carries a tool part whose call id is on the drop list. A
 * tool result can travel in a message with no id of its own; it belongs to a
 * dropped message and must go with it.
 */
export function carriesDroppedToolCall(message: LiveMessage, toolCallIds: ReadonlySet<string>): boolean {
  if (toolCallIds.size === 0 || !Array.isArray(message.content)) return false
  for (const part of message.content) {
    if (part === null || typeof part !== "object") continue
    const p = part as { type?: unknown; id?: unknown }
    if ((p.type === "tool-call" || p.type === "tool-result" || p.type === "tool") && typeof p.id === "string" && toolCallIds.has(p.id)) {
      return true
    }
  }
  return false
}

/**
 * Removes listed messages. Order and every other message are preserved; the
 * input is not mutated. A listed id absent from the request is simply ignored.
 */
export function filterDropped<T extends LiveMessage>(messages: readonly T[], list: DropList): T[] {
  return messages.filter(
    (message) => !(typeof message.id === "string" && list.ids.has(message.id)) && !carriesDroppedToolCall(message, list.toolCallIds),
  )
}
