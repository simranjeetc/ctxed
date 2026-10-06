// ctxed-opencode core.
//
// This module does exactly three things: run ctxed in id-only prune mode,
// parse the `droppedIds` set it prints, and remove live messages whose id is in
// that set. Every decision — grouping entries, resolving a selection, and
// keeping the result structurally sound — lives in ctxed. This module holds no
// policy of its own.
//
// The command path adds two more thin actions: render the live transcript to
// ctxed on stdin to get its categories, and persist the user's chosen category
// ids as the session's active selection. Neither inspects ids or decides what a
// bucket means.

import { spawn } from "node:child_process"
import { existsSync } from "node:fs"
import { homedir } from "node:os"
import { join } from "node:path"

import { transcriptJson, type HookMessage as TranscriptMessage } from "./transcript.ts"

/** A live OpenCode outbound message. */
export type LiveMessage = TranscriptMessage

export const DEFAULT_TIMEOUT_MS = 2000

/**
 * The in-session command categorizes by calling a model, which takes seconds —
 * far longer than the local id-only prune the dispatch hook runs. Give the
 * command its own budget so a slow model never fails the fast dispatch path,
 * and a fast dispatch timeout never truncates the model call.
 */
export const DEFAULT_COMMAND_TIMEOUT_MS = 60000

export interface PluginConfig {
  /** True when the plugin is configured enough to run at all. */
  enabled: boolean
  ctxedPath: string
  /** The categorizer command ctxed runs (its prompt goes to the command's stdin). */
  categorizerCmd: string
  /** A categorizer model name, when the transport is a model endpoint. */
  categorizerModel: string
  /** Maximum number of buckets to ask ctxed for. */
  maxCategories: string
  timeoutMs: number
  /** Timeout for the command's model-backed categorize step. */
  commandTimeoutMs: number
  /** When set, the dispatch hook appends its decision to this file (testing). */
  debugLog: string
}

export interface RunResult {
  code: number
  stdout: string
  stderr: string
  error?: Error
}

/**
 * Runs ctxed with the given argv. When `stdin` is provided it is written to the
 * child's stdin and the stream is closed; otherwise stdin is ignored.
 */
export type Runner = (args: string[], stdin?: string) => Promise<RunResult>

const OPTION_NAMES: Record<keyof Omit<PluginConfig, "enabled">, string> = {
  ctxedPath: "ctxedPath",
  categorizerCmd: "categorizerCmd",
  categorizerModel: "categorizerModel",
  maxCategories: "maxCategories",
  timeoutMs: "timeoutMs",
  commandTimeoutMs: "commandTimeoutMs",
  debugLog: "debugLog",
}

const ENV_NAMES: Record<keyof Omit<PluginConfig, "enabled">, string> = {
  ctxedPath: "CTXED_PLUGIN_CTXED_PATH",
  categorizerCmd: "CTXED_PLUGIN_CATEGORIZER_CMD",
  categorizerModel: "CTXED_PLUGIN_CATEGORIZER_MODEL",
  maxCategories: "CTXED_PLUGIN_MAX_CATEGORIES",
  timeoutMs: "CTXED_PLUGIN_TIMEOUT_MS",
  commandTimeoutMs: "CTXED_PLUGIN_COMMAND_TIMEOUT_MS",
  debugLog: "CTXED_PLUGIN_DEBUG_LOG",
}

function asString(value: unknown): string {
  if (typeof value === "number") return String(value)
  if (typeof value === "string") return value.trim()
  return ""
}

/**
 * Reads plugin configuration from OpenCode plugin options first, falling back
 * to environment variables. The plugin is enabled whenever a ctxed path is
 * resolvable; a categorizer transport is only needed for the command path, so
 * it is not a gate (the categorize step fails open and reports if it is missing).
 */
export function loadConfig(
  options: Record<string, unknown> = {},
  env: Record<string, string | undefined> = {},
): PluginConfig {
  const pick = (key: keyof typeof OPTION_NAMES): string => {
    const fromOptions = asString(options[OPTION_NAMES[key]])
    if (fromOptions !== "") return fromOptions
    return asString(env[ENV_NAMES[key]])
  }

  const ctxedPath = pick("ctxedPath") || "ctxed"
  const categorizerCmd = pick("categorizerCmd")
  const categorizerModel = pick("categorizerModel")
  const maxCategories = pick("maxCategories")
  const debugLog = pick("debugLog")

  let timeoutMs = Number.parseInt(pick("timeoutMs"), 10)
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) timeoutMs = DEFAULT_TIMEOUT_MS

  let commandTimeoutMs = Number.parseInt(pick("commandTimeoutMs"), 10)
  if (!Number.isFinite(commandTimeoutMs) || commandTimeoutMs <= 0) commandTimeoutMs = DEFAULT_COMMAND_TIMEOUT_MS

  return {
    enabled: true,
    ctxedPath,
    categorizerCmd,
    categorizerModel,
    maxCategories,
    timeoutMs,
    commandTimeoutMs,
    debugLog,
  }
}

