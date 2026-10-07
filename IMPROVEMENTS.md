# Improvements backlog

Run with `scripts/improve-loop.sh`. Each task passes through three hands:

| Role | Who | May change |
| --- | --- | --- |
| Implementer | opencode, `IMPROVE_MODEL` (default `opencode-go/deepseek-v4-pro`) | source, tests, docs for its one task |
| Checks | the script | nothing; runs gofmt, build, vet, test and the task's Check |
| Reviewer | `REVIEW_MODEL` (default sonnet) | nothing; read-only, judges the diff |

The script ticks the box and commits, only after the checks and the reviewer pass.
Task text and checks are always read from the last commit, so editing this file
in the working tree changes nothing; the implementer touching it fails the task.

Rules for the implementer:

1. Do only the task named in your prompt. Do not start or touch any other task.
2. Do not edit `IMPROVEMENTS.md` or `scripts/improve-loop.sh`. Do not tick, commit, or stage.
3. Do not delete, skip or weaken existing tests to make a check pass.
4. Run the task's **Check** (the one command in backticks) and `go test ./...`
   yourself before you stop.
5. If you cannot finish, write the reason to `.verify/improve/<task id>.blocked`
   and stop.
6. In a move task, "unchanged" covers the code, not file-level metadata. A file
   you create starts with a bare `package cli`; a package keeps exactly one
   package doc comment, in the file that already had it. Copy nothing else
   (imports are re-derived by the tools).
7. Teardown must not inherit the caller's cancellation. When you put a deadline
   on cleanup (a session delete, a temp-file removal), derive it from
   `context.WithoutCancel(ctx)`, not from `ctx`: cleanup runs precisely when the
   caller was interrupted or timed out.

Tasks marked `(manual)` need the owner. Skip a task marked `(needs Txx)`
while Txx is unticked. Change a task or its Check only by editing and committing
this file yourself.

---

## Before sharing

- [x] **T01 (manual) Create a public GitHub repo and push.**
  `git remote add origin <url> && git push -u origin main`.

- [x] **T02 (manual) Choose a license.** Chosen: MIT.

- [x] **T03 (needs T02) Add the LICENSE file.** Put the MIT license text in `LICENSE`, with the copyright holder "Simranjeet Singh Chawla" and the year 2026.
  Check: `test -f LICENSE`

- [x] **T04 gofmt the tree.** Run `gofmt -w .`; change nothing else.
  Check: `test -z "$(gofmt -l .)"`.

- [x] **T05 Fix the staticcheck finding.** `internal/harness/claude.go:21`: the error string starts with a capital letter (ST1005). Make it lowercase.
  Check: `~/go/bin/staticcheck ./...` prints nothing.

- [x] **T06 Fix the main.go doc comment.** `cmd/ctxed/main.go` says "inspects and edits"; ctxed is read-only now. Change it to "shows what an agent session's context is made of".
  Check: `grep -q "made of" cmd/ctxed/main.go`.

- [x] **T07 Add a Makefile** with targets `build`, `test` (`go test -race ./...`), `lint` (gofmt check, `go vet`, staticcheck) and `install` (`go install ./cmd/ctxed`).
  Check: `make lint test`.

- [x] **T08 Add CI.** Create `.github/workflows/ci.yml`. On push and pull_request it runs on ubuntu-latest with Go from `go.mod`, then a gofmt check, `go vet ./...`, staticcheck (`dominikh/staticcheck-action`), `go test -race ./...` and `govulncheck ./...`.
  Check: `test -f .github/workflows/ci.yml && grep -q "go test -race" .github/workflows/ci.yml`

- [x] **T09 Set the version at build time.** In `internal/cli/cli.go`, change `const version = "0.1.0"` to `var version = "dev"`. If it is still "dev", read the module version from `debug.ReadBuildInfo()`. Document `-ldflags "-X github.com/simranjeetc/ctxed/internal/cli.version=…"` in a comment.
  Check: `go run ./cmd/ctxed version | grep -qE 'dev|devel'`

- [x] **T10 Add GoReleaser.** Add `.goreleaser.yaml` (darwin and linux, amd64 and arm64, with ldflags setting the version from T09) and `.github/workflows/release.yml`, which runs it on `v*` tags.
  Check: `test -f .goreleaser.yaml && test -f .github/workflows/release.yml`

