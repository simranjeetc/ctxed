package session_test

import (
	"testing"

	"github.com/simranjeetc/ctxed/internal/session"
)

func TestAssignFallbackIDsIsDeterministic(t *testing.T) {
	build := func() *session.Document {
		d := &session.Document{}
		d.Add(&session.Entry{Role: "user", Kind: session.KindMessage, Text: "hello world"})
		d.Add(&session.Entry{Role: "assistant", Kind: session.KindMessage, Text: "hi there"})
		d.Add(&session.Entry{Role: "assistant", Kind: session.KindMessage, Text: "hi there"}) // duplicate content
		d.Add(&session.Entry{Role: "user", Kind: session.KindMessage, Text: "hello world"})  // duplicate content
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
