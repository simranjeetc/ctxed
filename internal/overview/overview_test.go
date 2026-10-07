package overview_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/overview"
	"github.com/simranjeetc/ctxed/internal/session"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

// Tokens in these tests are counted with tokenize.Approximation (one token per
// four runes, rounded up), so the numbers are deterministic:
//
//	"x"        1 rune  -> 1 token
//	"abcd"     4 runes -> 1 token
//	"abcde"    5 runes -> 2 tokens
//	"abcdefgh" 8 runes -> 2 tokens
//	"abcdefghi" 9 runes -> 3 tokens
func msg(id, text string) *session.Entry {
	return &session.Entry{ID: id, Role: "user", Kind: session.KindMessage, Text: text}
}

func summary(id, text string) *session.Entry {
	return &session.Entry{ID: id, Role: "assistant", Kind: session.KindSummary, Text: text}
}

func TestBuild(t *testing.T) {
	tok := tokenize.Approximation{}
	cat := func(label string, status string, ids ...string) categorize.Category {
		return categorize.Category{Label: label, Status: status, IDs: ids}
	}
	row := func(kind, label, status string, entries, tokens int, ids ...string) overview.Row {
		return overview.Row{Kind: kind, Label: label, Status: status, Entries: entries, Tokens: tokens, IDs: ids}
	}

	tests := []struct {
		name    string
		source  string
		entries []*session.Entry
		file    categorize.File
		want    overview.Report
	}{
		{
			name:    "placement and sorting",
			source:  "opencode",
			entries: []*session.Entry{msg("e0", "x"), msg("e1", "abcde"), msg("e2", "abcdefghi")},
			file: categorize.File{Categories: []categorize.Category{
				cat("small", categorize.StatusDone, "e0"),
				cat("mid", categorize.StatusInProgress, "e1"),
				cat("large", "", "e2"),
			}},
			want: overview.Report{
				Source:  "opencode",
				Session: "sess",
				Tokenizer: categorize.TokenInfo{
					Name:        "approximation",
					Approximate: true,
				},
				Entries: 3,
				Tokens:  6,
				Rows: []overview.Row{
					row(overview.KindTopic, "large", "", 1, 3, "e2"),
					row(overview.KindTopic, "mid", categorize.StatusInProgress, 1, 2, "e1"),
					row(overview.KindTopic, "small", categorize.StatusDone, 1, 1, "e0"),
				},
			},
		},
		{
			name:    "nearest earlier placed entry",
			source:  "opencode",
			entries: []*session.Entry{msg("e0", "x"), msg("e1", "abcde"), msg("e2", "abcdefghi"), msg("e3", "abcdefgh")},
			file: categorize.File{Categories: []categorize.Category{
				cat("aay", "", "e0"),
				cat("bee", "", "e2"),
			}},
			want: overview.Report{
				Source:  "opencode",
				Session: "sess",
				Tokenizer: categorize.TokenInfo{
					Name:        "approximation",
					Approximate: true,
				},
				Entries: 4,
				Tokens:  8,
				Rows: []overview.Row{
					row(overview.KindTopic, "bee", "", 2, 5, "e2", "e3"),
					row(overview.KindTopic, "aay", "", 2, 3, "e0", "e1"),
				},
			},
		},
		{
			name:    "nearest later placed entry",
			source:  "opencode",
			entries: []*session.Entry{msg("e0", "x"), msg("e1", "abcde")},
			file: categorize.File{Categories: []categorize.Category{
				cat("bee", "", "e1"),
			}},
			want: overview.Report{
				Source:  "opencode",
				Session: "sess",
				Tokenizer: categorize.TokenInfo{
					Name:        "approximation",
					Approximate: true,
				},
				Entries: 2,
				Tokens:  3,
				Rows: []overview.Row{
					row(overview.KindTopic, "bee", "", 2, 3, "e0", "e1"),
				},
			},
		},
		{
			name:    "no placed entry falls back to first topic",
			source:  "opencode",
			entries: []*session.Entry{msg("e0", "x"), msg("e1", "abcde")},
			file: categorize.File{Categories: []categorize.Category{
				cat("aay", "", "nope"),
			}},
			want: overview.Report{
				Source:  "opencode",
				Session: "sess",
				Tokenizer: categorize.TokenInfo{
					Name:        "approximation",
					Approximate: true,
				},
				Entries: 2,
				Tokens:  3,
				Rows: []overview.Row{
					row(overview.KindTopic, "aay", "", 2, 3, "e0", "e1"),
				},
			},
		},
		{
			name:    "no categories makes one whole-session row",
			source:  "opencode",
			entries: []*session.Entry{msg("e0", "x"), msg("e1", "abcde")},
			want: overview.Report{
				Source:  "opencode",
				Session: "sess",
				Tokenizer: categorize.TokenInfo{
					Name:        "approximation",
					Approximate: true,
				},
				Entries: 2,
				Tokens:  3,
				Rows: []overview.Row{
					row(overview.KindTopic, "Whole session", "", 2, 3, "e0", "e1"),
				},
			},
		},
		{
			name:    "summary gets its own row after topics",
			source:  "opencode",
			entries: []*session.Entry{msg("e0", "x"), summary("s1", "abcde")},
			file: categorize.File{
				Categories: []categorize.Category{cat("aay", "", "e0")},
				Pending:    []string{"open question"},
			},
			want: overview.Report{
				Source:  "opencode",
				Session: "sess",
				Tokenizer: categorize.TokenInfo{
					Name:        "approximation",
					Approximate: true,
				},
				Entries: 2,
				Tokens:  3,
				Pending: []string{"open question"},
				Rows: []overview.Row{
					row(overview.KindTopic, "aay", "", 1, 1, "e0"),
					row(overview.KindSummary, "Compaction summary + kept tail", "", 1, 2, "s1"),
				},
			},
		},
		{
			name:    "summary label for a non-opencode source",
			source:  "claude",
			entries: []*session.Entry{msg("e0", "x"), summary("s1", "abcde")},
			file: categorize.File{Categories: []categorize.Category{
				cat("aay", "", "e0"),
			}},
			want: overview.Report{
				Source:  "claude",
				Session: "sess",
				Tokenizer: categorize.TokenInfo{
					Name:        "approximation",
					Approximate: true,
				},
				Entries: 2,
				Tokens:  3,
				Rows: []overview.Row{
					row(overview.KindTopic, "aay", "", 1, 1, "e0"),
					row(overview.KindSummary, "Compaction summary", "", 1, 2, "s1"),
				},
			},
		},
		{
			name:    "empty category is dropped",
			source:  "opencode",
			entries: []*session.Entry{msg("e0", "x")},
			file: categorize.File{Categories: []categorize.Category{
				cat("aay", "", ""),
				cat("bee", "", "e0"),
			}},
			want: overview.Report{
				Source:  "opencode",
				Session: "sess",
				Tokenizer: categorize.TokenInfo{
					Name:        "approximation",
					Approximate: true,
				},
				Entries: 1,
				Tokens:  1,
				Rows: []overview.Row{
					row(overview.KindTopic, "bee", "", 1, 1, "e0"),
				},
			},
		},
		{
			name:    "sort is stable for equal token counts",
			source:  "opencode",
			entries: []*session.Entry{msg("e0", "x"), msg("e1", "abcd")},
			file: categorize.File{Categories: []categorize.Category{
				cat("aay", "", "e0"),
				cat("bee", "", "e1"),
			}},
			want: overview.Report{
				Source:  "opencode",
				Session: "sess",
				Tokenizer: categorize.TokenInfo{
					Name:        "approximation",
					Approximate: true,
				},
				Entries: 2,
				Tokens:  2,
				Rows: []overview.Row{
					row(overview.KindTopic, "aay", "", 1, 1, "e0"),
					row(overview.KindTopic, "bee", "", 1, 1, "e1"),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := &session.Document{Source: tc.source, Entries: tc.entries}
			got := overview.Build(doc, tc.file, tok, "sess")
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Build() = %+v\nwant      %+v", got, tc.want)
			}
		})
	}
}

