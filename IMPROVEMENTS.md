# Improvements backlog

Run with `scripts/improve-loop.sh`. Each task passes through three hands:

| Role | Who | May change |
| --- | --- | --- |
| Implementer | `IMPROVE_MODEL` (default haiku) | source, tests, docs for its one task |
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

Tasks marked `(manual)` need the owner. Skip a task marked `(needs Txx)`
while Txx is unticked. Change a task or its Check only by editing and committing
this file yourself.

---

## Before sharing

- [ ] **T01 (manual) Create a public GitHub repo and push.**
  `git remote add origin <url> && git push -u origin main`.

- [ ] **T02 (manual) Choose a license.** Tell the agent MIT or Apache-2.0.

- [ ] **T03 (needs T02) Add the LICENSE file.** Put the chosen license text in `LICENSE`, with the copyright holder "Simranjeet Singh Chawla" and the year 2026.
  Check: `test -f LICENSE`

- [x] **T04 gofmt the tree.** Run `gofmt -w .`; change nothing else.
  Check: `test -z "$(gofmt -l .)"`.

- [x] **T05 Fix the staticcheck finding.** `internal/harness/claude.go:21`: the error string starts with a capital letter (ST1005). Make it lowercase.
  Check: `~/go/bin/staticcheck ./...` prints nothing.

- [ ] **T06 Fix the main.go doc comment.** `cmd/ctxed/main.go` says "inspects and edits"; ctxed is read-only now. Change it to "shows what an agent session's context is made of".
  Check: `grep -q "made of" cmd/ctxed/main.go`.

- [ ] **T07 Add a Makefile** with targets `build`, `test` (`go test -race ./...`), `lint` (gofmt check, `go vet`, staticcheck) and `install` (`go install ./cmd/ctxed`).
  Check: `make lint test`.

- [ ] **T08 Add CI.** Create `.github/workflows/ci.yml`. On push and pull_request it runs on ubuntu-latest with Go from `go.mod`, then a gofmt check, `go vet ./...`, staticcheck (`dominikh/staticcheck-action`), `go test -race ./...` and `govulncheck ./...`.
  Check: `test -f .github/workflows/ci.yml && grep -q "go test -race" .github/workflows/ci.yml`

- [ ] **T09 Set the version at build time.** In `internal/cli/cli.go`, change `const version = "0.1.0"` to `var version = "dev"`. If it is still "dev", read the module version from `debug.ReadBuildInfo()`. Document `-ldflags "-X github.com/simranjeetc/ctxed/internal/cli.version=…"` in a comment.
  Check: `go run ./cmd/ctxed version | grep -qE 'dev|devel'`

- [ ] **T10 Add GoReleaser.** Add `.goreleaser.yaml` (darwin and linux, amd64 and arm64, with ldflags setting the version from T09) and `.github/workflows/release.yml`, which runs it on `v*` tags.
  Check: `test -f .goreleaser.yaml && test -f .github/workflows/release.yml`

- [ ] **T11 Keep the parked code out of the binary.** Add `//go:build parked` to the parked command files and their tests in `internal/cli` (runDrop, runPrune, runCompactInstruction, the opencode command and `export_test.go`), splitting `cli.go` first if needed. Live commands must not import `prune`, `compact` or `ocprune`.
  Check: `! go list -deps ./cmd/ctxed | grep -qE 'internal/(prune|compact|ocprune)' && go test -tags parked ./...`

## Correctness

- [ ] **T12 Accept a single topic.** `categorize.Parse` rejects fewer than 2 categories ("at least 2 are required"). Allow 1. Update the prompt text and tests to match.
  Check: `go test ./internal/categorize`

- [ ] **T13 Cancel on Ctrl-C.** In `cmd/ctxed/main.go`, create a context with `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` and pass it down, replacing `context.Background()` in `internal/cli/overview.go`.
  Check: `grep -q NotifyContext cmd/ctxed/main.go`.

- [ ] **T14 Wrap errors with %w.** In `internal/harness/*.go` and `internal/model/model.go`, change `fmt.Errorf(... %v ..., err)` to `%w` wherever an error value is wrapped.
  Check: `! grep -rnE 'Errorf\(.*%v.*err\)' internal/harness internal/model`

- [ ] **T15 Handle the ReadAll error** in `internal/model/model.go` (`raw, _ := io.ReadAll(...)`).
  Check: `! grep -n 'raw, _ :=' internal/model/model.go`

