// Package cli implements the ctxed command-line contract.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/simranjeetc/ctxed/internal/adapter"
	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/compact"
	"github.com/simranjeetc/ctxed/internal/inspect"
	"github.com/simranjeetc/ctxed/internal/model"
	"github.com/simranjeetc/ctxed/internal/prune"
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

const version = "0.1.0"

// Run dispatches a command and returns its exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return RunWithStdin(args, os.Stdin, stdout, stderr)
}

// RunWithStdin is Run with an explicit stdin, so a transcript can be piped in
// (`ctxed categorize - …`) and tests can supply one without touching the OS.
func RunWithStdin(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return ExitUsage
	}
	switch args[0] {
	case "inspect":
		return runInspect(args[1:], stdout, stderr)
	case "drop":
		return runDrop(args[1:], stdout, stderr)
	case "categorize":
		return runCategorize(args[1:], stdin, stdout, stderr)
	case "prune":
		return runPrune(args[1:], stdin, stdout, stderr)
	case "compact-instruction":
		return runCompactInstruction(args[1:], stdout, stderr)
	case "help", "--help", "-h":
		usage(stdout)
		return ExitOK
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "ctxed %s\n", version)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "ctxed: unknown command %q\n", args[0])
		usage(stderr)
		return ExitUsage
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `ctxed — inspect and edit an agent session's context window

Usage:
  ctxed inspect <session> [--json] [--model M] [--tokenizer ENC]
  ctxed drop    <session> --indices 3,7,9 [--out FILE] [--json] [--force]
                [--model M] [--tokenizer ENC]
  ctxed categorize <session> [--model M] [--base-url URL] [--api-key K]
                [--categorizer-cmd CMD] [--max-categories N] [--out FILE]
  ctxed prune   <session> (--categories-file F --categories 1,3 | --ids id1,id2)
                [--ids-only]
  ctxed compact-instruction <session> --categories-file F --categories 1,3

  <session> may be a file path, or "-" to read a transcript on stdin.
  On stdin, the categories file's session field is "-".
  For categorize, --out "-" prints the categories document to stdout.

Commands:
  inspect             print each entry: index, role, kind, tokens, first-line preview
  drop                write a copy of the session with the given entries removed
  categorize          group entries into high-level categories and write an editable file
  prune               emit the transcript with selected entries excluded (no write)
  compact-instruction print a Claude Code /compact instruction for the selected buckets

Flags:
  prune --ids-only   print {"droppedIds":[…]} (the resolved drop set) instead of a transcript

Exit codes: 0 ok, 1 error, 2 usage, 3 refused (invalid or unsafe selection)
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
		fmt.Fprintln(stderr, "ctxed inspect: expected exactly one session file")
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

func runDrop(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("drop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	indices := fs.String("indices", "", "comma-separated entry indices to remove")
	out := fs.String("out", "", "output path (default: <name>.edited.<ext>)")
	asJSON := fs.Bool("json", false, "emit JSON stats")
	force := fs.Bool("force", false, "write even if the edit orphans tool results")
	model := fs.String("model", "", "model name, for tokenizer selection")
	encoding := fs.String("tokenizer", "", "tokenizer encoding name")
	flags, rest := splitArgs(args, valueFlagsDrop)
	if err := fs.Parse(flags); err != nil {
		return parseErrExit(err, stderr)
	}
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "ctxed drop: expected exactly one session file")
		return ExitUsage
	}
	if strings.TrimSpace(*indices) == "" {
		fmt.Fprintln(stderr, "ctxed drop: --indices is required")
		return ExitUsage
	}

	in := rest[0]
	data, err := os.ReadFile(in)
	if err != nil {
		return fail(stderr, err)
	}
	doc, code := load(data, stderr)
	if code != ExitOK {
		return code
	}

	ids, err := prune.ParseIndices(*indices)
	if err != nil {
		fmt.Fprintf(stderr, "ctxed drop: %v\n", err)
		return ExitRefused
	}
	if err := prune.Validate(doc, ids); err != nil {
		fmt.Fprintf(stderr, "ctxed drop: %v\n", err)
		return ExitRefused
	}
	if orphans := prune.Orphans(doc, ids); len(orphans) > 0 && !*force {
		fmt.Fprintln(stderr, "ctxed drop: refusing to write a structurally invalid session:")
		for _, o := range orphans {
			fmt.Fprintln(stderr, "  - "+o)
		}
		fmt.Fprintln(stderr, "re-run with --force to write anyway")
		return ExitRefused
	}

	tok, err := tokenize.Resolve(*model, *encoding)
	if err != nil {
		return fail(stderr, err)
	}
	outPath := *out
	if outPath == "" {
		outPath = defaultOut(in)
	}
	stats := computeStats(in, outPath, doc, ids, tok)

	doc.Drop(ids)
	edited, err := write(doc)
	if err != nil {
		return fail(stderr, err)
	}
	if err := os.WriteFile(outPath, edited, 0o644); err != nil {
		return fail(stderr, err)
	}

	if *asJSON {
		b, err := json.MarshalIndent(stats, "", "  ")
		if err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "%s\n", b)
		return ExitOK
	}
	fmt.Fprintf(stdout, "wrote %s\n", outPath)
	fmt.Fprintf(stdout, "entries %d → %d · tokens %s → %s (%s)\n",
		stats.EntriesBefore, stats.EntriesAfter,
		human(stats.TokensBefore), human(stats.TokensAfter),
		signed(stats.TokensAfter-stats.TokensBefore))
	return ExitOK
}

var (
	valueFlagsInspect    = map[string]bool{"model": true, "tokenizer": true}
	valueFlagsDrop       = map[string]bool{"indices": true, "out": true, "model": true, "tokenizer": true}
	valueFlagsCategorize = map[string]bool{"model": true, "tokenizer": true, "base-url": true, "api-key": true, "categorizer-cmd": true, "max-categories": true, "out": true, "max-input-bytes": true}
	valueFlagsPrune      = map[string]bool{"categories-file": true, "categories": true, "ids": true}
	valueFlagsCompact    = map[string]bool{"categories-file": true, "categories": true}
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
	maxInput := fs.Int("max-input-bytes", 400000, "maximum prompt size in bytes")

	flags, rest := splitArgs(args, valueFlagsCategorize)
	if err := fs.Parse(flags); err != nil {
		return parseErrExit(err, stderr)
	}
	if len(rest) > 1 {
		fmt.Fprintln(stderr, "ctxed categorize: expected at most one session file")
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

	prompt := categorize.Prompt(doc, *maxCats)
	if len(prompt) > *maxInput {
		fmt.Fprintf(stderr, "ctxed categorize: prompt is %d bytes; the limit is %d\n", len(prompt), *maxInput)
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
		fmt.Fprintf(stdout, "%s", data)
		return ExitOK
	}
	if err := categorize.WriteFile(outPath, f); err != nil {
		return fail(stderr, err)
	}
	if err := categorize.Render(stdout, f); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "wrote %s\n", outPath)
	return ExitOK
}

func runPrune(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	fs.SetOutput(stderr)
	catsFile := fs.String("categories-file", "", "categories file")
	cats := fs.String("categories", "", "comma-separated category ids to drop")
	ids := fs.String("ids", "", "comma-separated stable entry ids to drop")
	idsOnly := fs.Bool("ids-only", false, "emit the resolved dropped entry ids as JSON instead of a transcript")

	flags, rest := splitArgs(args, valueFlagsPrune)
	if err := fs.Parse(flags); err != nil {
		return parseErrExit(err, stderr)
	}
	idsSet := false
	for _, f := range flags {
		if name := strings.TrimLeft(f, "-"); name == "ids" || strings.HasPrefix(name, "ids=") {
			idsSet = true
		}
	}
	if len(rest) > 1 {
		fmt.Fprintln(stderr, "ctxed prune: expected at most one session file")
		return ExitUsage
	}
	in := ""
	if len(rest) == 1 {
		in = rest[0]
	}
	data, _, err := readSession(in, stdin)
	if err != nil {
		return fail(stderr, err)
	}
	doc, code := load(data, stderr)
	if code != ExitOK {
		return code
	}

	var pruneIDs []string
	switch {
	case *catsFile != "":
		raw, err := os.ReadFile(*catsFile)
		if err != nil {
			return fail(stderr, err)
		}
		f, err := categorize.Load(raw, doc)
		if err != nil {
			fmt.Fprintf(stderr, "ctxed prune: %v\n", err)
			return ExitRefused
		}
		if strings.TrimSpace(*cats) == "" {
			fmt.Fprintln(stderr, "ctxed prune: --categories is required with --categories-file")
			return ExitUsage
		}
		sel, err := prune.ParseIndices(*cats)
		if err != nil {
			fmt.Fprintf(stderr, "ctxed prune: %v\n", err)
			return ExitUsage
		}
		pruneIDs, err = categorize.Select(f, sel)
		if err != nil {
			fmt.Fprintf(stderr, "ctxed prune: %v\n", err)
			return ExitRefused
		}
	case *ids != "" || idsSet:
		pruneIDs = splitList(*ids)
	default:
		fmt.Fprintln(stderr, "ctxed prune: provide --categories-file with --categories, or --ids")
		return ExitUsage
	}

	indices, missing := prune.IndicesForIDs(doc, pruneIDs)
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "ctxed prune: unknown entry id(s): %s\n", strings.Join(missing, ", "))
		return ExitRefused
	}
	final, adjustments := prune.ResolveOrphans(doc, indices)
	for _, a := range adjustments {
		fmt.Fprintln(stderr, "adjustment: "+a)
	}
	if *idsOnly {
		return writeDroppedIDs(stdout, stderr, doc, final)
	}
	doc.Drop(final)
	edited, err := write(doc)
	if err != nil {
		return fail(stderr, err)
	}
	if _, err := stdout.Write(edited); err != nil {
		return fail(stderr, err)
	}
	return ExitOK
}

// runCompactInstruction renders the text to paste after `/compact ` in a live
// Claude Code session. It reads the session only to validate the categories
// file; it writes nothing and drops nothing.
func runCompactInstruction(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compact-instruction", flag.ContinueOnError)
	fs.SetOutput(stderr)
	catsFile := fs.String("categories-file", "", "categories file")
	cats := fs.String("categories", "", "comma-separated category ids to drop")

	flags, rest := splitArgs(args, valueFlagsCompact)
	if err := fs.Parse(flags); err != nil {
		return parseErrExit(err, stderr)
	}
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "ctxed compact-instruction: expected exactly one session file")
		return ExitUsage
	}
	if strings.TrimSpace(*catsFile) == "" {
		fmt.Fprintln(stderr, "ctxed compact-instruction: --categories-file is required")
		return ExitUsage
	}
	if strings.TrimSpace(*cats) == "" {
		fmt.Fprintln(stderr, "ctxed compact-instruction: --categories is required")
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
	raw, err := os.ReadFile(*catsFile)
	if err != nil {
		return fail(stderr, err)
	}
	f, err := categorize.Load(raw, doc)
	if err != nil {
		fmt.Fprintf(stderr, "ctxed compact-instruction: %v\n", err)
		return ExitRefused
	}
	sel, err := prune.ParseIndices(*cats)
	if err != nil {
		fmt.Fprintf(stderr, "ctxed compact-instruction: %v\n", err)
		return ExitUsage
	}
	instruction, err := compact.Instruction(f, sel)
	if err != nil {
		fmt.Fprintf(stderr, "ctxed compact-instruction: %v\n", err)
		return ExitRefused
	}
	fmt.Fprintln(stdout, instruction)
	return ExitOK
}

// writeDroppedIDs prints the resolved drop set as a JSON object on stdout, so a
// dispatch plugin can filter live messages by id. ids are emitted in session
// order, which makes the output deterministic.
//
// It also reports the tool-call ids that the dropped entries issued or answered
// (`droppedToolCallIds`). A plugin needs these because a tool result can live in
// a live message that carries no id of its own; the plugin can then drop such a
// message by matching the call id inside it. The set is deduplicated and sorted,
// so the output stays deterministic.
func writeDroppedIDs(stdout, stderr io.Writer, doc *session.Document, indices []int) int {
	drop := make(map[int]bool, len(indices))
	for _, i := range indices {
		drop[i] = true
	}
	dropped := make([]string, 0, len(indices))
	callSet := map[string]bool{}
	for _, e := range doc.Entries {
		if !drop[e.Index] {
			continue
		}
		dropped = append(dropped, e.ID)
		for _, id := range e.CallIDs {
			callSet[id] = true
		}
		for _, id := range e.ResultIDs {
			callSet[id] = true
		}
	}
	callIDs := make([]string, 0, len(callSet))
	for id := range callSet {
		callIDs = append(callIDs, id)
	}
	sort.Strings(callIDs)
	b, err := json.Marshal(struct {
		DroppedIDs         []string `json:"droppedIds"`
		DroppedToolCallIDs []string `json:"droppedToolCallIds"`
	}{DroppedIDs: dropped, DroppedToolCallIDs: callIDs})
	if err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "%s\n", b)
	return ExitOK
}

func envOr(v, key string) string {
	if v != "" {
		return v
	}
	return os.Getenv(key)
}

func splitList(s string) []string {
	fields := strings.Split(s, ",")
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
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

// write dispatches to the document's source adapter.
func write(doc *session.Document) ([]byte, error) {
	for _, a := range adapter.All() {
		if a.Name() == doc.Source {
			return a.Write(doc)
		}
	}
	return nil, fmt.Errorf("no adapter for source %q", doc.Source)
}

type stats struct {
	Source         string                `json:"source"`
	Input          string                `json:"input"`
	Output         string                `json:"output"`
	EntriesBefore  int                   `json:"entriesBefore"`
	EntriesAfter   int                   `json:"entriesAfter"`
	TokensBefore   int                   `json:"tokensBefore"`
	TokensAfter    int                   `json:"tokensAfter"`
	RemovedIndices []int                 `json:"removedIndices"`
	Tokenizer      inspect.TokenizerInfo `json:"tokenizer"`
}

func computeStats(in, out string, doc *session.Document, ids []int, tok tokenize.Tokenizer) stats {
	dropped := make(map[int]bool, len(ids))
	for _, i := range ids {
		dropped[i] = true
	}
	s := stats{
		Source:         doc.Source,
		Input:          in,
		Output:         out,
		EntriesBefore:  len(doc.Entries),
		RemovedIndices: ids,
		Tokenizer:      inspect.TokenizerInfo{Name: tok.Name(), Approximate: tok.Approximate()},
	}
	for _, e := range doc.Entries {
		n := tok.Count(e.Text)
		s.TokensBefore += n
		if dropped[e.Index] {
			continue
		}
		s.EntriesAfter++
		s.TokensAfter += n
	}
	return s
}

func defaultOut(in string) string {
	ext := filepath.Ext(in)
	base := strings.TrimSuffix(in, ext)
	return base + ".edited" + ext
}

func parseErrExit(err error, stderr io.Writer) int {
	if err == flag.ErrHelp {
		return ExitOK
	}
	return ExitUsage
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "ctxed: %v\n", err)
	return ExitError
}

func human(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func signed(n int) string {
	if n > 0 {
		return "+" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
