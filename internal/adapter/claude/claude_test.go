package claude_test

import (
	"encoding/json"
	"fmt"
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

// The compacted fixture is a real transcript (Claude Code 2.1.291), scrubbed:
// topic one, topic two, /compact, topic three, /compact, topic four. Each
// boundary preserves the last assistant turn before it.
const compactedFixture = "../../../testdata/claude_session_compacted.jsonl"

func parseCompacted(t *testing.T) (*claude.Adapter, []byte, *session.Document) {
	t.Helper()
	data, err := os.ReadFile(compactedFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	a := &claude.Adapter{}
	doc, err := a.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return a, data, doc
}

func TestCompactedLiveView(t *testing.T) {
	_, _, doc := parseCompacted(t)
	var texts []string
	for _, e := range doc.Entries {
		texts = append(texts, e.Preview)
	}
	all := strings.Join(texts, "\n")
	// Summary (1), preserved topic-three reply (thinking + text = 2), /compact
	// caveat, command and stdout (3), topic four prompt and reply (3).
	if len(doc.Entries) != 9 {
		t.Fatalf("got %d live entries, want 9:\n%s", len(doc.Entries), all)
	}
	for _, gone := range []string{"Topic one", "Topic two", "Topic three"} {
		if strings.Contains(all, gone) {
			t.Fatalf("pre-boundary prompt %q is in the live view:\n%s", gone, all)
		}
	}
	if !strings.Contains(all, "Topic four") {
		t.Fatalf("post-boundary prompt missing from the live view:\n%s", all)
	}
	for i, e := range doc.Entries {
		if e.Index != i {
			t.Fatalf("live entry %d has index %d", i, e.Index)
		}
	}
}

func TestCompactedSummaryKindAndLastBoundaryWins(t *testing.T) {
	_, _, doc := parseCompacted(t)
	s := doc.Entries[0]
	if s.Kind != session.KindSummary || s.Role != "user" {
		t.Fatalf("entry 0 = %s/%s, want user/summary", s.Role, s.Kind)
	}
	// The second summary is the live one; the first is history.
	if !strings.Contains(s.Text, "Topic three") && !strings.Contains(s.Text, "Fridays") {
		t.Fatalf("live summary is not the last compaction's:\n%s", s.Text)
	}
	summaries := 0
	for _, e := range doc.Entries {
		if e.Kind == session.KindSummary {
			summaries++
		}
	}
	if summaries != 1 {
		t.Fatalf("%d summary entries live, want 1", summaries)
	}
	// Preserved lines follow the summary.
	if doc.Entries[1].Kind != session.KindReasoning || doc.Entries[2].Preview != "ok" {
		t.Fatalf("preserved turn not after the summary: %+v %+v", doc.Entries[1], doc.Entries[2])
	}
}

func TestCompactedRoundTripKeepsPreBoundaryLines(t *testing.T) {
	a, data, doc := parseCompacted(t)
	out, err := a.Write(doc)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if string(out) != string(data) {
		t.Fatal("unedited write is not byte-identical")
	}

	last := len(doc.Entries) - 1
	doc.Drop([]int{last})
	out, err = a.Write(doc)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	in := strings.Split(string(data), "\n")
	got := strings.Split(string(out), "\n")
	if len(got) != len(in)-1 {
		t.Fatalf("%d lines -> %d, want one fewer", len(in), len(got))
	}
	boundary := -1
	for i, l := range in {
		if strings.Contains(l, `"subtype":"compact_boundary"`) {
			boundary = i
		}
	}
	for i := 0; i <= boundary; i++ {
		if got[i] != in[i] {
			t.Fatalf("pre-boundary line %d changed", i)
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, path := range []string{fixture, compactedFixture} {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatalf("read seed %s: %v", path, err)
		}
		f.Add(data)
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		// Parse must never panic on arbitrary input.
		_, _ = (&claude.Adapter{}).Parse(data)
	})
}

// generateSession builds a synthetic n-message Claude Code transcript: a
// leading ai-title line, then alternating user/assistant messages, each carrying
// a realistic multi-token body. It exercises the same code paths as a real
// transcript without needing a fixture on disk.
func generateSession(n int) []byte {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	_ = enc.Encode(map[string]any{
		"type":      "ai-title",
		"aiTitle":   "generated benchmark session",
		"sessionId": "bench",
	})
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		_ = enc.Encode(map[string]any{
			"type": role,
			"uuid": fmt.Sprintf("msg-%06d", i),
			"message": map[string]any{
				"role":    role,
				"content": benchmarkBody(i),
			},
		})
	}
	return []byte(b.String())
}

func benchmarkBody(i int) string {
	return fmt.Sprintf("message %d about context windows: %s", i, strings.Repeat("token ", 40))
}

func BenchmarkParse(b *testing.B) {
	data := generateSession(2000)
	a := &claude.Adapter{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := a.Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}
