package session_test

import (
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/session"
)

func TestAssignFallbackIDsIsDeterministic(t *testing.T) {
	build := func() *session.Document {
		d := &session.Document{}
		d.Add(&session.Entry{Role: "user", Kind: session.KindMessage, Text: "hello world"})
		d.Add(&session.Entry{Role: "assistant", Kind: session.KindMessage, Text: "hi there"})
		d.Add(&session.Entry{Role: "assistant", Kind: session.KindMessage, Text: "hi there"}) // duplicate content
		d.Add(&session.Entry{Role: "user", Kind: session.KindMessage, Text: "hello world"})   // duplicate content
		d.AssignFallbackIDs()
		return d
	}
	a, b := build(), build()
	for i := range a.Entries {
		if a.Entries[i].ID == "" {
			t.Fatalf("entry %d has no id", i)
		}
		if a.Entries[i].ID != b.Entries[i].ID {
			t.Fatalf("entry %d id not deterministic: %q vs %q", i, a.Entries[i].ID, b.Entries[i].ID)
		}
	}
	// Duplicate content must not collapse to the same id.
	if a.Entries[0].ID == a.Entries[3].ID {
		t.Fatalf("duplicate entries share id %q", a.Entries[0].ID)
	}
	if a.Entries[1].ID == a.Entries[2].ID {
		t.Fatalf("duplicate entries share id %q", a.Entries[1].ID)
	}
}

func TestAssignFallbackIDsKeepsNativeIDs(t *testing.T) {
	d := &session.Document{}
	d.Add(&session.Entry{ID: "msg_123", Role: "user", Kind: session.KindMessage, Text: "x"})
	d.AssignFallbackIDs()
	if d.Entries[0].ID != "msg_123" {
		t.Fatalf("native id overwritten: %q", d.Entries[0].ID)
	}
}

func TestPreview(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"trims surrounding whitespace", "  hello  ", "hello"},
		{"stops at newline", "first\nsecond", "first"},
		{"stops at carriage return", "first\rsecond", "first"},
		{"trims after cutting newline", "first \nsecond", "first"},
		{"short text unchanged", "short", "short"},
		{"truncates over 80 runes", strings.Repeat("a", 85), strings.Repeat("a", 80) + "…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := session.Preview(tc.in); got != tc.want {
				t.Fatalf("Preview(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDropJSON(t *testing.T) {
	d := &session.Document{Format: session.FormatJSON}
	d.Add(&session.Entry{Role: "user", Kind: session.KindMessage, Text: "a"})
	d.Add(&session.Entry{Role: "assistant", Kind: session.KindMessage, Text: "b"})
	d.Items = []session.RawItem{
		{Raw: []byte(`"x"`), EntryIndex: 0},
		{Raw: []byte(`"meta"`), EntryIndex: -1},
		{Raw: []byte(`"y"`), EntryIndex: 1},
	}
	d.Drop([]int{0})

	if !d.Items[0].Dropped {
		t.Fatal("dropped entry's item not marked dropped")
	}
	if d.Items[1].Dropped {
		t.Fatal("non-entry item must never be marked dropped")
	}
	if d.Items[2].Dropped {
		t.Fatal("kept entry's item must not be marked dropped")
	}
	if len(d.Entries) != 1 || d.Entries[0].Text != "b" {
		t.Fatalf("entries after drop = %#v, want only the kept entry", d.Entries)
	}
}

func TestDropJSONL(t *testing.T) {
	d := &session.Document{Format: session.FormatJSONL}
	d.Add(&session.Entry{Role: "user", Kind: session.KindMessage, Text: "a"})
	d.Add(&session.Entry{Role: "assistant", Kind: session.KindMessage, Text: "b"})
	d.Lines = []session.RawLine{
		{Raw: `{"a":1}`, EntryIndex: 0},
		{Raw: `{"b":2}`, EntryIndex: 1},
	}
	d.Drop([]int{1})

	if d.Lines[0].Dropped {
		t.Fatal("kept entry's line must not be marked dropped")
	}
	if !d.Lines[1].Dropped {
		t.Fatal("dropped entry's line not marked dropped")
	}
	if len(d.Entries) != 1 || d.Entries[0].Text != "a" {
		t.Fatalf("entries after drop = %#v, want only the kept entry", d.Entries)
	}
}
