package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/harness"
	"github.com/simranjeetc/ctxed/internal/model"
	"github.com/simranjeetc/ctxed/internal/ocprune"
	"github.com/simranjeetc/ctxed/internal/prune"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

var (
	valueFlagsOCCategorize = map[string]bool{"session": true, "export": true, "categorizer-cmd": true, "max-categories": true, "max-input-bytes": true}
	valueFlagsOCDrop       = map[string]bool{"session": true}
)

// runOpenCode is the live OpenCode prune: `categorize` sorts what the model
// still sees and saves a numbered listing; `drop` adds listed categories to the
// session's drop list, which the OpenCode plugin applies to every request.
func runOpenCode(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "ctxed opencode: expected categorize or drop")
		return ExitUsage
	}
	switch args[0] {
	case "categorize":
		return runOCCategorize(args[1:], stdout, stderr)
	case "drop":
		return runOCDrop(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ctxed opencode: unknown command %q; expected categorize or drop\n", args[0])
		return ExitUsage
	}
}

// ocSession resolves the session id: --session, else $OPENCODE_SESSION_ID,
// which OpenCode sets for every shell command it runs.
func ocSession(flagValue string, stderr io.Writer, cmd string) (string, int) {
	id := envOr(flagValue, "OPENCODE_SESSION_ID")
	if id == "" {
		fmt.Fprintf(stderr, "ctxed opencode %s: no session; pass --session or run it from inside OpenCode\n", cmd)
		return "", ExitUsage
	}
	if err := ocprune.ValidSession(id); err != nil {
		fmt.Fprintf(stderr, "ctxed opencode %s: %v\n", cmd, err)
		return "", ExitUsage
	}
	return id, ExitOK
}

