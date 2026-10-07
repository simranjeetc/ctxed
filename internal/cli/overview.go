package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/harness"
	"github.com/simranjeetc/ctxed/internal/model"
	"github.com/simranjeetc/ctxed/internal/overview"
	"github.com/simranjeetc/ctxed/internal/session"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

var valueFlagsOverview = map[string]bool{"session": true, "categorizer-cmd": true, "max-categories": true, "max-input-bytes": true, "model": true, "tokenizer": true}

// runOverview reports what the live context of a session is made of. It reads
// the session and asks a model to name the topics; it changes nothing.
func runOverview(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("overview", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sessionFlag := fs.String("session", "", "Claude Code session UUID or OpenCode ses_ id (default: from the environment)")
	cmd := fs.String("categorizer-cmd", "", "shell command: prompt on stdin, model answer on stdout")
	maxCats := fs.Int("max-categories", categorize.DefaultMaxCategories, "maximum number of topics")
	maxInput := fs.Int("max-input-bytes", 30000, "target prompt size in bytes")
	asJSON := fs.Bool("json", false, "emit JSON")
	noModel := fs.Bool("no-model", false, "skip the categorizer; print only the whole-session sizes")
	modelName := fs.String("model", "", "model name, for tokenizer selection")
	encoding := fs.String("tokenizer", "", "tokenizer encoding name")
	flags, rest := splitArgs(args, valueFlagsOverview)
	if err := fs.Parse(flags); err != nil {
		return parseErrExit(err, stderr)
	}
	if len(rest) > 1 {
		_, _ = fmt.Fprintln(stderr, "ctxed overview: expected at most one session file")
		return ExitUsage
	}
	file := ""
	if len(rest) == 1 {
		file = rest[0]
	}

	src, code := findSession(ctx, file, *sessionFlag, stderr)
	if code != ExitOK {
		return code
	}
	data, err := src.read()
	if err != nil {
		return fail(stderr, err)
	}
	doc, code := load(data, stderr)
	if code != ExitOK {
		return code
	}
	tok, err := tokenize.Resolve(*modelName, *encoding)
	if err != nil {
		return fail(stderr, err)
	}

	var f categorize.File
	if !*noModel {
		topics := overview.Topics(doc)
		if len(topics.Entries) >= 2 {
			f, err = categorizeTopics(ctx, topics, doc.Source, *cmd, *maxCats, *maxInput, tok)
			if err != nil {
				// The sizes are still worth showing; only the topic names are missing.
				_, _ = fmt.Fprintf(stderr, "ctxed overview: topics unavailable: %v\n", err)
				f = categorize.File{}
			}
		}
	}
	r := overview.Build(doc, f, tok, src.name)
	if err := overview.Render(stdout, r, *asJSON); err != nil {
		return fail(stderr, err)
	}
	return ExitOK
}

// sessionSource is where a session's bytes come from.
type sessionSource struct {
	name string
	read func() ([]byte, error)
}

// findSession resolves the session: a file argument, else --session, else the
// id the running harness puts in the environment of every shell command.
func findSession(ctx context.Context, file, id string, stderr io.Writer) (sessionSource, int) {
	if file != "" {
		return sessionSource{name: file, read: func() ([]byte, error) { return os.ReadFile(file) }}, ExitOK
	}
	if id == "" {
		cc, oc := os.Getenv("CLAUDE_CODE_SESSION_ID"), os.Getenv("OPENCODE_SESSION_ID")
		switch {
		case cc != "" && oc != "":
			_, _ = fmt.Fprintln(stderr, "ctxed overview: both CLAUDE_CODE_SESSION_ID and OPENCODE_SESSION_ID are set; pass --session")
			return sessionSource{}, ExitUsage
		case cc != "":
			id = cc
		case oc != "":
			id = oc
		default:
			_, _ = fmt.Fprintln(stderr, "ctxed overview: no session; pass a session file, or --session ID, or run it from inside Claude Code or OpenCode")
			return sessionSource{}, ExitUsage
		}
	}
	if strings.HasPrefix(id, "ses_") {
		if err := harness.ValidOpenCodeSession(id); err != nil {
			_, _ = fmt.Fprintf(stderr, "ctxed overview: %v\n", err)
			return sessionSource{}, ExitUsage
		}
		return sessionSource{name: id, read: func() ([]byte, error) {
			ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			return harness.ExportOpenCode(ctx, id)
		}}, ExitOK
	}
	if err := harness.ValidClaudeSession(id); err != nil {
		_, _ = fmt.Fprintf(stderr, "ctxed overview: %v\n", err)
		return sessionSource{}, ExitUsage
	}
	cwd, _ := os.Getwd()
	path, err := harness.ClaudeTranscript(id, cwd)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "ctxed overview: %v\n", err)
		return sessionSource{}, ExitError
	}
	return sessionSource{name: id, read: func() ([]byte, error) { return os.ReadFile(path) }}, ExitOK
}

// categorizeTopics asks a model to group the entries into topics. The model is
// --categorizer-cmd, else $CTXED_CATEGORIZER_CMD, else the harness's own CLI
// (`claude -p` or `opencode run`) with $CTXED_CATEGORIZER_MODEL.
func categorizeTopics(ctx context.Context, doc *session.Document, source, cmd string, maxCats, maxInput int, tok tokenize.Tokenizer) (categorize.File, error) {
	var client model.Client
	if c := envOr(cmd, "CTXED_CATEGORIZER_CMD"); strings.TrimSpace(c) != "" {
		var err error
		if client, err = model.Resolve(model.Config{Command: c}); err != nil {
			return categorize.File{}, err
		}
	} else if source == "opencode" {
		client = harness.OpenCodeRun{Model: os.Getenv("CTXED_CATEGORIZER_MODEL")}
	} else {
		client = harness.ClaudePrint{Model: os.Getenv("CTXED_CATEGORIZER_MODEL")}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	text, err := client.Complete(ctx, categorize.Prompt(doc, maxCats, maxInput))
	if err != nil {
		return categorize.File{}, err
	}
	return categorize.Parse(text, doc, tok, maxCats)
}