- [ ] **T16 Bound the opencode cleanup.** In `internal/harness/opencode.go`, run `session delete` with a 10s context and print a failure to stderr instead of discarding it.
  Check: `! grep -rn '_ = exec.Command' internal/harness`

- [ ] **T17 Send the prompt to opencode on stdin, not as an argument.** In `OpenCodeRun.Complete`, pass the prompt on stdin if `opencode run` accepts it; otherwise write it to a temp file and attach it with `--file`. Verify the CLI's behaviour with `opencode run --help` first.
  Check: `go test ./internal/harness && ! grep -n '"--title", "ctxed categorize", prompt)' internal/harness/opencode.go`

- [ ] **T18 Add a `--no-model` flag** to `ctxed overview`. It skips the categorizer and prints only the "Whole session" sizes. Document it in the README and the usage text.
  Check: `CTXED_CATEGORIZER_CMD=false go run ./cmd/ctxed overview testdata/claude_session.jsonl --no-model 2>&1 | grep -q "Whole session"`

- [ ] **T19 Add a privacy note to the README.** It says that `overview` and `categorize` send excerpts of the session to the configured model, and that `--no-model` avoids it.
  Check: `grep -qi privacy README.md`.

## Tests

- [ ] **T20 Unit tests for `internal/overview`.** Write table-driven tests for `Build` (placement, nearest-entry fallback, the summary row, sorting) and `Short`.
  Check: `go test -cover ./internal/overview | grep -qE 'coverage: ([89][0-9]|100)'`

- [ ] **T21 Golden-file test for the overview table.** Save the rendered table as `internal/overview/testdata/table.golden`, compare against it, and regenerate it when `-update` is passed.
  Check: `test -f internal/overview/testdata/table.golden && go test ./internal/overview`

- [ ] **T22 Tests for `internal/harness`.** Put a fake `opencode` and a fake `claude` shell script on a temp PATH, then test `OpenCodeBin`, `ExportOpenCode`, `ClaudePrint.Complete` and `ClaudeTranscript`.
  Check: `go test -cover ./internal/harness | grep -qE 'coverage: ([7-9][0-9]|100)'`

- [ ] **T23 Tests for `internal/inspect`.**
  Check: `go test -cover ./internal/inspect | grep -qE 'coverage: ([7-9][0-9]|100)'`

- [ ] **T24 Raise coverage for `internal/session` and `internal/tokenize`.**
  Check: `go test -cover ./internal/session ./internal/tokenize | grep -cE 'coverage: (7[5-9]|[89][0-9]|100)' | grep -qx 2`

- [ ] **T25 Fuzz the adapters.** Add `FuzzParse` to `internal/adapter/claude` and `internal/adapter/opencode`, seeded from `testdata/`; Parse must never panic.
  Check: `go test -run=^$ -fuzz=FuzzParse -fuzztime=20s ./internal/adapter/claude && go test -run=^$ -fuzz=FuzzParse -fuzztime=20s ./internal/adapter/opencode`

- [ ] **T26 Benchmarks.** Add `BenchmarkParse` (both adapters) and `BenchmarkCount` (tokenize) on a generated 2,000-message session.
  Check: `go test -run=^$ -bench=. -benchtime=1x ./internal/... | grep -q Benchmark`

## Hygiene

- [ ] **T27 Add `.golangci.yml`** enabling errcheck, staticcheck, revive, gosec and errorlint, and fix what it reports in live code (not parked code).
  Check: `test -f .golangci.yml && { ! command -v golangci-lint >/dev/null || golangci-lint run ./...; }`

- [ ] **T28 Add Dependabot** in `.github/dependabot.yml` for gomod and github-actions, weekly.
  Check: `test -f .github/dependabot.yml`

- [ ] **T29 Split `internal/cli/cli.go`** into one file per command (`inspect.go`, `categorize.go`, `common.go`), moving code without changing it.
  Check: `go test ./internal/cli && [ $(wc -l < internal/cli/cli.go) -lt 200 ]`

- [ ] **T30 Tidy the repo root.** Add `show-me-*.html` to `.gitignore`. Do not delete the files.
  Check: `! git status --short | grep -q show-me`

- [ ] **T31 Add a CHANGELOG.md** in Keep a Changelog format, with a `0.1.0` entry summarising `ctxed overview`, `inspect` and `categorize`.
  Check: `test -f CHANGELOG.md`

- [ ] **T32 Improve the README for sharing.** Add a CI badge (after T08), a line `go install github.com/simranjeetc/ctxed/cmd/ctxed@latest`, and a placeholder for a demo GIF at `docs/demo.gif`.
  Check: `grep -q 'go install github.com' README.md`