- [x] **T11a Move the parked tests into their own file.** A pure move; no build tag yet, no other file touched. Create `internal/cli/parked_test.go` (`package cli_test`) and move these 33 test functions into it from `internal/cli/cli_test.go`, each unchanged: TestCategorizeCompactedSeesOnlyLiveEntries, TestCompactInstructionByCategory, TestCompactInstructionIsDeterministic, TestCompactInstructionMissingSelectionIsUsageError, TestCompactInstructionStdinClosed, TestCompactInstructionUnknownCategoryRefused, TestDropCompactedKeepsHistory, TestDropDuplicateIndexRefused, TestDropInvalidIndexRefused, TestDropJSONStats, TestDropMissingIndicesIsUsageError, TestDropOrphanRefusedAndForceOverrides, TestDropOutOverride, TestDropWritesAndLeavesInputIntact, TestPluginRoleThinSubstitution, TestPruneByCategoryIsNonDestructive, TestPruneCompactedEntryIsUnknown, TestPruneDeterministic, TestPruneFromStdinIDsOnly, TestPruneHonorsEditedCategoriesFile, TestPruneIDsOnlyByCategory, TestPruneIDsOnlyByExplicitID, TestPruneIDsOnlyDeterministic, TestPruneIDsOnlyEmitsDroppedToolCallIDs, TestPruneIDsOnlyEmptySelection, TestPruneIDsOnlyNothingDropped, TestPruneIDsOnlyReflectsOrphanResolution, TestPruneMissingSelectionIsUsageError, TestPruneResolvesOrphanAndReports, TestPruneStdinClosed, TestPruneUnknownCategoryRefused, TestPruneUnknownIDRefused, TestStdinClosed. Also move these three, which only the moved tests use: the helper functions `categorizeTo` and `assertNoEditedFile`, and the constant `claudeCategorizeResponse` (delete its line from the `const (...)` block in `cli_test.go` and declare it in `parked_test.go`). Everything else stays in `cli_test.go`, unchanged. In particular these stay, even though some moved tests use them (both files are in the same package, so nothing needs copying or redeclaring): the blank import of `internal/adapter/builtin`; `run`, `runStdin`, `copyFixture`, `snapshot`; the constants `opencodeFixture`, `claudeFixture`, `ocCategorizeResponse`, `claudeCompactedFixture`, `compactedPreBoundaryID`, `compactedSummaryID`, `compactedTopicFourID`. Do not add any new constant inside a test function. Then fix the imports of both files: run `~/go/bin/goimports -w internal/cli/cli_test.go internal/cli/parked_test.go`.
  Check: `test $(grep -c '^func Test' internal/cli/parked_test.go) -eq 33 && grep -qE 'claudeCategorizeResponse *=' internal/cli/parked_test.go && ! grep -qE 'claudeCategorizeResponse *=' internal/cli/cli_test.go && test $(grep -cE '^func (categorizeTo|assertNoEditedFile)\(' internal/cli/parked_test.go) -eq 2 && test $(grep -c '^func Test' internal/cli/cli_test.go) -eq 15 && grep -q 'internal/adapter/builtin' internal/cli/cli_test.go && grep -q '^const claudeCompactedFixture' internal/cli/cli_test.go && test $(grep -cE '^\s*compacted(PreBoundary|Summary|TopicFour)ID *=' internal/cli/cli_test.go) -eq 3 && ! grep -qE 'claudeCompactedFixture *=|compacted[A-Za-z]*ID *=|^func (run|runStdin|copyFixture|snapshot)\(' internal/cli/parked_test.go && test $(cat internal/cli/cli_test.go internal/cli/parked_test.go | grep -cE '^\s+const ') -eq 3 && go test ./internal/cli && test -z "$(gofmt -l internal/cli)"`

