package claude_test

import (
	"os"
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/adapter/claude"
	"github.com/simranjeetc/ctxed/internal/session"
)

const fixture = "../../../testdata/claude_session.jsonl"

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestDetect(t *testing.T) {
	a := &claude.Adapter{}
	if !a.Detect(readFixture(t)) {
		t.Fatal("fixture not detected as claude-code")
	}
	if a.Detect([]byte(`{"info":{},"messages":[]}`)) {
		t.Fatal("an opencode-style object detected as claude-code")
	}
}

func TestParseEntries(t *testing.T) {
	doc, err := (&claude.Adapter{}).Parse(readFixture(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Fixture lines: ai-title (non-entry), assistant thinking, assistant tool_use, user tool_result.
	if len(doc.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(doc.Entries))
	}
	if doc.Entries[0].Kind != session.KindReasoning {
		t.Fatalf("entry 0 kind = %q, want reasoning", doc.Entries[0].Kind)
	}
	if doc.Entries[1].Kind != session.KindToolCall || len(doc.Entries[1].CallIDs) == 0 {
		t.Fatalf("entry 1 = %+v, want tool-call with ids", doc.Entries[1])
	}
	if doc.Entries[2].Kind != session.KindToolResult || len(doc.Entries[2].ResultIDs) == 0 {
		t.Fatalf("entry 2 = %+v, want tool-result with ids", doc.Entries[2])
	}
	if doc.Entries[1].CallIDs[0] != doc.Entries[2].ResultIDs[0] {
		t.Fatalf("tool call %v not matched by result %v", doc.Entries[1].CallIDs, doc.Entries[2].ResultIDs)
	}
}

func TestNonEntryLinePreservedOnWrite(t *testing.T) {
	a := &claude.Adapter{}
	doc, _ := a.Parse(readFixture(t))
	ids := make([]int, 0, len(doc.Entries))
	for _, e := range doc.Entries {
		ids = append(ids, e.Index)
	}
	doc.Drop(ids)
	out, err := a.Write(doc)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !strings.Contains(string(out), `"ai-title"`) {
		t.Fatalf("non-entry ai-title line was lost:\n%s", out)
	}
	if strings.Contains(string(out), `"tool_use"`) {
		t.Fatal("dropped tool_use line still present")
	}
}

func TestRoundTripReparses(t *testing.T) {
	a := &claude.Adapter{}
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
