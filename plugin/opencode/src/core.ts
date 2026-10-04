// ctxed-opencode core.
//
// This module does exactly three things: run ctxed in id-only prune mode,
// parse the `droppedIds` set it prints, and remove live messages whose id is in
// that set. Every decision — grouping entries, resolving a selection, and
// keeping the result structurally sound — lives in ctxed. This module holds no
// policy of its own.

import { spawn } from "node:child_process"

/** A live OpenCode outbound message. Only `id` is relevant here. */
export interface LiveMessage {
  id?: string
  [key: string]: unknown
}

export const DEFAULT_TIMEOUT_MS = 2000

export interface PluginConfig {
  /** True when a session export and a selection are both configured. */
  enabled: boolean
  ctxedPath: string
  sessionExport: string
  categoriesFile: string
  categories: string
  ids: string
  timeoutMs: number
}

export interface RunResult {
  code: number
  stdout: string
  stderr: string
  error?: Error
}

export type Runner = (args: string[]) => Promise<RunResult>

const OPTION_NAMES: Record<keyof Omit<PluginConfig, "enabled">, string> = {
  ctxedPath: "ctxedPath",
  sessionExport: "sessionExport",
  categoriesFile: "categoriesFile",
  categories: "categories",
  ids: "ids",
  timeoutMs: "timeoutMs",
}

const ENV_NAMES: Record<keyof Omit<PluginConfig, "enabled">, string> = {
  ctxedPath: "CTXED_PLUGIN_CTXED_PATH",
  sessionExport: "CTXED_PLUGIN_SESSION_EXPORT",
  categoriesFile: "CTXED_PLUGIN_CATEGORIES_FILE",
  categories: "CTXED_PLUGIN_CATEGORIES",
  ids: "CTXED_PLUGIN_IDS",
  timeoutMs: "CTXED_PLUGIN_TIMEOUT_MS",
}

function asString(value: unknown): string {
  if (typeof value === "number") return String(value)
  if (typeof value === "string") return value.trim()
  return ""
}

/**
 * Reads plugin configuration from OpenCode plugin options first, falling back
 * to environment variables. A missing session export or selection disables the
 * plugin, which makes it a no-op rather than an error.
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
  const sessionExport = pick("sessionExport")
  const categoriesFile = pick("categoriesFile")
  const categories = pick("categories")
  const ids = pick("ids")

  let timeoutMs = Number.parseInt(pick("timeoutMs"), 10)
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) timeoutMs = DEFAULT_TIMEOUT_MS

  const hasCategorySelection = categoriesFile !== "" && categories !== ""
  const hasIdSelection = ids !== ""
  return {
    enabled: sessionExport !== "" && (hasCategorySelection || hasIdSelection),
    ctxedPath,
    sessionExport,
    categoriesFile,
    categories,
    ids,
    timeoutMs,
  }
}

/** Builds the exact ctxed invocation the plugin relies on. */
export function buildArgs(config: PluginConfig): string[] {
  const args = [config.ctxedPath, "prune", config.sessionExport]
  if (config.categoriesFile !== "" && config.categories !== "") {
    args.push("--categories-file", config.categoriesFile, "--categories", config.categories)
  } else {
    args.push("--ids", config.ids)
  }
  args.push("--ids-only")
  return args
}

/** A compact fingerprint of the configured invocation inputs. */
export function selectionSignature(config: PluginConfig): string {
  return JSON.stringify([
    config.ctxedPath,
    config.sessionExport,
    config.categoriesFile,
    config.categories,
    config.ids,
  ])
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

export function cacheKey(config: PluginConfig, revision: string): string {
  return `${selectionSignature(config)}\n${revision}`
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
 * Removes only messages whose id is in the dropped set. Order and every other
 * message are preserved; the input array is not mutated.
 */
export function filterMessages<T extends LiveMessage>(
  messages: readonly T[],
  dropped: ReadonlySet<string>,
): T[] {
  return messages.filter((message) => !(typeof message.id === "string" && dropped.has(message.id)))
}

/** Caches dropped-id results, keyed by selection and revision. */
export class PruneCache {
  private readonly entries = new Map<string, string[]>()

  get(key: string): string[] | undefined {
    return this.entries.get(key)
  }

  set(key: string, ids: string[]): void {
    this.entries.set(key, ids)
  }

  get size(): number {
    return this.entries.size
  }
}

/** The default runner: spawn ctxed with a hard timeout, never throw. */
export function runCtxed(args: string[], timeoutMs: number = DEFAULT_TIMEOUT_MS): Promise<RunResult> {
  return new Promise<RunResult>((resolve) => {
    let settled = false
    let stdout = ""
    let stderr = ""

    const child = spawn(args[0], args.slice(1), { stdio: ["ignore", "pipe", "pipe"] })

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
  return (args) => runCtxed(args, config.timeoutMs)
}

export interface ApplyInput<T extends LiveMessage> {
  messages: readonly T[]
  config: PluginConfig
  revision: string
  cache: PruneCache
  run: Runner
  report: (message: string, error?: unknown) => void
}

export interface ApplyResult<T extends LiveMessage> {
  messages: T[]
  droppedIds: string[]
  dropped: number
  cached: boolean
  error?: string
}

/**
 * Resolves the dropped set (from cache or ctxed) and filters the messages.
 * Fail-open: any ctxed failure, timeout, or unparseable output leaves the
 * messages unchanged and reports the error instead of blocking the turn.
 */
export async function applyPrune<T extends LiveMessage>(input: ApplyInput<T>): Promise<ApplyResult<T>> {
  const { messages, config, revision, cache, run, report } = input

  if (!config.enabled) {
    return { messages: [...messages], droppedIds: [], dropped: 0, cached: false }
  }

  const key = cacheKey(config, revision)
  let droppedIds = cache.get(key)
  let cached = droppedIds !== undefined

  if (droppedIds === undefined) {
    const result = await run(buildArgs(config))
    if (result.code !== 0) {
      const detail = (result.stderr || String(result.error ?? "")).trim()
      const error = `ctxed exited ${result.code}${detail === "" ? "" : `: ${detail}`}`
      report(`ctxed-opencode: ${error}`, result.error)
      return { messages: [...messages], droppedIds: [], dropped: 0, cached: false, error }
    }
    try {
      droppedIds = parseDroppedIds(result.stdout)
    } catch (cause) {
      const error = `ctxed output could not be parsed: ${(cause as Error).message}`
      report(`ctxed-opencode: ${error}`, cause)
      return { messages: [...messages], droppedIds: [], dropped: 0, cached: false, error }
    }
    cache.set(key, droppedIds)
    cached = false
  }

  const droppedSet = new Set(droppedIds)
  const filtered = filterMessages(messages, droppedSet)
  return {
    messages: filtered,
    droppedIds,
    dropped: messages.length - filtered.length,
    cached,
  }
}