- [x] **T11b Move the parked code into its own file.** A pure move; no build tag yet, and only `cli.go` and the new file change. Create `internal/cli/parked.go` (`package cli`) and move these from `internal/cli/cli.go`, each unchanged: the functions `runDrop`, `runPrune`, `runCompactInstruction`, `writeDroppedIDs`, `splitList`, `write`, `computeStats`, `defaultOut`, `human`, `signed`; the type `stats`; and the variables `valueFlagsDrop`, `valueFlagsPrune`, `valueFlagsCompact` (take them out of the `var (...)` block in `cli.go` into a `var (...)` block in `parked.go`). Everything else stays in `cli.go`, unchanged; in particular `splitArgs`, `readSession`, `envOr`, `categoriesOut`, `load`, `parseErrExit`, `fail`, `valueFlagsInspect` and `valueFlagsCategorize` stay even though moved code uses them (same package, so nothing needs copying). Then fix the imports of both files: run `~/go/bin/goimports -w internal/cli/cli.go internal/cli/parked.go`. `cli.go` must no longer import `internal/prune` or `internal/compact`.
  Check: `test $(grep -cE '^func (runDrop|runPrune|runCompactInstruction|writeDroppedIDs|splitList|write|computeStats|defaultOut|human|signed)\(' internal/cli/parked.go) -eq 10 && ! grep -qE '^func (runDrop|runPrune|runCompactInstruction|writeDroppedIDs|splitList|write|computeStats|defaultOut|human|signed)\(|valueFlags(Drop|Prune|Compact)|internal/(prune|compact)"' internal/cli/cli.go && test $(grep -cE '^func (splitArgs|readSession|envOr|categoriesOut|load|parseErrExit|fail)\(' internal/cli/cli.go) -eq 7 && ! grep -qE '^func (splitArgs|readSession|envOr|categoriesOut|load|parseErrExit|fail)\(' internal/cli/parked.go && go build ./... && go test ./internal/cli && test -z "$(gofmt -l internal/cli)"`

- [x] **T11c (needs T11b) Put the parked files behind a build tag.** Only these edits. (1) Make `//go:build parked` followed by a blank line the first lines of exactly these five files in `internal/cli`: `parked.go`, `opencode.go`, `parked_test.go`, `opencode_test.go`, `export_test.go`. (2) Create `internal/cli/export_live_test.go` whose first lines are `//go:build !parked` and a blank line, in `package cli`, defining `RunWithParked(args []string, stdin io.Reader, stdout, stderr io.Writer) int` that just returns `RunWithStdin(args, stdin, stdout, stderr)`, with a one-line comment saying the default build has no parked commands. This is needed because the test helper `run` in `cli_test.go` calls `cli.RunWithParked`, which otherwise exists only in the parked build. Change nothing else.
  Check: `! go list -deps ./cmd/ctxed | grep -qE 'internal/(prune|compact|ocprune)' && go test ./internal/cli && go test -tags parked ./... && go vet -tags parked ./... && ~/go/bin/staticcheck ./... && test -z "$(gofmt -l internal/cli)"`
  Tests-tag: `parked`

## Correctness

- [x] **T12 Accept a single topic.** `categorize.Parse` rejects fewer than 2 categories ("at least 2 are required"). Allow 1. Update the prompt text and tests to match.
  Check: `go test ./internal/categorize`

- [x] **T13 Cancel on Ctrl-C.** In `cmd/ctxed/main.go`, create a context with `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` and pass it down, replacing `context.Background()` in `internal/cli/overview.go`.
  Check: `grep -q NotifyContext cmd/ctxed/main.go`.

- [x] **T14 Wrap errors with %w.** In `internal/harness/*.go` and `internal/model/model.go`, change `fmt.Errorf(... %v ..., err)` to `%w` wherever an error value is wrapped.
  Check: `! grep -rnE 'Errorf\(.*%v.*err\)' internal/harness internal/model`

- [x] **T15 Handle the ReadAll error** in `internal/model/model.go` (`raw, _ := io.ReadAll(...)`).
  Check: `! grep -n 'raw, _ :=' internal/model/model.go`

- [x] **T16 Bound the opencode cleanup.** In `internal/harness/opencode.go`, run `session delete` with a 10s deadline of its own that ignores the caller's cancellation — `context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)` — so the delete still runs when T13's Ctrl-C cancels `ctx`, and print a failure to stderr instead of discarding it.
  Check: `! grep -rn '_ = exec.Command' internal/harness && go test ./internal/harness && grep -qE 'WithoutCancel|context\.Background\(\)' internal/harness/opencode.go`