/**
 * Resolves the ctxed binary to something runnable. A configured absolute path is
 * kept as-is. A bare name ("ctxed", the default) is resolved against the usual
 * install locations first, because OpenCode runs its server with a minimal PATH
 * that omits a user's ~/go/bin or Homebrew bin. Falls back to the bare name for
 * a normal PATH lookup when no known location exists.
 */
export function resolveCtxedPath(configured: string): string {
  if (configured.includes("/")) return configured

  const home = homedir()
  const candidates = [
    join(home, "go", "bin", "ctxed"),
    "/opt/homebrew/bin/ctxed",
    "/usr/local/bin/ctxed",
    join(home, ".local", "bin", "ctxed"),
    join(home, ".opencode", "bin", "ctxed"),
  ]
  for (const candidate of candidates) {
    if (existsSync(candidate)) return candidate
  }
  return configured
}

/** Builds the exact ctxed invocation the plugin relies on. */
export function buildArgs(config: PluginConfig, selection: Selection): string[] {
  return [
    config.ctxedPath,
    "prune",
    "-",
    "--categories-file",
    selection.categoriesFile,
    "--categories",
    selection.categoryIds,
    "--ids-only",
  ]
}

/** A compact fingerprint of the configured invocation inputs. */
export function selectionSignature(config: PluginConfig, selection: Selection): string {
  return JSON.stringify([config.ctxedPath, selection.categoriesFile, selection.categoryIds])
}

/**
 * A cheap revision of the live conversation: the ordered message ids. It
 * changes whenever the message set changes, and costs no subprocess.
 */
export function sessionRevision(messages: readonly LiveMessage[]): string {
  const ids: string[] = []
  for (const message of messages) ids.push(typeof message.id === "string" ? message.id : "?")
  return ids.join("|")
}

export function cacheKey(config: PluginConfig, selection: Selection, revision: string): string {
  return `${selectionSignature(config, selection)}\n${revision}`
}

/** Parses `{"droppedIds":[…]}` and rejects anything that is not that shape. */
export function parseDroppedIds(stdout: string): string[] {
  let parsed: unknown
  try {
    parsed = JSON.parse(stdout)
  } catch {
    throw new Error("ctxed output is not valid JSON")
  }
  if (parsed === null || typeof parsed !== "object") {
    throw new Error("ctxed output is not a JSON object")
  }
  const dropped = (parsed as { droppedIds?: unknown }).droppedIds
  if (!Array.isArray(dropped)) throw new Error("ctxed output has no droppedIds array")
  for (const id of dropped) {
    if (typeof id !== "string") throw new Error("ctxed output droppedIds contains a non-string id")
  }
  return dropped as string[]
}

/**
 * The full drop decision ctxed returns: the message ids to drop, and the tool-call
 * ids those messages issued or answered. The tool-call ids let the plugin drop a
 * tool result that lives in a live message carrying no id of its own.
 */
export interface DroppedSet {
  ids: string[]
  toolCallIds: string[]
}

/** Parses the id-only output into a DroppedSet. */
export function parseDropped(stdout: string): DroppedSet {
  const ids = parseDroppedIds(stdout)
  let toolCallIds: string[] = []
  try {
    const parsed = JSON.parse(stdout) as { droppedToolCallIds?: unknown }
    if (Array.isArray(parsed.droppedToolCallIds)) {
      toolCallIds = parsed.droppedToolCallIds.filter((x): x is string => typeof x === "string")
    }
  } catch {
    // parseDroppedIds already validated the document; a missing optional field is fine.
  }
  return { ids, toolCallIds }
}

/**
 * Removes only messages whose id is in the dropped set. Order and every other
 * message are preserved; the input array is not mutated.
 */
