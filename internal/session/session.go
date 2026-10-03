// Package session defines the canonical, harness-neutral model of a session
// document and the opaque source material needed to write an edited document
// back in its original shape.
package session

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"unicode/utf8"
)

// Kind classifies a conversation entry.
type Kind string

const (
	KindMessage    Kind = "message"
	KindToolCall   Kind = "tool-call"
	KindToolResult Kind = "tool-result"
	KindReasoning  Kind = "reasoning"
	KindSummary    Kind = "summary"
)

// Format is the on-disk shape of a session document.
type Format string

const (
	FormatJSON  Format = "json"
	FormatJSONL Format = "jsonl"
)

// Entry is one addressable conversation entry.
type Entry struct {
	Index     int
	ID        string // stable id: harness-provided (msg_…, uuid) or a deterministic fallback
	Role      string
	Kind      Kind
	Text      string
	Preview   string
	CallIDs   []string // tool-call ids this entry issues
	ResultIDs []string // tool-call ids this entry answers
	Raw       json.RawMessage
}

// AssignFallbackIDs gives every entry without a native id a stable fallback id
// derived from its content — not its position — so a reference keeps meaning
// across re-enumeration.
func (d *Document) AssignFallbackIDs() {
	seen := map[string]int{}
	for _, e := range d.Entries {
		if e.ID != "" {
			continue
		}
		base := "fx_" + shortHash(e.Role+"\x00"+string(e.Kind)+"\x00"+e.Text)
		n := seen[base]
		seen[base] = n + 1
		if n == 0 {
			e.ID = base
		} else {
			e.ID = fmt.Sprintf("%s-%d", base, n+1)
		}
	}
}

func shortHash(s string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%012x", h.Sum64()&0xffffffffffff)
}

// Preview returns the first line of text, trimmed and truncated for display.
func Preview(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimSpace(text)
	const max = 80
	if utf8.RuneCountInString(text) > max {
		text = string([]rune(text)[:max]) + "…"
	}
	return text
}

// RawItem is one element of a JSON document's entries array, in original order.
// EntryIndex is -1 when the item is not a conversation entry; such items are
// always preserved on write.
type RawItem struct {
	Raw        json.RawMessage
	EntryIndex int
	Dropped    bool
}

// RawLine is one line of a JSONL document, in original order.
type RawLine struct {
	Raw        string
	EntryIndex int
	Dropped    bool
}

// Document is a parsed session plus everything needed to write it back.
type Document struct {
	Source  string
	Format  Format
	Entries []*Entry

	// JSON source (FormatJSON).
	Top        map[string]json.RawMessage
	EntriesKey string
	Items      []RawItem

	// JSONL source (FormatJSONL).
	Lines []RawLine
}

// Add appends an entry, assigning its index.
func (d *Document) Add(e *Entry) {
	e.Index = len(d.Entries)
	e.Preview = Preview(e.Text)
	d.Entries = append(d.Entries, e)
}

// Drop marks the given original entry indices as removed.
func (d *Document) Drop(indices []int) {
	drop := make(map[int]bool, len(indices))
	for _, i := range indices {
		drop[i] = true
	}
	switch d.Format {
	case FormatJSON:
		for i := range d.Items {
			if d.Items[i].EntryIndex >= 0 && drop[d.Items[i].EntryIndex] {
				d.Items[i].Dropped = true
			}
		}
	case FormatJSONL:
		for i := range d.Lines {
			if d.Lines[i].EntryIndex >= 0 && drop[d.Lines[i].EntryIndex] {
				d.Lines[i].Dropped = true
			}
		}
	}
	kept := d.Entries[:0]
	for _, e := range d.Entries {
		if !drop[e.Index] {
			kept = append(kept, e)
		}
	}
	d.Entries = kept
}
