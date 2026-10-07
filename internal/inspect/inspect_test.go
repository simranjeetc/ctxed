package inspect_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/inspect"
	"github.com/simranjeetc/ctxed/internal/session"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

// wordTok counts whitespace-separated words and reports exact (non-approximate)
// counts, so the tests can exercise the "exact" rendering path deterministically.
type wordTok struct{}

func (wordTok) Name() string      { return "words" }
func (wordTok) Approximate() bool { return false }
func (wordTok) Count(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Fields(s))
}

func TestBuild(t *testing.T) {
	doc := &session.Document{
		Source: "opencode",
		Entries: []*session.Entry{
			{Index: 0, Role: "user", Kind: session.KindMessage, Text: "x", Preview: "x"},
			{Index: 1, Role: "assistant", Kind: session.KindToolCall, Text: "abcd", Preview: "abcd"},
			{Index: 2, Role: "user", Kind: session.KindToolResult, Text: "abcdefgh", Preview: "abcdefgh"},
		},
	}
	got := inspect.Build(doc, tokenize.Approximation{})
	want := inspect.Report{
		Source: "opencode",
		Entries: []inspect.Row{
			{Index: 0, Role: "user", Kind: "message", Tokens: 1, Preview: "x"},
			{Index: 1, Role: "assistant", Kind: "tool-call", Tokens: 1, Preview: "abcd"},
			{Index: 2, Role: "user", Kind: "tool-result", Tokens: 2, Preview: "abcdefgh"},
		},
		TotalEntries: 3,
		TotalTokens:  4,
		Tokenizer:    inspect.TokenizerInfo{Name: "approximation", Approximate: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Build() = %+v\nwant      %+v", got, want)
	}
}

func TestBuildEmpty(t *testing.T) {
	doc := &session.Document{Source: "empty", Entries: nil}
	got := inspect.Build(doc, wordTok{})
	if got.Source != "empty" {
		t.Fatalf("Source = %q, want %q", got.Source, "empty")
	}
	if got.TotalEntries != 0 || got.TotalTokens != 0 {
		t.Fatalf("empty doc: entries=%d tokens=%d, want 0 and 0", got.TotalEntries, got.TotalTokens)
	}
	if len(got.Entries) != 0 {
		t.Fatalf("empty doc: %d entries, want none", len(got.Entries))
	}
	if got.Tokenizer != (inspect.TokenizerInfo{Name: "words", Approximate: false}) {
		t.Fatalf("Tokenizer = %+v", got.Tokenizer)
	}
}

func TestRenderTable(t *testing.T) {
	tests := []struct {
		name    string
		tok     tokenize.Tokenizer
		substrs []string
	}{
		{
			name: "approximate",
			tok:  tokenize.Approximation{},
			substrs: []string{
				"IDX", "ROLE", "KIND", "TOKENS", "PREVIEW",
				"user", "tool-call", "tool-result",
				"TOTAL 3 entries · 8 tokens",
				"approximation · approximate",
			},
		},
		{
			name: "exact",
			tok:  wordTok{},
			substrs: []string{
				"TOTAL 3 entries · 6 tokens",
				"words · exact",
			},
		},
	}
	doc := &session.Document{
		Source: "claude",
		Entries: []*session.Entry{
			{Index: 0, Role: "user", Kind: session.KindMessage, Text: "hello world", Preview: "hello world"},
			{Index: 1, Role: "assistant", Kind: session.KindToolCall, Text: "one two three", Preview: "one two three"},
			{Index: 2, Role: "user", Kind: session.KindToolResult, Text: "four", Preview: "four"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := inspect.Render(&b, doc, tc.tok, false); err != nil {
				t.Fatal(err)
			}
			out := b.String()
			for _, s := range tc.substrs {
				if !strings.Contains(out, s) {
					t.Fatalf("Render() output missing %q:\n%s", s, out)
				}
			}
		})
	}
}

func TestRenderTableCommaGroups(t *testing.T) {
	// Four thousand runes count as 1,000 approximation tokens, which exercises
	// the thousands separator in the TOTAL line.
	doc := &session.Document{
		Source: "opencode",
		Entries: []*session.Entry{
			{Index: 0, Role: "user", Kind: session.KindMessage, Text: strings.Repeat("a", 4000), Preview: "big"},
		},
	}
	var b bytes.Buffer
	if err := inspect.Render(&b, doc, tokenize.Approximation{}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "1,000 tokens") {
		t.Fatalf("Render() TOTAL line missing comma grouping:\n%s", b.String())
	}
}

func TestRenderJSON(t *testing.T) {
	doc := &session.Document{
		Source: "opencode",
		Entries: []*session.Entry{
			{Index: 0, Role: "user", Kind: session.KindMessage, Text: "x", Preview: "x"},
		},
	}
	var b bytes.Buffer
	if err := inspect.Render(&b, doc, tokenize.Approximation{}, true); err != nil {
		t.Fatal(err)
	}
	var got inspect.Report
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("Render() wrote invalid JSON: %v\n%s", err, b.String())
	}
	want := inspect.Build(doc, tokenize.Approximation{})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Render() JSON = %+v\nwant       %+v", got, want)
	}
}
