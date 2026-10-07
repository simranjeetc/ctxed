// Package cli implements the ctxed command-line contract.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/simranjeetc/ctxed/internal/adapter"
	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/inspect"
	"github.com/simranjeetc/ctxed/internal/model"
	"github.com/simranjeetc/ctxed/internal/session"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

// Exit codes. They are stable: a harness may branch on them.
const (
	ExitOK      = 0 // success
	ExitError   = 1 // runtime error
	ExitUsage   = 2 // usage error
	ExitRefused = 3 // refused: invalid or unsafe edit
)

// version is set at build time with: go install -ldflags "-X github.com/simranjeetc/ctxed/internal/cli.version=X.Y.Z"
// If version is still "dev" at runtime, it is read from the module version in debug.ReadBuildInfo().
var version = "dev"

func init() {
	if version == "dev" {
		bi, ok := debug.ReadBuildInfo()
		if ok && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
	}
}

// Run dispatches a command and returns its exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return RunContext(context.Background(), args, os.Stdin, stdout, stderr)
}

// RunWithStdin is Run with an explicit stdin, so a transcript can be piped in
// (`ctxed categorize - …`) and tests can supply one without touching the OS.
func RunWithStdin(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return RunContext(context.Background(), args, stdin, stdout, stderr)
}

// RunContext is RunWithStdin with a caller-supplied context, so the session
// read and the model call are cancelled when the user interrupts (Ctrl-C or
// SIGTERM).
func RunContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return ExitUsage
	}
	switch args[0] {
	// Pruning (drop, prune, compact-instruction, opencode) is parked: its code
	// stays in this package but no command reaches it. See
	// openspec/changes/context-overview.
	case "overview":
		return runOverview(ctx, args[1:], stdout, stderr)
	case "inspect":
		return runInspect(args[1:], stdout, stderr)
	case "categorize":
		return runCategorize(args[1:], stdin, stdout, stderr)
	case "help", "--help", "-h":
		usage(stdout)
		return ExitOK
	case "version", "--version", "-v":
		_, _ = fmt.Fprintf(stdout, "ctxed %s\n", version)
		return ExitOK
	default:
		_, _ = fmt.Fprintf(stderr, "ctxed: unknown command %q\n", args[0])
		usage(stderr)
		return ExitUsage
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `ctxed — show what an agent session's context is made of

Usage:
  ctxed overview [<session>] [--session ID] [--json] [--categorizer-cmd CMD]
                [--max-categories N] [--no-model]
  ctxed inspect <session> [--json] [--model M] [--tokenizer ENC]
  ctxed categorize <session> [--model M] [--base-url URL] [--api-key K]
                [--categorizer-cmd CMD] [--max-categories N] [--out FILE]

  <session> is a Claude Code transcript (.jsonl) or an OpenCode export (.json);
  for categorize it may be "-" to read stdin. overview with no <session> uses
  --session, else the session it runs in (CLAUDE_CODE_SESSION_ID or
  OPENCODE_SESSION_ID).

Commands:
  overview    topics in the live context, each with messages, tokens, share and
              status, plus what is still pending (read-only)
  inspect     print each live entry: index, role, kind, tokens, first-line preview
  categorize  group entries into high-level categories and write an editable file

Token counts are estimates unless --model/--tokenizer selects a real tokenizer.
Topic status and pending items are a model's reading of the session.

Exit codes: 0 ok, 1 error, 2 usage
`)
}

func runInspect(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit JSON")
	model := fs.String("model", "", "model name, for tokenizer selection")
	encoding := fs.String("tokenizer", "", "tokenizer encoding name")
	flags, rest := splitArgs(args, valueFlagsInspect)
	if err := fs.Parse(flags); err != nil {
		return parseErrExit(err, stderr)
	}
	if len(rest) != 1 {
		_, _ = fmt.Fprintln(stderr, "ctxed inspect: expected exactly one session file")
		return ExitUsage
	}
	data, err := os.ReadFile(rest[0])
	if err != nil {
		return fail(stderr, err)
	}
	doc, code := load(data, stderr)
	if code != ExitOK {
		return code
	}
	tok, err := tokenize.Resolve(*model, *encoding)
	if err != nil {
		return fail(stderr, err)
	}
	if err := inspect.Render(stdout, doc, tok, *asJSON); err != nil {
		return fail(stderr, err)
	}
	return ExitOK
}

var (
	valueFlagsInspect    = map[string]bool{"model": true, "tokenizer": true}
	valueFlagsCategorize = map[string]bool{"model": true, "tokenizer": true, "base-url": true, "api-key": true, "categorizer-cmd": true, "max-categories": true, "out": true, "max-input-bytes": true}
)