export function filterMessages<T extends LiveMessage>(
  messages: readonly T[],
  dropped: ReadonlySet<string>,
): T[] {
  return messages.filter((message) => !(typeof message.id === "string" && dropped.has(message.id)))
}

/**
 * True when a live message carries a content part (tool-call or tool-result)
 * whose id is in the dropped tool-call set. Such a message has no id of its own
 * to match, but it belongs to a dropped entry and must not be sent.
 */
export function carriesDroppedToolCall(message: LiveMessage, droppedToolCalls: ReadonlySet<string>): boolean {
  const content = (message as { content?: unknown }).content
  if (!Array.isArray(content)) return false
  for (const part of content) {
    if (part === null || typeof part !== "object") continue
    const p = part as { type?: unknown; id?: unknown }
    if ((p.type === "tool-call" || p.type === "tool-result" || p.type === "tool") && typeof p.id === "string" && droppedToolCalls.has(p.id)) {
      return true
    }
  }
  return false
}

/** Drops messages by id, and any message carrying a dropped tool-call id. */
export function filterDropped<T extends LiveMessage>(
  messages: readonly T[],
  dropped: ReadonlySet<string>,
  droppedToolCalls: ReadonlySet<string>,
): T[] {
  return messages.filter(
    (message) => !(typeof message.id === "string" && dropped.has(message.id)) && !carriesDroppedToolCall(message, droppedToolCalls),
  )
}

/** Caches drop decisions, keyed by selection and revision. */
export class PruneCache {
  private readonly entries = new Map<string, DroppedSet>()

  get(key: string): DroppedSet | undefined {
    return this.entries.get(key)
  }

  set(key: string, value: DroppedSet): void {
    this.entries.set(key, value)
  }

  /** Drops every entry; used when the selection changes. */
  clear(): void {
    this.entries.clear()
  }

  get size(): number {
    return this.entries.size
  }
}

/** The default runner: spawn ctxed with a hard timeout, never throw. */
export function runCtxed(
  args: string[],
  timeoutMs: number = DEFAULT_TIMEOUT_MS,
  stdin?: string,
): Promise<RunResult> {
  return new Promise<RunResult>((resolve) => {
    let settled = false
    let stdout = ""
    let stderr = ""

    const child = spawn(args[0], args.slice(1), {
      stdio: [stdin === undefined ? "ignore" : "pipe", "pipe", "pipe"],
    })

    if (stdin !== undefined && child.stdin) {
      child.stdin.on("error", () => {
        // The child may exit before reading all input; the exit code still
        // tells the caller what happened.
      })
      child.stdin.end(stdin)
    }

    const finish = (result: RunResult): void => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      if (!child.killed) {
        try {
          child.kill("SIGKILL")
        } catch {
          // The child already exited; nothing to kill.
        }
      }
      resolve(result)
    }

    const timer = setTimeout(() => {
      finish({ code: -1, stdout, stderr, error: new Error(`ctxed timed out after ${timeoutMs}ms`) })
    }, timeoutMs)

    child.stdout?.on("data", (chunk: Buffer) => {
      stdout += chunk.toString()
    })
    child.stderr?.on("data", (chunk: Buffer) => {
      stderr += chunk.toString()
    })
    child.on("error", (error) => finish({ code: -1, stdout, stderr, error }))
    child.on("close", (code) => finish({ code: code ?? -1, stdout, stderr }))
  })
}

export function defaultRunner(config: PluginConfig): Runner {
  return (args, stdin) => runCtxed(args, config.timeoutMs, stdin)
}

/**
 * The runner for the in-session command. Same spawn, but on the command's own
 * (longer) budget, because the categorize step calls a model.
 */
export function commandRunner(config: PluginConfig): Runner {
  return (args, stdin) => runCtxed(args, config.commandTimeoutMs, stdin)
}

export interface ApplyInput<T extends LiveMessage> {
  messages: readonly T[]
  config: PluginConfig
  selection: Selection
  revision: string
  cache: PruneCache
  run: Runner
  report: (message: string, error?: unknown) => void
}

export interface ApplyResult<T extends LiveMessage> {
  messages: T[]
  droppedIds: string[]
  droppedToolCallIds: string[]
  dropped: number
  cached: boolean
  error?: string
}

/**
 * Resolves the dropped set (from cache or ctxed) and filters the messages.
 * Fail-open: any ctxed failure, timeout, or unparseable output leaves the
 * messages unchanged and reports the error instead of blocking the turn.
 *
 * The selection is re-derived over the **live** transcript every time the
 * revision changes, so a message added after the selection was recorded is
 * covered by it too.
 */
