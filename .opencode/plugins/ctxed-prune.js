// src/plugin.ts
import { appendFileSync, existsSync as existsSync2, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join as join2 } from "node:path";
import { fileURLToPath } from "node:url";
import { Plugin } from "@opencode/plugin";

// src/core.ts
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

// src/transcript.ts
function entryType(value) {
  if (typeof value === "string" && value !== "") return value;
  return "system";
}
function renderParts(parts) {
  const out = [];
  for (const part of parts ?? []) {
    switch (part.type) {
      case "text":
        out.push({ type: "text", text: part.text ?? "" });
        break;
      case "reasoning":
        out.push({ type: "reasoning", text: part.text ?? "" });
        break;
      case "tool-call":
        out.push({ type: "tool", id: part.id ?? "", name: part.name ?? "", state: part.input ?? part.state ?? {} });
        break;
      case "tool-result":
        out.push({ type: "tool", id: part.id ?? "", name: part.name ?? "", state: part.result ?? part.state ?? {} });
        break;
      case "tool":
        out.push({ type: "tool", id: part.id ?? "", name: part.name ?? "", state: part.state ?? {} });
        break;
      default:
        break;
    }
  }
  return out;
}
function textPart(text) {
  return typeof text === "string" && text !== "" ? [{ type: "text", text }] : [];
}
function serializeHookMessages(messages) {
  return {
    messages: messages.map((message) => ({
      id: typeof message.id === "string" ? message.id : "",
      type: entryType(message.role),
      content: renderParts(message.content)
    }))
  };
}
function serializeContextMessages(messages) {
  return {
    messages: messages.map((message) => {
      const parts = renderParts(message.content);
      const content = parts.length > 0 ? parts : textPart(message.text);
      return {
        id: typeof message.id === "string" ? message.id : "",
        type: entryType(message.type),
        content
      };
    })
  };
}
function transcriptJson(messages) {
  return JSON.stringify(serializeHookMessages(messages));
}

