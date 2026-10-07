package overview_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/simranjeetc/ctxed/internal/categorize"
	"github.com/simranjeetc/ctxed/internal/overview"
)

// update regenerates the golden file when `go test ./internal/overview -update`
// is passed.
var update = flag.Bool("update", false, "update table.golden")

// TestRenderGolden renders a fixed report and compares the table against
// testdata/table.golden, so any change to the table layout is caught and can be
// regenerated deliberately with -update.
func TestRenderGolden(t *testing.T) {
	report := overview.Report{
		Source:  "opencode",
		Session: "golden-session",
		Tokenizer: categorize.TokenInfo{
			Name:        "approximation",
			Approximate: true,
		},
		Entries:   7,
		Tokens:    1268,
		Compacted: 7,
		Pending:   []string{"question one", "question two"},
		Rows: []overview.Row{
			{
				Kind:    overview.KindTopic,
				Label:   "a very long topic label that will be clipped by the table",
				Status:  categorize.StatusDone,
				Entries: 3,
				Tokens:  800,
			},
			{
				Kind:    overview.KindTopic,
				Label:   "second topic",
				Status:  categorize.StatusInProgress,
				Entries: 2,
				Tokens:  400,
			},
			{
				Kind:    overview.KindTopic,
				Label:   "weird status",
				Status:  "unexpected",
				Entries: 1,
				Tokens:  34,
			},
			{
				Kind:    overview.KindSummary,
				Label:   "Compaction summary + kept tail",
				Entries: 1,
				Tokens:  34,
			},
		},
	}

	var b bytes.Buffer
	if err := overview.Render(&b, report, false); err != nil {
		t.Fatal(err)
	}
	got := b.String()

	golden := filepath.Join("testdata", "table.golden")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden file: %v (run `go test ./internal/overview -update` to create it)", err)
	}
	if got != string(want) {
		t.Fatalf("Render() output differs from %s:\n--- got ---\n%s--- want ---\n%s", golden, got, want)
	}
}