export async function applyPrune<T extends LiveMessage>(input: ApplyInput<T>): Promise<ApplyResult<T>> {
  const { messages, config, selection, revision, cache, run, report } = input

  if (!config.enabled) {
    return { messages: [...messages], droppedIds: [], droppedToolCallIds: [], dropped: 0, cached: false }
  }

  const key = cacheKey(config, selection, revision)
  let decision = cache.get(key)
  let cached = decision !== undefined

  if (decision === undefined) {
    const result = await run(buildArgs(config, selection), transcriptJson(messages))
    if (result.code !== 0) {
      const detail = (result.stderr || String(result.error ?? "")).trim()
      const error = `ctxed exited ${result.code}${detail === "" ? "" : `: ${detail}`}`
      report(`ctxed-opencode: ${error}`, result.error)
      return { messages: [...messages], droppedIds: [], droppedToolCallIds: [], dropped: 0, cached: false, error }
    }
    try {
      decision = parseDropped(result.stdout)
    } catch (cause) {
      const error = `ctxed output could not be parsed: ${(cause as Error).message}`
      report(`ctxed-opencode: ${error}`, cause)
      return { messages: [...messages], droppedIds: [], droppedToolCallIds: [], dropped: 0, cached: false, error }
    }
    cache.set(key, decision)
    cached = false
  }

  const filtered = filterDropped(messages, new Set(decision.ids), new Set(decision.toolCallIds))
  return {
    messages: filtered,
    droppedIds: decision.ids,
    droppedToolCallIds: decision.toolCallIds,
    dropped: messages.length - filtered.length,
    cached,
  }
}

// ---------------------------------------------------------------------------
// Command path: categorize the live transcript, then record a selection.
// ---------------------------------------------------------------------------

/** One bucket as ctxed reports it in a categories file. Ids stay internal. */
export interface Bucket {
  id: number
  label: string
  entryCount: number
  tokens: number
}

/**
 * Parses the buckets from a categories file for presentation. Only the fields
 * a human sees are read; entry ids are deliberately ignored here and never
 * surface to the user or leave this module.
 */
export function parseBuckets(categoriesJson: string): Bucket[] {
  let parsed: unknown
  try {
    parsed = JSON.parse(categoriesJson)
  } catch {
    throw new Error("categories file is not valid JSON")
  }
  if (parsed === null || typeof parsed !== "object") {
    throw new Error("categories file is not a JSON object")
  }
  const categories = (parsed as { categories?: unknown }).categories
  if (!Array.isArray(categories)) throw new Error("categories file has no categories array")
  const buckets: Bucket[] = []
  for (const raw of categories) {
    if (raw === null || typeof raw !== "object") continue
    const c = raw as Record<string, unknown>
    const id = typeof c.id === "number" ? c.id : Number.parseInt(String(c.id ?? ""), 10)
    if (!Number.isFinite(id)) continue
    buckets.push({
      id,
      label: typeof c.label === "string" ? c.label : `category ${id}`,
      entryCount: typeof c.entryCount === "number" ? c.entryCount : 0,
      tokens: typeof c.tokens === "number" ? c.tokens : 0,
    })
  }
  return buckets
}

/** Renders the buckets as a human-readable list. No ids appear. */
export function formatBuckets(buckets: readonly Bucket[]): string {
  const lines = buckets.map((b) => `  ${b.id}  ${b.label}  (${b.entryCount} entries, ${b.tokens} tokens)`)
  return ["Categories:", ...lines].join("\n")
}

export interface CategorizeResult {
  buckets: Bucket[]
  categoriesJson: string
  exitCode: number
  stdout: string
  stderr: string
  error?: string
}

/** Builds the ctxed invocation that categorizes a transcript on stdin. */
export function buildCategorizeArgs(config: PluginConfig): string[] {
  const args = [config.ctxedPath, "categorize", "-"]
  if (config.categorizerCmd !== "") args.push("--categorizer-cmd", config.categorizerCmd)
  if (config.categorizerModel !== "") args.push("--model", config.categorizerModel)
  if (config.maxCategories !== "") args.push("--max-categories", config.maxCategories)
  args.push("--out", "-")
  return args
}

