package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/model"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

var valueFlagsCategorize = map[string]bool{"model": true, "tokenizer": true, "base-url": true, "api-key": true, "categorizer-cmd": true, "max-categories": true, "out": true, "max-input-bytes": true}

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