// splitArgs separates flags from positional arguments so a session file may
// appear before, after, or between flags (Go's flag package alone would stop
// parsing at the first positional).
func splitArgs(args []string, valueFlags map[string]bool) (flags, pos []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if strings.ContainsRune(name, '=') {
				continue
			}
			if valueFlags[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		pos = append(pos, a)
	}
	return flags, pos
}

// readSession returns the session bytes from a path, or from stdin when the
// path is "-" or empty. The returned name is used for default output paths and
// the categories file's session field.
func readSession(in string, stdin io.Reader) (data []byte, name string, err error) {
	if in == "" || in == "-" {
		data, err = io.ReadAll(stdin)
		return data, "-", err
	}
	data, err = os.ReadFile(in)
	return data, in, err
}

func runCategorize(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("categorize", flag.ContinueOnError)
	fs.SetOutput(stderr)
	modelName := fs.String("model", "", "model name for the call and its tokenizer")
	encoding := fs.String("tokenizer", "", "tokenizer encoding override")
	baseURL := fs.String("base-url", "", "OpenAI-compatible base URL")
	apiKey := fs.String("api-key", "", "API key")
	cmd := fs.String("categorizer-cmd", "", "shell command returning the model response")
	maxCats := fs.Int("max-categories", categorize.DefaultMaxCategories, "maximum number of categories")
	out := fs.String("out", "", "categories file path")
	maxInput := fs.Int("max-input-bytes", 30000, "target prompt size in bytes; the prompt is sampled and shortened to fit")

	flags, rest := splitArgs(args, valueFlagsCategorize)
	if err := fs.Parse(flags); err != nil {
		return parseErrExit(err, stderr)
	}
	if len(rest) > 1 {
		_, _ = fmt.Fprintln(stderr, "ctxed categorize: expected at most one session file")
		return ExitUsage
	}
	in := ""
	if len(rest) == 1 {
		in = rest[0]
	}
	data, name, err := readSession(in, stdin)
	if err != nil {
		return fail(stderr, err)
	}
	doc, code := load(data, stderr)
	if code != ExitOK {
		return code
	}

	effModel := envOr(*modelName, "CTXED_MODEL")
	tok, err := tokenize.Resolve(effModel, *encoding)
	if err != nil {
		return fail(stderr, err)
	}
	client, err := model.Resolve(model.Config{
		BaseURL: envOr(*baseURL, "OPENAI_BASE_URL"),
		APIKey:  envOr(*apiKey, "OPENAI_API_KEY"),
		Model:   effModel,
		Command: *cmd,
	})
	if err != nil {
		return fail(stderr, err)
	}

	prompt := categorize.Prompt(doc, *maxCats, *maxInput)
	if len(prompt) > *maxInput {
		// Prompt is bounded by construction; this is a safety net only.
		_, _ = fmt.Fprintf(stderr, "ctxed categorize: prompt is %d bytes; the limit is %d\n", len(prompt), *maxInput)
		return ExitError
	}
	text, err := client.Complete(context.Background(), prompt)
	if err != nil {
		return fail(stderr, err)
	}
	f, err := categorize.Parse(text, doc, tok, *maxCats)
	if err != nil {
		return fail(stderr, err)
	}
	f.Session = name

	outPath := *out
	if outPath == "" {
		outPath = categoriesOut(name)
	}
	// `--out -` prints the categories file to stdout instead of writing it, so a
	// plugin can categorize a live transcript without a temp file (the stdin
	// counterpart to `categorize -`/`prune -`).
	if outPath == "-" {
		data, err := categorize.Marshal(f)
		if err != nil {
			return fail(stderr, err)
		}
		_, _ = fmt.Fprintf(stdout, "%s", data)
		return ExitOK
	}
	if err := categorize.WriteFile(outPath, f); err != nil {
		return fail(stderr, err)
	}
	if err := categorize.Render(stdout, f); err != nil {
		return fail(stderr, err)
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s\n", outPath)
	return ExitOK
}

func envOr(v, key string) string {
	if v != "" {
		return v
	}
	return os.Getenv(key)
}

func categoriesOut(in string) string {
	ext := filepath.Ext(in)
	return strings.TrimSuffix(in, ext) + ".categories.json"
}

// load reads, detects, and parses a document.
func load(data []byte, stderr io.Writer) (*session.Document, int) {
	a, err := adapter.Detect(data)
	if err != nil {
		return nil, fail(stderr, err)
	}
	doc, err := a.Parse(data)
	if err != nil {
		return nil, fail(stderr, err)
	}
	return doc, ExitOK
}

func parseErrExit(err error, _ io.Writer) int {
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	return ExitUsage
}

func fail(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintf(stderr, "ctxed: %v\n", err)
	return ExitError
}
