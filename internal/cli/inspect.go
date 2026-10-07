package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/simranjeetc/ctxed/internal/inspect"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

var valueFlagsInspect = map[string]bool{"model": true, "tokenizer": true}

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