// src/core.ts
var DEFAULT_TIMEOUT_MS = 2e3;
var DEFAULT_COMMAND_TIMEOUT_MS = 6e4;
var OPTION_NAMES = {
  ctxedPath: "ctxedPath",
  categorizerCmd: "categorizerCmd",
  categorizerModel: "categorizerModel",
  maxCategories: "maxCategories",
  timeoutMs: "timeoutMs",
  commandTimeoutMs: "commandTimeoutMs",
  debugLog: "debugLog"
};
var ENV_NAMES = {
  ctxedPath: "CTXED_PLUGIN_CTXED_PATH",
  categorizerCmd: "CTXED_PLUGIN_CATEGORIZER_CMD",
  categorizerModel: "CTXED_PLUGIN_CATEGORIZER_MODEL",
  maxCategories: "CTXED_PLUGIN_MAX_CATEGORIES",
  timeoutMs: "CTXED_PLUGIN_TIMEOUT_MS",
  commandTimeoutMs: "CTXED_PLUGIN_COMMAND_TIMEOUT_MS",
  debugLog: "CTXED_PLUGIN_DEBUG_LOG"
};
function asString(value) {
  if (typeof value === "number") return String(value);
  if (typeof value === "string") return value.trim();
  return "";
}
function loadConfig(options = {}, env = {}) {
  const pick = (key) => {
    const fromOptions = asString(options[OPTION_NAMES[key]]);
    if (fromOptions !== "") return fromOptions;
    return asString(env[ENV_NAMES[key]]);
  };
  const ctxedPath = pick("ctxedPath") || "ctxed";
  const categorizerCmd = pick("categorizerCmd");
  const categorizerModel = pick("categorizerModel");
  const maxCategories = pick("maxCategories");
  const debugLog = pick("debugLog");
  let timeoutMs = Number.parseInt(pick("timeoutMs"), 10);
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) timeoutMs = DEFAULT_TIMEOUT_MS;
  let commandTimeoutMs = Number.parseInt(pick("commandTimeoutMs"), 10);
  if (!Number.isFinite(commandTimeoutMs) || commandTimeoutMs <= 0) commandTimeoutMs = DEFAULT_COMMAND_TIMEOUT_MS;
  return {
    enabled: true,
    ctxedPath,
    categorizerCmd,
    categorizerModel,
    maxCategories,
    timeoutMs,
    commandTimeoutMs,
    debugLog
  };
}
function resolveCtxedPath(configured) {
  if (configured.includes("/")) return configured;
  const home = homedir();
  const candidates = [
    join(home, "go", "bin", "ctxed"),
    "/opt/homebrew/bin/ctxed",
    "/usr/local/bin/ctxed",
    join(home, ".local", "bin", "ctxed"),
    join(home, ".opencode", "bin", "ctxed")
  ];
  for (const candidate of candidates) {
    if (existsSync(candidate)) return candidate;
  }
  return configured;
}
function buildArgs(config, selection) {
  return [
    config.ctxedPath,
    "prune",
    "-",
    "--categories-file",
    selection.categoriesFile,
    "--categories",
    selection.categoryIds,
    "--ids-only"
  ];
}
function selectionSignature(config, selection) {
  return JSON.stringify([config.ctxedPath, selection.categoriesFile, selection.categoryIds]);
}
function sessionRevision(messages) {
  const ids = [];
  for (const message of messages) ids.push(typeof message.id === "string" ? message.id : "?");
  return ids.join("|");
}
function cacheKey(config, selection, revision) {
  return `${selectionSignature(config, selection)}
${revision}`;
}
function parseDroppedIds(stdout) {
  let parsed;
  try {
    parsed = JSON.parse(stdout);
  } catch {
    throw new Error("ctxed output is not valid JSON");
  }
  if (parsed === null || typeof parsed !== "object") {
    throw new Error("ctxed output is not a JSON object");
  }
  const dropped = parsed.droppedIds;
  if (!Array.isArray(dropped)) throw new Error("ctxed output has no droppedIds array");
  for (const id of dropped) {
    if (typeof id !== "string") throw new Error("ctxed output droppedIds contains a non-string id");
  }
  return dropped;
}
function parseDropped(stdout) {
  const ids = parseDroppedIds(stdout);
  let toolCallIds = [];
  try {
    const parsed = JSON.parse(stdout);
    if (Array.isArray(parsed.droppedToolCallIds)) {
      toolCallIds = parsed.droppedToolCallIds.filter((x) => typeof x === "string");
    }
  } catch {
  }
  return { ids, toolCallIds };
}
function carriesDroppedToolCall(message, droppedToolCalls) {
  const content = message.content;
  if (!Array.isArray(content)) return false;
  for (const part of content) {
    if (part === null || typeof part !== "object") continue;
    const p = part;
    if ((p.type === "tool-call" || p.type === "tool-result" || p.type === "tool") && typeof p.id === "string" && droppedToolCalls.has(p.id)) {
      return true;
    }
  }
  return false;
}
function filterDropped(messages, dropped, droppedToolCalls) {
  return messages.filter(
    (message) => !(typeof message.id === "string" && dropped.has(message.id)) && !carriesDroppedToolCall(message, droppedToolCalls)
  );
}
var PruneCache = class {
  entries = /* @__PURE__ */ new Map();
  get(key) {
    return this.entries.get(key);
  }
  set(key, value) {
    this.entries.set(key, value);
  }
  /** Drops every entry; used when the selection changes. */
  clear() {
    this.entries.clear();
  }
  get size() {
    return this.entries.size;
  }
};
function runCtxed(args, timeoutMs = DEFAULT_TIMEOUT_MS, stdin) {
  return new Promise((resolve) => {
    let settled = false;
    let stdout = "";
    let stderr = "";
    const child = spawn(args[0], args.slice(1), {
      stdio: [stdin === void 0 ? "ignore" : "pipe", "pipe", "pipe"]
    });
    if (stdin !== void 0 && child.stdin) {
      child.stdin.on("error", () => {
      });
      child.stdin.end(stdin);
    }
    const finish = (result) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      if (!child.killed) {
        try {
          child.kill("SIGKILL");
        } catch {
        }
      }
      resolve(result);
    };
    const timer = setTimeout(() => {
      finish({ code: -1, stdout, stderr, error: new Error(`ctxed timed out after ${timeoutMs}ms`) });
    }, timeoutMs);
    child.stdout?.on("data", (chunk) => {
      stdout += chunk.toString();
    });
    child.stderr?.on("data", (chunk) => {
      stderr += chunk.toString();
    });
    child.on("error", (error) => finish({ code: -1, stdout, stderr, error }));
    child.on("close", (code) => finish({ code: code ?? -1, stdout, stderr }));
  });
}
function defaultRunner(config) {
  return (args, stdin) => runCtxed(args, config.timeoutMs, stdin);
}
function commandRunner(config) {
  return (args, stdin) => runCtxed(args, config.commandTimeoutMs, stdin);
}
async function applyPrune(input) {
  const { messages, config, selection, revision, cache, run, report } = input;
  if (!config.enabled) {
    return { messages: [...messages], droppedIds: [], droppedToolCallIds: [], dropped: 0, cached: false };
  }
  const key = cacheKey(config, selection, revision);
  let decision = cache.get(key);
  let cached = decision !== void 0;
  if (decision === void 0) {
    const result = await run(buildArgs(config, selection), transcriptJson(messages));
    if (result.code !== 0) {
      const detail = (result.stderr || String(result.error ?? "")).trim();
      const error = `ctxed exited ${result.code}${detail === "" ? "" : `: ${detail}`}`;
      report(`ctxed-opencode: ${error}`, result.error);
      return { messages: [...messages], droppedIds: [], droppedToolCallIds: [], dropped: 0, cached: false, error };
    }
    try {
      decision = parseDropped(result.stdout);
    } catch (cause) {
      const error = `ctxed output could not be parsed: ${cause.message}`;
      report(`ctxed-opencode: ${error}`, cause);
      return { messages: [...messages], droppedIds: [], droppedToolCallIds: [], dropped: 0, cached: false, error };
    }
    cache.set(key, decision);
    cached = false;
  }
  const filtered = filterDropped(messages, new Set(decision.ids), new Set(decision.toolCallIds));
  return {
    messages: filtered,
    droppedIds: decision.ids,
    droppedToolCallIds: decision.toolCallIds,
    dropped: messages.length - filtered.length,
    cached
  };
}
function parseBuckets(categoriesJson) {
  let parsed;
  try {
    parsed = JSON.parse(categoriesJson);
  } catch {
    throw new Error("categories file is not valid JSON");
  }
  if (parsed === null || typeof parsed !== "object") {
    throw new Error("categories file is not a JSON object");
  }
  const categories = parsed.categories;
  if (!Array.isArray(categories)) throw new Error("categories file has no categories array");
  const buckets = [];
  for (const raw of categories) {
    if (raw === null || typeof raw !== "object") continue;
    const c = raw;
    const id = typeof c.id === "number" ? c.id : Number.parseInt(String(c.id ?? ""), 10);
    if (!Number.isFinite(id)) continue;
    buckets.push({
      id,
      label: typeof c.label === "string" ? c.label : `category ${id}`,
      entryCount: typeof c.entryCount === "number" ? c.entryCount : 0,
      tokens: typeof c.tokens === "number" ? c.tokens : 0
    });
  }
  return buckets;
}
function formatBuckets(buckets) {
  const lines = buckets.map((b) => `  ${b.id}  ${b.label}  (${b.entryCount} entries, ${b.tokens} tokens)`);
  return ["Categories:", ...lines].join("\n");
}
function buildCategorizeArgs(config) {
  const args = [config.ctxedPath, "categorize", "-"];
  if (config.categorizerCmd !== "") args.push("--categorizer-cmd", config.categorizerCmd);
  if (config.categorizerModel !== "") args.push("--model", config.categorizerModel);
  if (config.maxCategories !== "") args.push("--max-categories", config.maxCategories);
  args.push("--out", "-");
  return args;
}
async function categorizeLive(input) {
  const { transcript, config, run } = input;
  const result = await run(buildCategorizeArgs(config), transcript);
  if (result.code !== 0) {
    const detail = (result.stderr || String(result.error ?? "")).trim();
    return {
      buckets: [],
      categoriesJson: "",
      exitCode: result.code,
      stdout: result.stdout,
      stderr: result.stderr,
      error: `ctxed exited ${result.code}${detail === "" ? "" : `: ${detail}`}`
    };
  }
  const json = extractJsonObject(result.stdout) ?? result.stdout;
  try {
    const buckets = parseBuckets(json);
    return { buckets, categoriesJson: json, exitCode: 0, stdout: result.stdout, stderr: result.stderr };
  } catch (cause) {
    return {
      buckets: [],
      categoriesJson: json,
      exitCode: 0,
      stdout: result.stdout,
      stderr: result.stderr,
      error: `ctxed categories could not be parsed: ${cause.message}`
    };
  }
}
function extractJsonObject(text) {
  const start = text.indexOf("{");
  const end = text.lastIndexOf("}");
  if (start === -1 || end === -1 || end <= start) return void 0;
  return text.slice(start, end + 1);
}
function selectionKey(sessionID) {
  return `ctxed.prune.selection.${sessionID}`;
}
function parseSelection(raw) {
  if (raw === null || raw === void 0 || typeof raw !== "object") return void 0;
  const s = raw;
  const categoriesFile = typeof s.categoriesFile === "string" ? s.categoriesFile : "";
  const categoryIds = typeof s.categoryIds === "string" ? s.categoryIds : "";
  if (categoriesFile === "" || categoryIds === "") return void 0;
  return {
    categoriesFile,
    categoryIds,
    selectedAt: typeof s.selectedAt === "string" ? s.selectedAt : ""
  };
}
function parseSelectionInput(input) {
  return input.split(/[\s,]+/).map((s) => s.trim()).filter((s) => /^\d+$/.test(s));
}

