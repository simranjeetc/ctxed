// Package overview reports what a session's live context is made of: one row
// per topic with its size and status, plus the compaction summary as its own
// row. It reads a parsed session and a categorization; it never writes.
package overview

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/session"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

// Row kinds.
const (
	KindTopic   = "topic"
	KindSummary = "summary"
)

// Row is one line of the report.
type Row struct {
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Status  string   `json:"status,omitempty"`
	Entries int      `json:"entries"`
	Tokens  int      `json:"tokens"`
	IDs     []string `json:"entryIds"`
}

// Report is the whole overview. Entries and Tokens are the sums of the rows.
type Report struct {
	Source    string               `json:"source"`
	Session   string               `json:"session"`
	Tokenizer categorize.TokenInfo `json:"tokenizer"`
	Entries   int                  `json:"entries"`
	Tokens    int                  `json:"tokens"`
	Compacted int                  `json:"compacted"`
	Rows      []Row                `json:"rows"`
	Pending   []string             `json:"pending,omitempty"`
}

// Topics returns the entries a categorizer should group: every live entry
// except compaction summaries, which get their own row.
func Topics(doc *session.Document) *session.Document {
	sub := *doc
	sub.Entries = nil
	for _, e := range doc.Entries {
		if e.Kind != session.KindSummary {
			sub.Entries = append(sub.Entries, e)
		}
	}
	return &sub
}

// Build assembles the report. Every live entry lands in exactly one row:
// summaries in their own row, and an entry the categorizer did not place (the
// prompt samples long sessions) in the topic of the nearest earlier placed
// entry, or the nearest later one. With no categories, all topic entries form
// one "Whole session" row. Tokens are counted here, per entry, so the totals
// equal `ctxed inspect` on the same session.
func Build(doc *session.Document, f categorize.File, tok tokenize.Tokenizer, sessionName string) Report {
	r := Report{
		Source:    doc.Source,
		Session:   sessionName,
		Tokenizer: categorize.TokenInfo{Name: tok.Name(), Approximate: tok.Approximate()},
		Compacted: doc.Compacted,
		Pending:   f.Pending,
	}

	topicOf := map[string]int{}
	topics := make([]Row, 0, len(f.Categories))
	for i, c := range f.Categories {
		topics = append(topics, Row{Kind: KindTopic, Label: c.Label, Status: c.Status})
		for _, id := range c.IDs {
			topicOf[id] = i
		}
	}

	var summary *Row
	var unplaced []int // positions in doc.Entries
	place := make([]int, len(doc.Entries))
	for i, e := range doc.Entries {
		place[i] = -1
		if e.Kind == session.KindSummary {
			if summary == nil {
				summary = &Row{Kind: KindSummary, Label: summaryLabel(doc.Source)}
			}
			add(summary, e, tok)
			continue
		}
		if t, ok := topicOf[e.ID]; ok {
			place[i] = t
		} else {
			unplaced = append(unplaced, i)
		}
	}
	if len(topics) == 0 && len(unplaced) > 0 {
		topics = append(topics, Row{Kind: KindTopic, Label: "Whole session"})
	}
	for _, i := range unplaced {
		place[i] = nearest(place, i)
	}
	for i, e := range doc.Entries {
		if e.Kind != session.KindSummary && place[i] >= 0 {
			add(&topics[place[i]], e, tok)
		}
	}

	kept := topics[:0]
	for _, t := range topics {
		if t.Entries > 0 {
			kept = append(kept, t)
		}
	}
	sort.SliceStable(kept, func(a, b int) bool { return kept[a].Tokens > kept[b].Tokens })
	r.Rows = kept
	if summary != nil {
		r.Rows = append(r.Rows, *summary)
	}
	for _, row := range r.Rows {
		r.Entries += row.Entries
		r.Tokens += row.Tokens
	}
	return r
}

func add(r *Row, e *session.Entry, tok tokenize.Tokenizer) {
	r.Entries++
	r.Tokens += tok.Count(e.Text)
	r.IDs = append(r.IDs, e.ID)
}

// nearest is the topic of the closest placed entry before i, else after i,
// else the first topic.
func nearest(place []int, i int) int {
	for j := i - 1; j >= 0; j-- {
		if place[j] >= 0 {
			return place[j]
		}
	}
	for j := i + 1; j < len(place); j++ {
		if place[j] >= 0 {
			return place[j]
		}
	}
	return 0
}

func summaryLabel(source string) string {
	if source == "opencode" {
		return "Compaction summary + kept tail"
	}
	return "Compaction summary"
}

// Render prints the report as a table, or as JSON.
func Render(w io.Writer, r Report, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	est := ""
	if r.Tokenizer.Approximate {
		est = " (estimate)"
	}
	_, _ = fmt.Fprintf(w, "Session %s · live context: %d messages · ~%s tokens%s\n\n", r.Session, r.Entries, Short(r.Tokens), est)
	_, _ = fmt.Fprintf(w, " %-2s  %-44s %5s %8s %6s  %s\n", "#", "Topic", "Msgs", "Tokens", "Share", "Status")
	n := 0
	for _, row := range r.Rows {
		num, status := "", "—"
		if row.Kind == KindTopic {
			n++
			num = fmt.Sprint(n)
			status = statusText(row.Status)
		}
		_, _ = fmt.Fprintf(w, " %-2s  %-44s %5d %8s %5.0f%%  %s\n", num, clip(row.Label, 44), row.Entries, Short(row.Tokens), share(row.Tokens, r.Tokens), status)
	}
	if len(r.Pending) > 0 {
		_, _ = fmt.Fprintf(w, "\nPending: %s\n", strings.Join(r.Pending, "; "))
	}
	if r.Compacted > 0 {
		_, _ = fmt.Fprintf(w, "\nNot counted: %d messages from before the last compaction.\n", r.Compacted)
	}
	return nil
}

func statusText(s string) string {
	switch s {
	case categorize.StatusDone:
		return "done"
	case categorize.StatusInProgress:
		return "in progress"
	}
	return "?"
}

func share(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

// Short formats a token count: 950, 12.3k, 1.2M.
func Short(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
