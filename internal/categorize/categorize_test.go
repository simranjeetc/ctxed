package categorize_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/adapter"
	_ "github.com/simranjeetc/ctxed/internal/adapter/builtin"
	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/session"
	"github.com/simranjeetc/ctxed/internal/tokenize"
)

const ocFixture = "../../testdata/opencode_session.json"

func loadDoc(t *testing.T) *session.Document {
	t.Helper()
	data, err := os.ReadFile(ocFixture)
	if err != nil {
		t.Fatal(err)
	}
	a, err := adapter.Detect(data)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := a.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// Entries are numbered, not listed by id: a model answering with ids spends
// most of its output (and time) copying them.
func TestPromptNumbersEntries(t *testing.T) {
	doc := loadDoc(t)
	p := categorize.Prompt(doc, 5, 30000)
	for i, e := range doc.Entries {
		if !strings.Contains(p, fmt.Sprintf("- [%d] role=%s ", i+1, e.Role)) {
			t.Fatalf("prompt missing entry %d", i+1)
		}
		if strings.Contains(p, e.ID) {
			t.Fatalf("prompt lists id %q", e.ID)
		}
	}
}

func TestPromptIsBounded(t *testing.T) {
	// A large session must not produce a prompt proportional to its size. A
	// multi-hundred-KB prompt makes the model call slow enough to time out — the
	// failure this bound exists to prevent.
	doc := loadDoc(t)
	big := *doc
	big.Entries = nil
	for i := 0; i < 2000; i++ {
		e := doc.Entries[i%len(doc.Entries)]
		e.ID = "msg_" + strings.Repeat("0", 5) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26))
		e.Text = strings.Repeat("realistic session text about one topic. ", 20)
		big.Entries = append(big.Entries, e)
	}
	const budget = 30000
	p := categorize.Prompt(&big, 5, budget)
	if len(p) > budget {
		t.Fatalf("prompt is %d bytes, over the %d budget", len(p), budget)
	}
	if !strings.Contains(p, "Entries:") {
		t.Fatal("prompt lost its header")
	}
}