func runOCCategorize(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("opencode categorize", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sessionFlag := fs.String("session", "", "OpenCode session id (default $OPENCODE_SESSION_ID)")
	exportFile := fs.String("export", "", "read this export instead of running `opencode session export`")
	cmd := fs.String("categorizer-cmd", "", "shell command returning the model response (default $CTXED_CATEGORIZER_CMD, else opencode run)")
	maxCats := fs.Int("max-categories", categorize.DefaultMaxCategories, "maximum number of categories")
	maxInput := fs.Int("max-input-bytes", 30000, "target prompt size in bytes")
	asJSON := fs.Bool("json", false, "print the listing as JSON")

	flags, rest := splitArgs(args, valueFlagsOCCategorize)
	if err := fs.Parse(flags); err != nil {
		return parseErrExit(err, stderr)
	}
	if len(rest) > 0 {
		fmt.Fprintln(stderr, "ctxed opencode categorize: takes no positional arguments")
		return ExitUsage
	}
	sid, code := ocSession(*sessionFlag, stderr, "categorize")
	if code != ExitOK {
		return code
	}
	dir, err := ocprune.Dir()
	if err != nil {
		return fail(stderr, err)
	}
	st, err := ocprune.Load(dir, sid)
	if err != nil {
		return fail(stderr, err)
	}

	var data []byte
	if *exportFile != "" {
		data, err = os.ReadFile(*exportFile)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		data, err = harness.ExportOpenCode(ctx, sid)
		cancel()
	}
	if err != nil {
		return fail(stderr, err)
	}
	doc, code := load(data, stderr)
	if code != ExitOK {
		return code
	}
	if doc.Source != "opencode" {
		fmt.Fprintf(stderr, "ctxed opencode categorize: not an OpenCode export (detected %s)\n", doc.Source)
		return ExitError
	}
	left := ocprune.LiveView(doc, st.Dropped)

	if len(doc.Entries) < 2 {
		st.Listing = nil
		if err := ocprune.Save(dir, st); err != nil {
			return fail(stderr, err)
		}
		if *asJSON {
			return printJSON(stdout, stderr, st.Listing)
		}
		fmt.Fprintln(stdout, "Nothing to prune: fewer than two messages are still sent to the model.")
		return ExitOK
	}

	client, err := ocCategorizer(*cmd)
	if err != nil {
		return fail(stderr, err)
	}
	prompt := categorize.Prompt(doc, *maxCats, *maxInput)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	text, err := client.Complete(ctx, prompt)
	cancel()
	if err != nil {
		return fail(stderr, err)
	}
	tok, _ := tokenize.Resolve("", "")
	f, err := categorize.Parse(text, doc, tok, *maxCats)
	if err != nil {
		return fail(stderr, err)
	}
	st.Listing = ocprune.NewListing(f, doc)
	if err := ocprune.Save(dir, st); err != nil {
		return fail(stderr, err)
	}

	if *asJSON {
		return printJSON(stdout, stderr, st.Listing)
	}
	fmt.Fprintf(stdout, "Topics in what the model still sees (session %s):\n", sid)
	for _, c := range st.Listing {
		fmt.Fprintf(stdout, "  %d. %s — %d messages, ~%s tokens\n", c.ID, c.Label, len(c.EntryIDs), human(c.Tokens))
	}
	if len(st.Dropped.IDs) > 0 {
		fmt.Fprintf(stdout, "Already dropped and not listed: %d messages.\n", len(st.Dropped.IDs))
	} else if left > 0 {
		fmt.Fprintf(stdout, "Not listed: %d messages from before the last compaction.\n", left)
	}
	fmt.Fprintln(stdout, "Drop with: ctxed opencode drop <numbers>   (for example 1,3)")
	return ExitOK
}

// ocCategorizer picks the categorizer: --categorizer-cmd, $CTXED_CATEGORIZER_CMD,
// else `opencode run` with $CTXED_CATEGORIZER_MODEL.
func ocCategorizer(cmd string) (model.Client, error) {
	if c := envOr(cmd, "CTXED_CATEGORIZER_CMD"); strings.TrimSpace(c) != "" {
		return model.Resolve(model.Config{Command: c})
	}
	return harness.OpenCodeRun{Model: os.Getenv("CTXED_CATEGORIZER_MODEL")}, nil
}

func runOCDrop(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("opencode drop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sessionFlag := fs.String("session", "", "OpenCode session id (default $OPENCODE_SESSION_ID)")

	flags, rest := splitArgs(args, valueFlagsOCDrop)
	if err := fs.Parse(flags); err != nil {
		return parseErrExit(err, stderr)
	}
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "ctxed opencode drop: expected the category numbers, for example: ctxed opencode drop 1,3")
		return ExitUsage
	}
	numbers, err := prune.ParseIndices(rest[0])
	if err != nil || len(numbers) == 0 {
		fmt.Fprintf(stderr, "ctxed opencode drop: %q is not a list of category numbers\n", rest[0])
		return ExitUsage
	}
	sid, code := ocSession(*sessionFlag, stderr, "drop")
	if code != ExitOK {
		return code
	}
	dir, err := ocprune.Dir()
	if err != nil {
		return fail(stderr, err)
	}
	st, err := ocprune.Load(dir, sid)
	if err != nil {
		return fail(stderr, err)
	}
	before := len(st.Dropped.IDs)
	chosen, err := st.Drop(numbers)
	if err != nil {
		fmt.Fprintf(stderr, "ctxed opencode drop: %v\n", err)
		return ExitRefused
	}
	if err := ocprune.Save(dir, st); err != nil {
		return fail(stderr, err)
	}
	labels := make([]string, 0, len(chosen))
	for _, c := range chosen {
		labels = append(labels, fmt.Sprintf("%q", c.Label))
	}
	fmt.Fprintf(stdout, "Dropped %s (%d messages). The model stops seeing them from the next request.\n",
		strings.Join(labels, ", "), len(st.Dropped.IDs)-before)
	fmt.Fprintf(stdout, "Drop list for this session: %d messages.\n", len(st.Dropped.IDs))
	return ExitOK
}

func printJSON(stdout, stderr io.Writer, v any) int {
	if v == nil {
		v = []ocprune.Category{}
	}
	if l, ok := v.([]ocprune.Category); ok && l == nil {
		v = []ocprune.Category{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fail(stderr, errors.New("cannot encode the listing"))
	}
	fmt.Fprintf(stdout, "%s\n", b)
	return ExitOK
}