// src/plugin.ts
var COMMAND_NAME = "ctxed-prune";
var CtxedPrunePlugin = Plugin.define({
  id: "ctxed.prune",
  async setup(ctx) {
    const config = loadConfig(ctx.options, process.env);
    config.ctxedPath = resolveCtxedPath(config.ctxedPath);
    if (config.categorizerCmd === "" && config.categorizerModel === "") {
      const bundled = resolveBundledCategorizer();
      if (bundled !== "") config.categorizerCmd = bundled;
    }
    const cache = new PruneCache();
    const run = defaultRunner(config);
    const runCommand = commandRunner(config);
    const report = (message, error) => {
      if (error === void 0) console.error(message);
      else console.error(message, error);
    };
    const say = async (sessionID, text) => {
      try {
        await ctx.session.synthetic({ sessionID, text });
      } catch (error) {
        report(`ctxed-opencode: could not surface output: ${text}`, error);
      }
    };
    await ctx.command.transform((editor) => {
      editor.add({
        name: COMMAND_NAME,
        description: "Prune the session's context by topic bucket (ctxed).",
        async execute({ sessionID, prompt }) {
          const messages = await sessionMessages(ctx, sessionID);
          if (messages.length === 0) {
            await say(sessionID, `No session messages to categorize yet; send a message, then /${COMMAND_NAME}.`);
            return;
          }
          const result = await categorizeLive({
            transcript: JSON.stringify(serializeContextMessages(messages)),
            config,
            run: runCommand
          });
          if (result.error !== void 0) {
            await say(sessionID, `ctxed-opencode: categorize failed: ${result.error}`);
            return;
          }
          const selected = parseSelectionInput(commandText(prompt));
          if (selected.length === 0) {
            await say(
              sessionID,
              `${formatBuckets(result.buckets)}
Select buckets with /${COMMAND_NAME} <ids> (e.g. /${COMMAND_NAME} 1,3).`
            );
            return;
          }
          const categoriesFile = writeCategoriesFile(sessionID, result.categoriesJson);
          const categoryIds = selected.join(",");
          await ctx.storage.set(selectionKey(sessionID), {
            categoriesFile,
            categoryIds,
            selectedAt: (/* @__PURE__ */ new Date()).toISOString()
          });
          cache.clear();
          await say(sessionID, `Selected buckets ${categoryIds}; dropped on every subsequent dispatch.`);
        }
      });
    });
    await ctx.session.hook("context", async (event) => {
      const messages = event.messages;
      if (messages.length === 0) return;
      const sessionID = String(event.sessionID ?? "");
      const selection = parseSelection(await ctx.storage.get(selectionKey(sessionID)));
      if (selection === void 0) return;
      const result = await applyPrune({
        messages,
        config,
        selection,
        revision: sessionRevision(messages),
        cache,
        run,
        report
      });
      if (config.debugLog !== "") {
        debugDecide(config.debugLog, messages, result);
      }
      if (result.dropped > 0) {
        event.messages = result.messages;
      }
    });
  }
});
function debugDecide(logPath, messages, result) {
  try {
    const ids = (list) => list.map((m) => typeof m.id === "string" ? m.id : null);
    appendFileSync(logPath, JSON.stringify({ before: ids(messages), after: ids(result.messages) }) + "\n");
  } catch {
  }
}
function resolveBundledCategorizer() {
  const candidates = [
    "../../scripts/ctxed-categorizer-opencode.sh",
    "../../../scripts/ctxed-categorizer-opencode.sh"
  ];
  for (const relative of candidates) {
    try {
      const path = fileURLToPath(new URL(relative, import.meta.url));
      if (existsSync2(path)) return path;
    } catch {
    }
  }
  return "";
}
function commandText(prompt) {
  if (typeof prompt === "string") return prompt;
  if (prompt !== null && typeof prompt === "object") {
    const text = prompt.text;
    if (typeof text === "string") return text;
  }
  return "";
}
async function sessionMessages(ctx, sessionID) {
  try {
    const messages = await ctx.session.context({ sessionID });
    return Array.isArray(messages) ? messages : [];
  } catch {
    return [];
  }
}
function writeCategoriesFile(sessionID, categoriesJson) {
  const dir = join2(tmpdir(), "ctxed-opencode");
  mkdirSync(dir, { recursive: true });
  const path = join2(dir, `${sessionID}.categories.json`);
  writeFileSync(path, categoriesJson, "utf8");
  return path;
}
var plugin_default = CtxedPrunePlugin;
export {
  COMMAND_NAME,
  CtxedPrunePlugin,
  plugin_default as default
};