func TestParseCoversEveryEntry(t *testing.T) {
	doc := loadDoc(t)
	ids := []string{doc.Entries[0].ID, doc.Entries[1].ID, doc.Entries[2].ID}
	resp := `{"categories":[{"label":"A","ids":["` + ids[0] + `","` + ids[1] + `"]},{"label":"B","ids":["` + ids[2] + `"]}]}`

	f, err := categorize.Parse(resp, doc, tokenize.Approximation{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Categories) != 2 {
		t.Fatalf("got %d categories", len(f.Categories))
	}
	if len(f.Uncategorized) != 0 {
		t.Fatalf("unexpected uncategorized: %v", f.Uncategorized)
	}
	total := 0
	for _, c := range f.Categories {
		total += c.Count
		if c.Tokens <= 0 {
			t.Fatalf("category %q has no tokens", c.Label)
		}
	}
	if total != len(doc.Entries) {
		t.Fatalf("assigned %d of %d entries", total, len(doc.Entries))
	}
}

func TestParseLeavesUnassignedInUncategorized(t *testing.T) {
	doc := loadDoc(t)
	ids := []string{doc.Entries[0].ID, doc.Entries[1].ID, doc.Entries[2].ID}
	resp := `{"categories":[{"label":"A","ids":["` + ids[0] + `"]},{"label":"B","ids":["` + ids[1] + `"]}]}`

	f, err := categorize.Parse(resp, doc, tokenize.Approximation{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Uncategorized) != 1 || f.Uncategorized[0] != ids[2] {
		t.Fatalf("uncategorized = %v, want [%s]", f.Uncategorized, ids[2])
	}
}

func TestParseExtractsJSONFromProse(t *testing.T) {
	doc := loadDoc(t)
	ids := []string{doc.Entries[0].ID, doc.Entries[1].ID, doc.Entries[2].ID}
	resp := "Sure, here you go:\n```json\n{\"categories\":[{\"label\":\"A\",\"ids\":[\"" + ids[0] + "\",\"" + ids[1] + "\"]},{\"label\":\"B\",\"ids\":[\"" + ids[2] + "\"]}]}\n```\n"
	if _, err := categorize.Parse(resp, doc, tokenize.Approximation{}, 5); err != nil {
		t.Fatalf("should parse a fenced response: %v", err)
	}
}

func TestParseEnforcesCategoryBounds(t *testing.T) {
	doc := loadDoc(t)
	one := `{"categories":[{"label":"only","ids":["` + doc.Entries[0].ID + `"]}]}`
	if _, err := categorize.Parse(one, doc, tokenize.Approximation{}, 5); err == nil {
		t.Fatal("expected an error for fewer than two categories")
	}
	many := `{"categories":[{"label":"1","ids":["a"]},{"label":"2","ids":["b"]},{"label":"3","ids":["c"]},{"label":"4","ids":["d"]},{"label":"5","ids":["e"]},{"label":"6","ids":["f"]}]}`
	if _, err := categorize.Parse(many, doc, tokenize.Approximation{}, 5); err == nil {
		t.Fatal("expected an error for more than the maximum categories")
	}
}

func TestParseRejectsNonJSON(t *testing.T) {
	doc := loadDoc(t)
	if _, err := categorize.Parse("no json here", doc, tokenize.Approximation{}, 5); err == nil {
		t.Fatal("expected an error")
	}
}

func TestValidateRejectsUnknownID(t *testing.T) {
	doc := loadDoc(t)
	f := categorize.File{Categories: []categorize.Category{{ID: 1, Label: "A", IDs: []string{"does-not-exist"}}}}
	err := categorize.Validate(f, doc)
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("expected an error naming the bad id, got %v", err)
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	doc := loadDoc(t)
	ids := []string{doc.Entries[0].ID, doc.Entries[1].ID, doc.Entries[2].ID}
	resp := `{"categories":[{"label":"Edited label","ids":["` + ids[0] + `","` + ids[1] + `"]},{"label":"B","ids":["` + ids[2] + `"]}]}`
	f, err := categorize.Parse(resp, doc, tokenize.Approximation{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cats.json")
	if err := categorize.WriteFile(path, f); err != nil {
		t.Fatal(err)
	}
	got, err := categorize.ReadFile(path, doc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Categories[0].Label != "Edited label" {
		t.Fatalf("label not preserved: %q", got.Categories[0].Label)
	}
}

func TestSelect(t *testing.T) {
	f := categorize.File{Categories: []categorize.Category{
		{ID: 1, Label: "A", IDs: []string{"x", "y"}},
		{ID: 2, Label: "B", IDs: []string{"z"}},
	}}
	got, err := categorize.Select(f, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "z" {
		t.Fatalf("select = %v", got)
	}
	if _, err := categorize.Select(f, []int{9}); err == nil {
		t.Fatal("expected an error for an unknown category")
	}
}

func TestLoadRejectsUnknownID(t *testing.T) {
	doc := loadDoc(t)
	if _, err := categorize.Load([]byte(`{"categories":[{"id":1,"label":"A","entryIds":["nope"]},{"id":2,"label":"B","entryIds":[]}]}`), doc); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseStatusAndPending(t *testing.T) {
	doc := loadDoc(t)
	ids := []string{doc.Entries[0].ID, doc.Entries[1].ID, doc.Entries[2].ID}
	resp := `{"categories":[` +
		`{"label":"A","status":"Done","ids":["` + ids[0] + `"]},` +
		`{"label":"B","status":"in progress","ids":["` + ids[1] + `"]},` +
		`{"label":"C","status":"maybe","ids":["` + ids[2] + `"]}],` +
		`"pending":[" set the URL ","","b","c","d","e","f"]}`
	f, err := categorize.Parse(resp, doc, tokenize.Approximation{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{f.Categories[0].Status, f.Categories[1].Status, f.Categories[2].Status}
	want := []string{categorize.StatusDone, categorize.StatusInProgress, ""}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statuses %q, want %q", got, want)
		}
	}
	if len(f.Pending) != 5 || f.Pending[0] != "set the URL" {
		t.Fatalf("pending %q", f.Pending)
	}
}

func TestPromptAsksForStatusAndPending(t *testing.T) {
	p := categorize.Prompt(loadDoc(t), 5, 30000)
	for _, s := range []string{`"status"`, `"pending"`, "in_progress"} {
		if !strings.Contains(p, s) {
			t.Fatalf("prompt does not mention %s", s)
		}
	}
}

func TestParseNumbersAndRanges(t *testing.T) {
	doc := loadDoc(t)
	if len(doc.Entries) < 4 {
		t.Skip("fixture too small")
	}
	n := len(doc.Entries)
	resp := fmt.Sprintf(`{"categories":[{"label":"A","ids":[1,"2"]},{"label":"B","ids":["3-%d","%d"]},{"label":"C","ids":["0","99999-100000"]}]}`, n+5, n)
	f, err := categorize.Parse(resp, doc, tokenize.Approximation{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Categories) != 2 || f.Categories[0].Count != 2 || f.Categories[1].Count != n-2 {
		t.Fatalf("categories %+v", f.Categories)
	}
	if f.Categories[0].IDs[0] != doc.Entries[0].ID || f.Categories[1].IDs[0] != doc.Entries[2].ID {
		t.Fatalf("numbers resolved to the wrong entries: %+v", f.Categories)
	}
	if len(f.Uncategorized) != 0 {
		t.Fatalf("uncategorized %v", f.Uncategorized)
	}
}