func TestShort(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want string
	}{
		{"zero", 0, "0"},
		{"small", 950, "950"},
		{"just under a thousand", 999, "999"},
		{"exactly a thousand", 1000, "1.0k"},
		{"thousands", 12300, "12.3k"},
		{"exactly a million", 1000000, "1.0M"},
		{"millions", 1234567, "1.2M"},
		{"negative", -5, "-5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := overview.Short(tc.n); got != tc.want {
				t.Fatalf("Short(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

func TestTopics(t *testing.T) {
	d := &session.Document{
		Entries: []*session.Entry{msg("e0", "x"), summary("s1", "y"), msg("e1", "z")},
	}
	sub := overview.Topics(d)
	if len(sub.Entries) != 2 || sub.Entries[0].ID != "e0" || sub.Entries[1].ID != "e1" {
		t.Fatalf("Topics() = %+v, want the two messages only", sub.Entries)
	}
	if len(d.Entries) != 3 {
		t.Fatalf("Topics() mutated the source document")
	}
}

func TestRenderTable(t *testing.T) {
	tests := []struct {
		name    string
		report  overview.Report
		substrs []string
	}{
		{
			name: "full report",
			report: overview.Report{
				Session:   "sess",
				Tokenizer: categorize.TokenInfo{Name: "approximation", Approximate: true},
				Entries:   4,
				Tokens:    100,
				Compacted: 4,
				Pending:   []string{"a", "b"},
				Rows: []overview.Row{
					{Kind: overview.KindTopic, Label: "a very long topic label that exceeds forty four runes for sure", Status: categorize.StatusDone, Entries: 2, Tokens: 60},
					{Kind: overview.KindTopic, Label: "in progress topic", Status: categorize.StatusInProgress, Entries: 1, Tokens: 30},
					{Kind: overview.KindTopic, Label: "unknown status topic", Status: "weird", Entries: 1, Tokens: 10},
				},
			},
			substrs: []string{
				"Session sess",
				"(estimate)",
				"done",
				"in progress",
				"?",
				"…",
				"Pending: a; b",
				"Not counted: 4 messages",
			},
		},
		{
			name: "zero-token report",
			report: overview.Report{
				Session: "sess",
				Entries: 1,
				Rows: []overview.Row{
					{Kind: overview.KindTopic, Label: "empty", Entries: 1},
				},
			},
			substrs: []string{"~0 tokens", "0%"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := overview.Render(&b, tc.report, false); err != nil {
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

func TestRenderJSON(t *testing.T) {
	want := overview.Report{
		Source:  "opencode",
		Session: "sess",
		Tokenizer: categorize.TokenInfo{
			Name:        "approximation",
			Approximate: true,
		},
		Entries: 1,
		Tokens:  1,
		Rows: []overview.Row{
			{Kind: overview.KindTopic, Label: "aay", Entries: 1, Tokens: 1, IDs: []string{"e0"}},
		},
	}
	var b bytes.Buffer
	if err := overview.Render(&b, want, true); err != nil {
		t.Fatal(err)
	}
	var got overview.Report
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("Render() wrote invalid JSON: %v\n%s", err, b.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Render() JSON = %+v, want %+v", got, want)
	}
}
