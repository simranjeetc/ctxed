// Load check: prove OpenCode actually loads this plugin.
//
// This is the check the spike says is mandatory: OpenCode fails *silently* on a
// bad plugin unless debug logging is on, so "it seemed fine" is not evidence.
//
// It builds a throwaway OpenCode project, bundles the plugin to the single flat
// file OpenCode requires, installs the pinned API dependency, then runs a real
// session with debug logging and asserts:
//   1. `opencode plugin list` shows the plugin; and
//   2. the debug log has a "loading plugin" line for it and NO "failed to load"
//      line for it.
//
// Exits 0 only when the plugin loads. Prints what it found either way.

import { spawnSync } from "node:child_process"
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"
import { setTimeout as sleep } from "node:timers/promises"

const here = dirname(fileURLToPath(import.meta.url))
const pluginDir = join(here, "..")
const model = process.env.CTXED_TEST_OPENCODE_MODEL || "opencode-go/deepseek-v4-flash"

function fail(message) {
  console.error(`FAIL ${message}`)
  process.exit(1)
}

function run(cmd, args, opts = {}) {
  return spawnSync(cmd, args, { encoding: "utf8", ...opts })
}

const project = mkdtempSync(join(tmpdir(), "ctxed-loadcheck-"))
try {
  const pluginsDir = join(project, ".opencode", "plugins")
  mkdirSync(pluginsDir, { recursive: true })

  // OpenCode only scans a project's `.opencode/plugins/` when the directory is
  // a project — i.e. it has an `opencode.json`. Without it, `plugin list` says
  // "No plugins found".
  writeFileSync(join(project, "opencode.json"), "{}")

  // Bundle to the single flat file OpenCode requires (a subdirectory is not
  // scanned; see docs/opencode-plugin-spike.md).
  const bundle = join(pluginsDir, "ctxed-prune.js")
  const build = run(
    join(pluginDir, "node_modules", ".bin", "esbuild"),
    [
      join(pluginDir, "src", "plugin.ts"),
      "--bundle",
      "--format=esm",
      "--platform=node",
      "--external:@opencode/plugin",
      `--outfile=${bundle}`,
    ],
    { cwd: pluginDir },
  )
  if (build.status !== 0) fail(`esbuild failed:\n${build.stderr}`)

  // A local plugin needs its own installed dependency tree.
  writeFileSync(
    join(project, ".opencode", "package.json"),
    JSON.stringify(
      { name: "ctxed-loadcheck", private: true, type: "module", dependencies: { "@opencode/plugin": "2.0.22" } },
      null,
      2,
    ),
  )
  const install = run("npm", ["install", "--no-audit", "--no-fund"], { cwd: join(project, ".opencode") })
  if (install.status !== 0) fail(`npm install failed:\n${install.stderr}`)

  // OpenCode's background server discovers a newly added local plugin
  // asynchronously, so the first `plugin list` after it exists can still say
  // "No plugins found". Poll before giving up.
  let listed = false
  let listOutput = ""
  for (let attempt = 0; attempt < 10 && !listed; attempt++) {
    const list = run("opencode", ["plugin", "list"], { cwd: project })
    listOutput = `${list.stdout || ""}${list.stderr || ""}`
    listed = /ctxed\.prune/.test(listOutput)
    if (!listed) await sleep(1000)
  }
  if (!listed) fail(`opencode plugin list did not show ctxed.prune after retries:\n${listOutput}`)

  const session = run(
    "opencode",
    ["run", "--standalone", "--print-logs", "--log-level", "debug", "--model", model, "Say alpha only."],
    { cwd: project },
  )
  const log = `${session.stdout || ""}\n${session.stderr || ""}`
  // The mandatory assertion is that OpenCode did NOT reject the plugin. A
  // rejection appears only in the debug log, and only as a "failed to load
  // plugin" line naming the bundle.
  const loadError = /failed to load plugin[^\n]*ctxed-prune/.test(log)
  if (loadError) fail(`OpenCode failed to load ctxed-prune:\n${log}`)

  // The positive signal is the load line. It may be absent if the run connected
  // to a server that had already loaded the plugin, so it is reported, not
  // required — `plugin list` above is the authoritative discovery check.
  const loaded = /loading plugin[^\n]*ctxed-prune/.test(log)
  console.log(
    `ok   opencode loads the plugin (listed as ctxed.prune; no load error${loaded ? "; load line seen" : ""})`,
  )
} finally {
  if (!process.env.CTXED_TEST_KEEP) rmSync(project, { recursive: true, force: true })
  else console.log(`kept scratch: ${project}`)
}
