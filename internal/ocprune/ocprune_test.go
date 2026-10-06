package ocprune

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/simranjeetc/ctxed/internal/adapter"
	_ "github.com/simranjeetc/ctxed/internal/adapter/builtin"
	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/session"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

// compactedExport is an OpenCode export shaped like a real one: two turns,
// a compaction, a system item, then turns that carry a tool part.
const compactedExport = `{"info":{"id":"ses_test1"},"messages":[
{"type":"user","id":"msg_old_u","text":"TOPIC-OLD: before compaction"},
{"type":"assistant","id":"msg_old_a","content":[{"type":"text","text":"ok"}]},
{"type":"idle","id":"msg_idle1","outcome":"done"},
{"type":"compaction","id":"msg_cmp","status":"completed","reason":"manual","summary":"old topic summarised"},
{"type":"system","id":"msg_sys","text":"Instructions updated"},
{"type":"user","id":"msg_a_u","text":"TOPIC-ALPHA: alpha"},
{"type":"assistant","id":"msg_a_a","content":[{"type":"tool","id":"call_1","name":"read","state":{}},{"type":"text","text":"ok"}]},
{"type":"user","id":"msg_b_u","text":"TOPIC-BETA: beta"},
{"type":"assistant","id":"msg_b_a","content":[{"type":"text","text":"ok"}]}
]}`

func parse(t *testing.T, data string) *session.Document {
	t.Helper()
	a, err := adapter.Detect([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := a.Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func ids(doc *session.Document) map[string]bool {
	m := map[string]bool{}
	for _, e := range doc.Entries {
		m[e.ID] = true
	}
	return m
}

func TestLiveViewSkipsBeforeCompaction(t *testing.T) {
	doc := parse(t, compactedExport)
	left := LiveView(doc, Dropped{})
	got := ids(doc)
	if got["msg_old_u"] || got["msg_old_a"] {
		t.Fatalf("pre-compaction entries are listed: %v", got)
	}
	for _, id := range []string{"msg_sys", "msg_a_u", "msg_a_a", "msg_b_u", "msg_b_a"} {
		if !got[id] {
			t.Fatalf("live entry %s missing: %v", id, got)
		}
	}
	// The adapter already leaves out the two pre-compaction entries; the live
	// view leaves out the compaction summary, which is never offered for dropping.
	if doc.Compacted != 2 || left != 1 {
		t.Fatalf("compacted %d, left out %d; want 2 and 1", doc.Compacted, left)
	}
}

func TestLiveViewSkipsDropped(t *testing.T) {
	doc := parse(t, compactedExport)
	LiveView(doc, Dropped{IDs: []string{"msg_a_u"}, ToolCallIDs: []string{"call_1"}})
	got := ids(doc)
	if got["msg_a_u"] || got["msg_a_a"] {
		t.Fatalf("dropped entries are listed again: %v", got)
	}
	if !got["msg_b_u"] {
		t.Fatalf("kept entry missing: %v", got)
	}
}

func TestLiveViewWithoutCompactionKeepsAll(t *testing.T) {
	doc := parse(t, `{"info":{},"messages":[{"type":"user","id":"m1","text":"a"},{"type":"assistant","id":"m2","content":[{"type":"text","text":"b"}]}]}`)
	if left := LiveView(doc, Dropped{}); left != 0 || len(doc.Entries) != 2 {
		t.Fatalf("left %d, entries %d", left, len(doc.Entries))
	}
}

func listing(t *testing.T, doc *session.Document, response string) []Category {
	t.Helper()
	tok, _ := tokenize.Resolve("", "")
	f, err := categorize.Parse(response, doc, tok, 5)
	if err != nil {
		t.Fatal(err)
	}
	return NewListing(f, doc)
}

func TestNewListingCarriesToolCalls(t *testing.T) {
	doc := parse(t, compactedExport)
	LiveView(doc, Dropped{})
	l := listing(t, doc, `{"categories":[{"label":"alpha","ids":["msg_a_u","msg_a_a"]},{"label":"rest","ids":["msg_sys","msg_b_u","msg_b_a"]}]}`)
	if len(l) != 2 || l[0].ID != 1 || l[0].Label != "alpha" {
		t.Fatalf("listing: %+v", l)
	}
	if len(l[0].ToolCallIDs) != 1 || l[0].ToolCallIDs[0] != "call_1" {
		t.Fatalf("tool calls: %+v", l[0].ToolCallIDs)
	}
}

func TestDropAccumulates(t *testing.T) {
	s := State{Session: "ses_x", Listing: []Category{
		{ID: 1, Label: "a", EntryIDs: []string{"m1", "m2"}, ToolCallIDs: []string{"c1"}},
		{ID: 2, Label: "b", EntryIDs: []string{"m3"}},
	}}
	if _, err := s.Drop([]int{1}); err != nil {
		t.Fatal(err)
	}
	s.Listing = []Category{{ID: 1, Label: "c", EntryIDs: []string{"m9", "m1"}}}
	if _, err := s.Drop([]int{1}); err != nil {
		t.Fatal(err)
	}
	want := []string{"m1", "m2", "m9"}
	if len(s.Dropped.IDs) != len(want) {
		t.Fatalf("dropped %v, want %v", s.Dropped.IDs, want)
	}
	for i := range want {
		if s.Dropped.IDs[i] != want[i] {
			t.Fatalf("dropped %v, want %v", s.Dropped.IDs, want)
		}
	}
	if len(s.Dropped.ToolCallIDs) != 1 {
		t.Fatalf("tool calls %v", s.Dropped.ToolCallIDs)
	}
}

func TestDropUnknownNumberChangesNothing(t *testing.T) {
	s := State{Session: "ses_x", Listing: []Category{{ID: 1, Label: "a", EntryIDs: []string{"m1"}}}}
	if _, err := s.Drop([]int{1, 4}); err == nil {
		t.Fatal("want an error for category 4")
	}
	if len(s.Dropped.IDs) != 0 {
		t.Fatalf("drop list changed: %v", s.Dropped.IDs)
	}
}

func TestDropWithoutListing(t *testing.T) {
	s := State{Session: "ses_x"}
	if _, err := s.Drop([]int{1}); err == nil {
		t.Fatal("want an error with no listing")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := State{Session: "ses_abc123", Dropped: Dropped{IDs: []string{"m1"}}}
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "ses_abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Dropped.IDs) != 1 || got.Dropped.ToolCallIDs == nil {
		t.Fatalf("round trip: %+v", got)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	got, err := Load(t.TempDir(), "ses_none")
	if err != nil || got.Session != "ses_none" || len(got.Dropped.IDs) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestBadSessionRefused(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"../etc", "ses_a/b", "abc", "", "ses_"} {
		if _, err := Load(dir, id); !errors.Is(err, ErrBadSession) {
			t.Fatalf("%q: want ErrBadSession, got %v", id, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("a bad session id touched the state dir")
	}
}