/**
 * Runs ctxed's categorize over a serialized transcript. `--out -` makes ctxed
 * print the categories file to stdout, so no temp file is needed. The caller
 * serializes with the function matching its live shape. Fail-open: any failure
 * returns an error and no buckets, and the caller leaves the session unchanged.
 */
export async function categorizeLive(input: {
  transcript: string
  config: PluginConfig
  run: Runner
}): Promise<CategorizeResult> {
  const { transcript, config, run } = input
  const result = await run(buildCategorizeArgs(config), transcript)
  if (result.code !== 0) {
    const detail = (result.stderr || String(result.error ?? "")).trim()
    return {
      buckets: [],
      categoriesJson: "",
      exitCode: result.code,
      stdout: result.stdout,
      stderr: result.stderr,
      error: `ctxed exited ${result.code}${detail === "" ? "" : `: ${detail}`}`,
    }
  }
  // ctxed writes the human table to stdout for a file target, but with `--out -`
  // the JSON document is what stdout carries; take the last parsable object.
  const json = extractJsonObject(result.stdout) ?? result.stdout
  try {
    const buckets = parseBuckets(json)
    return { buckets, categoriesJson: json, exitCode: 0, stdout: result.stdout, stderr: result.stderr }
  } catch (cause) {
    return {
      buckets: [],
      categoriesJson: json,
      exitCode: 0,
      stdout: result.stdout,
      stderr: result.stderr,
      error: `ctxed categories could not be parsed: ${(cause as Error).message}`,
    }
  }
}

/**
 * Pulls the last top-level JSON object out of mixed output (a human table may
 * precede it), so the categorize path tolerates ctxed printing both.
 */
export function extractJsonObject(text: string): string | undefined {
  const start = text.indexOf("{")
  const end = text.lastIndexOf("}")
  if (start === -1 || end === -1 || end <= start) return undefined
  return text.slice(start, end + 1)
}

// ---------------------------------------------------------------------------
// Selection store
// ---------------------------------------------------------------------------

/** The recorded selection for one session. Only category ids, never entry ids. */
export interface Selection {
  categoriesFile: string
  categoryIds: string
  selectedAt: string
}

/** A storage key namespaced to this plugin and one session. */
export function selectionKey(sessionID: string): string {
  return `ctxed.prune.selection.${sessionID}`
}

/** Parses a stored selection, returning undefined if it is absent or malformed. */
export function parseSelection(raw: unknown): Selection | undefined {
  if (raw === null || raw === undefined || typeof raw !== "object") return undefined
  const s = raw as Record<string, unknown>
  const categoriesFile = typeof s.categoriesFile === "string" ? s.categoriesFile : ""
  const categoryIds = typeof s.categoryIds === "string" ? s.categoryIds : ""
  if (categoriesFile === "" || categoryIds === "") return undefined
  return {
    categoriesFile,
    categoryIds,
    selectedAt: typeof s.selectedAt === "string" ? s.selectedAt : "",
  }
}

/**
 * Parses a user's bucket selection ("1,3" or "drop 1 3") into a normalized
 * id list. Accepts ids only — never message ids.
 */
export function parseSelectionInput(input: string): string[] {
  return input
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter((s) => /^\d+$/.test(s))
}

/**
 * Matches a *bare* bucket selection: a message that is nothing but bucket ids,
 * optionally prefixed with "drop" (e.g. "3", "3,4", "drop 1 3"). Stricter than
 * parseSelectionInput, which extracts ids from anywhere in a sentence — so a
 * reply like "what about 3?" is not mistaken for a selection. Returns [] when
 * the whole message is not a selection.
 */
export function parseBareSelection(input: string): string[] {
  const text = input.trim()
  if (text === "") return []
  if (!/^(?:drop\s+)?\d+(?:[\s,]+\d+)*$/i.test(text)) return []
  return parseSelectionInput(text)
}

/** Keeps only the requested ids that name a real bucket, in the order asked. */
export function filterBucketIds(buckets: readonly Bucket[], ids: readonly string[]): string[] {
  return ids.filter((id) => buckets.some((bucket) => String(bucket.id) === id))
}

/** Names the selected buckets for a confirmation ("bucket 3 (plugin install)"). */
export function formatSelectedLabels(buckets: readonly Bucket[], ids: readonly string[]): string {
  return ids
    .map((id) => {
      const bucket = buckets.find((candidate) => String(candidate.id) === id)
      return bucket === undefined ? `bucket ${id}` : `bucket ${id} (${bucket.label})`
    })
    .join(", ")
}
