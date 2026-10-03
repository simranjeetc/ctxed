package prune_test

import (
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/prune"
	"github.com/simranjeetc/ctxed/internal/session"
)

func buildDoc() *session.Document {
	doc := &session.Document{Format: session.FormatJSON}
	doc.Add(&session.Entry{Role: "assistant", Kind: session.KindToolCall, CallIDs: []string{"c1"}})
	doc.Add(&session.Entry{Role: "user", Kind: session.KindToolResult, ResultIDs: []string{"c1"}})
	doc.Add(&session.Entry{Role: "assistant", Kind: session.KindMessage})
	return doc
}

func TestParseIndices(t *testing.T) {
	got, err := prune.ParseIndices("3, 7,9")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 3 || got[2] != 9 {
		t.Fatalf("got %v", got)
	}
	if _, err := prune.ParseIndices("a,b"); err == nil {
		t.Fatal("expected error for non-numeric index")
	}
	if _, err := prune.ParseIndices("  "); err == nil {
		t.Fatal("expected error for empty index list")
	}
}

func TestValidate(t *testing.T) {
	doc := buildDoc()
	if err := prune.Validate(doc, []int{0, 2}); err != nil {
		t.Fatalf("valid indices rejected: %v", err)
	}
	if err := prune.Validate(doc, []int{5}); err == nil {
		t.Fatal("out-of-range index accepted")
	}
	if err := prune.Validate(doc, []int{1, 1}); err == nil {
		t.Fatal("duplicate index accepted")
	}
}

func TestOrphans(t *testing.T) {
	doc := buildDoc()
	// Dropping the tool call orphans the result.
	o := prune.Orphans(doc, []int{0})
	if len(o) != 1 || !strings.Contains(o[0], "c1") {
		t.Fatalf("expected one orphan naming c1, got %v", o)
	}
	// Dropping the result leaves no orphan.
	if o := prune.Orphans(doc, []int{1}); len(o) != 0 {
		t.Fatalf("dropping the result should not orphan, got %v", o)
	}
	// Dropping both is fine.
	if o := prune.Orphans(doc, []int{0, 1}); len(o) != 0 {
		t.Fatalf("dropping call and result together should not orphan, got %v", o)
	}
}

func TestIndicesForIDs(t *testing.T) {
	doc := &session.Document{Format: session.FormatJSON}
	doc.Add(&session.Entry{ID: "a"})
	doc.Add(&session.Entry{ID: "b"})
	idx, missing := prune.IndicesForIDs(doc, []string{"b", "zz", "a"})
	if len(idx) != 2 {
		t.Fatalf("indices = %v", idx)
	}
	if len(missing) != 1 || missing[0] != "zz" {
		t.Fatalf("missing = %v", missing)
	}
}

func TestResolveOrphans(t *testing.T) {
	doc := &session.Document{}
	doc.Add(&session.Entry{ID: "call", Role: "assistant", CallIDs: []string{"c1"}})
	doc.Add(&session.Entry{ID: "res", Role: "user", ResultIDs: []string{"c1"}})
	doc.Add(&session.Entry{ID: "other", Role: "assistant"})

	final, adj := prune.ResolveOrphans(doc, []int{0})
	if len(final) != 2 || final[0] != 0 || final[1] != 1 {
		t.Fatalf("final = %v, want [0 1]", final)
	}
	if len(adj) != 1 {
		t.Fatalf("expected one adjustment, got %v", adj)
	}

	if _, adj2 := prune.ResolveOrphans(doc, []int{1}); len(adj2) != 0 {
		t.Fatalf("dropping the result needs no adjustment, got %v", adj2)
	}
	if _, adj3 := prune.ResolveOrphans(doc, []int{2}); len(adj3) != 0 {
		t.Fatalf("dropping an unrelated entry needs no adjustment, got %v", adj3)
	}
}

func TestIndicesForIDsFollowsIDsNotPositions(t *testing.T) {
	doc := &session.Document{}
	doc.Add(&session.Entry{ID: "b"})
	doc.Add(&session.Entry{ID: "a"})
	idx, missing := prune.IndicesForIDs(doc, []string{"a"})
	if len(missing) != 0 || len(idx) != 1 || idx[0] != 1 {
		t.Fatalf("id a should resolve to index 1, got idx=%v missing=%v", idx, missing)
	}
}

func TestDropUpdatesEntries(t *testing.T) {
	doc := buildDoc()
	doc.Drop([]int{1})
	if len(doc.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(doc.Entries))
	}
	for _, e := range doc.Entries {
		if e.Index == 1 {
			t.Fatal("dropped entry still present")
		}
	}
}