- [x] **T17 Send the prompt to opencode on stdin, not as an argument.** In `OpenCodeRun.Complete`, pass the prompt on stdin if `opencode run` accepts it; otherwise write it to a temp file and attach it with `--file`. Verify the CLI's behaviour with `opencode run --help` first.
  Check: `go test ./internal/harness && ! grep -n '"--title", "ctxed categorize", prompt)' internal/harness/opencode.go`

- [x] **T18 Add a `--no-model` flag** to `ctxed overview`. It skips the categorizer and prints only the "Whole session" sizes. Document it in the README and the usage text.
  Check: `CTXED_CATEGORIZER_CMD=false go run ./cmd/ctxed overview testdata/claude_session.jsonl --no-model 2>&1 | grep -q "Whole session"`

- [x] **T19 Add a privacy note to the README.** It says that `overview` and `categorize` send excerpts of the session to the configured model, and that `--no-model` avoids it.
  Check: `grep -qi privacy README.md`.

## Tests

- [x] **T20 Unit tests for `internal/overview`.** Write table-driven tests for `Build` (placement, nearest-entry fallback, the summary row, sorting) and `Short`.
  Check: `go test -cover ./internal/overview | grep -qE 'coverage: ([89][0-9]|100)'`

- [x] **T21 Golden-file test for the overview table.** Save the rendered table as `internal/overview/testdata/table.golden`, compare against it, and regenerate it when `-update` is passed.
  Check: `test -f internal/overview/testdata/table.golden && go test ./internal/overview`

- [x] **T22 Tests for `internal/harness`.** Put a fake `opencode` and a fake `claude` shell script on a temp PATH, then test `OpenCodeBin`, `ExportOpenCode`, `ClaudePrint.Complete` and `ClaudeTranscript`.
  Check: `go test -cover ./internal/harness | grep -qE 'coverage: ([7-9][0-9]|100)'`

- [x] **T23 Tests for `internal/inspect`.**
  Check: `go test -cover ./internal/inspect | grep -qE 'coverage: ([7-9][0-9]|100)'`

- [x] **T24 Raise coverage for `internal/session` and `internal/tokenize`.**
  Check: `go test -cover ./internal/session ./internal/tokenize | grep -cE 'coverage: (7[5-9]|[89][0-9]|100)' | grep -qx 2`

- [x] **T25 Fuzz the adapters.** Add `FuzzParse` to `internal/adapter/claude` and `internal/adapter/opencode`, seeded from `testdata/`; Parse must never panic.
  Check: `go test -run=^$ -fuzz=FuzzParse -fuzztime=20s ./internal/adapter/claude && go test -run=^$ -fuzz=FuzzParse -fuzztime=20s ./internal/adapter/opencode`

- [x] **T26 Benchmarks.** Add `BenchmarkParse` (both adapters) and `BenchmarkCount` (tokenize) on a generated 2,000-message session.
  Check: `go test -run=^$ -bench=. -benchtime=1x ./internal/... | grep -q Benchmark`

## Hygiene

- [x] **T27 Add `.golangci.yml`** enabling errcheck, staticcheck, revive, gosec and errorlint, and fix what it reports in live code (not parked code).
  Check: `test -f .golangci.yml && { ! command -v golangci-lint >/dev/null || golangci-lint run ./...; }`

- [x] **T28 Add Dependabot** in `.github/dependabot.yml` for gomod and github-actions, weekly.
  Check: `test -f .github/dependabot.yml`

- [x] **T29 Split `internal/cli/cli.go`** into one file per command (`inspect.go`, `categorize.go`, `common.go`), moving code without changing it.
  Check: `go test ./internal/cli && [ $(wc -l < internal/cli/cli.go) -lt 200 ]`

- [x] **T30 Tidy the repo root.** Add `show-me-*.html` to `.gitignore`. Do not delete the files.
  Check: `! git status --short | grep -q show-me`

- [ ] **T31 Add a CHANGELOG.md** in Keep a Changelog format, with a `0.1.0` entry summarising `ctxed overview`, `inspect` and `categorize`.
  Check: `test -f CHANGELOG.md`

- [ ] **T32 Improve the README for sharing.** Add a CI badge (after T08), a line `go install github.com/simranjeetc/ctxed/cmd/ctxed@latest`, and a placeholder for a demo GIF at `docs/demo.gif`.
  Check: `grep -q 'go install github.com' README.md`