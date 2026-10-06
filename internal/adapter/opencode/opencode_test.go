package opencode_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/simranjeetc/ctxed/internal/adapter/opencode"
	"github.com/simranjeetc/ctxed/internal/session"
)

const fixture = "../../../testdata/opencode_session.json"

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestDetect(t *testing.T) {
	a := &opencode.Adapter{}
	if !a.Detect(readFixture(t)) {
		t.Fatal("fixture not detected as opencode")
	}
	if a.Detect([]byte(`{"messages":[]}`)) && false {
		t.Fatal("unreachable")
	}
	if a.Detect([]byte("not json")) {
		t.Fatal("non-JSON detected as opencode")
	}
}

func TestParseEntries(t *testing.T) {
	doc, err := (&opencode.Adapter{}).Parse(readFixture(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Fixture has 4 messages: user, assistant, idle, system. idle is not an entry.
	if len(doc.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(doc.Entries))
	}
	if doc.Entries[0].Role != "user" || doc.Entries[0].Kind != session.KindMessage {
		t.Fatalf("entry 0 = %+v", doc.Entries[0])
	}
	if doc.Entries[1].Kind != session.KindToolCall {
		t.Fatalf("assistant with tool parts should be a tool-call, got %q", doc.Entries[1].Kind)
	}
	if len(doc.Entries[1].CallIDs) == 0 {
		t.Fatal("tool-call entry has no call ids")
	}
	if doc.Entries[2].Role != "system" {
		t.Fatalf("entry 2 role = %q", doc.Entries[2].Role)
	}
}

func TestRoundTripReparses(t *testing.T) {
	a := &opencode.Adapter{}
	doc, _ := a.Parse(readFixture(t))
	out, err := a.Write(doc)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	doc2, err := a.Parse(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(doc2.Entries) != len(doc.Entries) {
		t.Fatalf("entries %d -> %d after round-trip", len(doc.Entries), len(doc2.Entries))
	}
}

func TestUnknownFieldsSurviveRoundTrip(t *testing.T) {
	raw := readFixture(t)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	top["customField"] = json.RawMessage(`{"keep":"me"}`)
	modified, _ := json.Marshal(top)

	a := &opencode.Adapter{}
	doc, err := a.Parse(modified)
	if err != nil {
		t.Fatal(err)
	}
	out, err := a.Write(doc)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	json.Unmarshal(out, &got)
	var custom struct {
		Keep string `json:"keep"`
	}
	if err := json.Unmarshal(got["customField"], &custom); err != nil || custom.Keep != "me" {
		t.Fatalf("customField not preserved: %s (err %v)", got["customField"], err)
	}
	if _, ok := got["info"]; !ok {
		t.Fatal("info field lost")
	}
}

func TestNonEntryMessagePreservedOnWrite(t *testing.T) {
	a := &opencode.Adapter{}
	data := readFixture(t)
	doc, _ := a.Parse(data)
	// Drop every conversation entry; the idle message must survive.
	ids := make([]int, 0, len(doc.Entries))
	for _, e := range doc.Entries {
		ids = append(ids, e.Index)
	}
	doc.Drop(ids)
	out, _ := a.Write(doc)

	var got struct {
		Messages []struct {
			Type string `json:"type"`
		} `json:"messages"`
	}
	json.Unmarshal(out, &got)
	if len(got.Messages) != 1 || got.Messages[0].Type != "idle" {
		t.Fatalf("expected only the idle message to remain, got %+v", got.Messages)
	}
}

// A compacted export: only the last compaction (as a summary carrying its
// summary and kept tail) and what follows it are entries; writing keeps every item.
func TestParseCompactedShowsLiveContext(t *testing.T) {
	data := []byte(`{"info":{"id":"ses_x"},"messages":[
{"type":"user","id":"m1","text":"old one"},
{"type":"compaction","id":"c1","summary":"first summary","recent":"first tail"},
{"type":"assistant","id":"m2","content":[{"type":"text","text":"old two"}]},
{"type":"compaction","id":"c2","summary":"SUMMARY","recent":"[User]: TAIL"},
{"type":"user","id":"m3","text":"new"},
{"type":"idle"}]}`)
	a := &opencode.Adapter{}
	doc, err := a.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Compacted != 2 {
		t.Fatalf("compacted %d, want 2", doc.Compacted)
	}
	if len(doc.Entries) != 2 {
		t.Fatalf("entries %d, want 2", len(doc.Entries))
	}
	s := doc.Entries[0]
	if s.ID != "c2" || s.Kind != session.KindSummary || s.Text != "SUMMARY\n[User]: TAIL" {
		t.Fatalf("summary entry %+v", s)
	}
	if doc.Entries[1].ID != "m3" {
		t.Fatalf("live entry %q", doc.Entries[1].ID)
	}
	out, err := a.Write(doc)
	if err != nil {
		t.Fatal(err)
	}
	var top struct{ Messages []json.RawMessage }
	if err := json.Unmarshal(out, &top); err != nil || len(top.Messages) != 6 {
		t.Fatalf("write kept %d items (%v), want 6", len(top.Messages), err)
	}
}
