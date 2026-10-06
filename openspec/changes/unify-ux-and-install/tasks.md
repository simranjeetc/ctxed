# Tasks

## 1. Shared wording and verbs

- [ ] 1.1 Move bucket-table, prompt and confirmation text into ctxed (CLI `render` and `serve` responses); golden tests
- [ ] 1.2 OpenCode plugin prints only ctxed-rendered text; add `/ctxed-prune clear`
- [ ] 1.3 Claude Code: answer the "can a mod register a slash command" open question; implement `/ctxed-prune` (or skill → mod) with the same verbs and text
- [ ] 1.4 Rewrite `.claude/skills/ctxed-prune-context/SKILL.md` as a thin trigger into the same flow; remove the `${PWD//\//-}` transcript guess and the OpenCode categorizer dependency

## 2. Categorizer auto

- [ ] 2.1 `--categorizer auto` resolution order per design; embedded Claude Code categorizer script; tests with a stub `claude` on PATH
- [ ] 2.2 Make `auto` the default when nothing is configured; the "no model configured" error lists what was tried

## 3. Installer

- [ ] 3.1 Embed assets; `ctxed install opencode|claude` with scopes; records ctxed's absolute path for the shim
- [ ] 3.2 Shims read the recorded path first; PATH discovery is the fallback
- [ ] 3.3 `ctxed uninstall …` and `ctxed doctor` (versions, protocol, categorizer, install locations)
- [ ] 3.4 Tests: install into a temp project, re-install is a no-op, foreign file refused without `--force`, uninstall leaves no files

## 4. Conformance

- [ ] 4.1 `testdata/conformance/<harness>/*.json`: live input → expected buckets (with stub categorizer) and dropped ids
- [ ] 4.2 Go adapter tests, OpenCode plugin tests and Claude mod tests (`claude plugin test`) all run the same fixtures

## 5. Verification and docs

- [ ] 5.1 Functional suite installs via `ctxed install` into scratch projects (replaces the hand-built bundle step and makes the self-config scenario "the installer ran")
- [ ] 5.2 README: one flow section; the two-flow table removed once D3b ships, or reduced to the guarantee difference until then
- [ ] 5.3 `openspec validate unify-ux-and-install --strict` passes; all suites green
