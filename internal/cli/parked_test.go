//go:build parked

package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/cli"
)

const claudeCategorizeResponse = "../../testdata/claude_session.categorize.json"

func categorizeTo(t *testing.T, in, response string) string {
	t.Helper()
	out := filepath.Join(filepath.Dir(in), "cats.json")
	code, _, stderr := run("categorize", in, "--categorizer-cmd", "cat "+response, "--out", out)
	if code != cli.ExitOK {
		t.Fatalf("categorize failed: exit %d, stderr %q", code, stderr)
	}
	return out
}

func assertNoEditedFile(t *testing.T, in string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(in), "*.edited.*"))
	if len(matches) != 0 {
		t.Fatalf("expected no edited file, found %v", matches)
	}
}

func TestDropWritesAndLeavesInputIntact(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	before, _ := os.ReadFile(in)

	code, out, errOut := run("drop", in, "--indices", "0")
	if code != cli.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "wrote") {
		t.Fatalf("stdout = %q", out)
	}
	after, _ := os.ReadFile(in)
	if !bytes.Equal(before, after) {
		t.Fatal("input file was modified")
	}

	edited := strings.TrimSuffix(in, ".json") + ".edited.json"
	doc, err := os.ReadFile(edited)
	if err != nil {
		t.Fatalf("expected edited file: %v", err)
	}
	var top struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(doc, &top); err != nil {
		t.Fatalf("edited file is not valid JSON: %v", err)
	}
	// 4 original messages minus the one dropped entry.
	if len(top.Messages) != 3 {
		t.Fatalf("edited messages = %d, want 3", len(top.Messages))
	}
	_ = errOut
}

func TestDropJSONStats(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	code, out, _ := run("drop", in, "--indices", "0", "--json")
	if code != cli.ExitOK {
		t.Fatalf("exit %d", code)
	}
	var s struct {
		EntriesBefore  int   `json:"entriesBefore"`
		EntriesAfter   int   `json:"entriesAfter"`
		TokensBefore   int   `json:"tokensBefore"`
		TokensAfter    int   `json:"tokensAfter"`
		RemovedIndices []int `json:"removedIndices"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, out)
	}
	if s.EntriesBefore != 3 || s.EntriesAfter != 2 {
		t.Fatalf("entries %d -> %d", s.EntriesBefore, s.EntriesAfter)
	}
	if s.TokensAfter >= s.TokensBefore {
		t.Fatalf("tokens did not drop: %d -> %d", s.TokensBefore, s.TokensAfter)
	}
	if len(s.RemovedIndices) != 1 || s.RemovedIndices[0] != 0 {
		t.Fatalf("removedIndices = %v", s.RemovedIndices)
	}
}

func TestDropOutOverride(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	outPath := filepath.Join(filepath.Dir(in), "custom.out")
	code, _, errOut := run("drop", in, "--indices", "1", "--out", outPath)
	if code != cli.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("--out not honored: %v", err)
	}
}

func TestDropInvalidIndexRefused(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	code, _, errOut := run("drop", in, "--indices", "99")
	if code != cli.ExitRefused {
		t.Fatalf("exit %d, want %d", code, cli.ExitRefused)
	}
	if !strings.Contains(errOut, "not in the session") {
		t.Fatalf("stderr = %q", errOut)
	}
	assertNoEditedFile(t, in)
}

func TestDropDuplicateIndexRefused(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	code, _, errOut := run("drop", in, "--indices", "0,0")
	if code != cli.ExitRefused || !strings.Contains(errOut, "more than once") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	assertNoEditedFile(t, in)
}

func TestDropOrphanRefusedAndForceOverrides(t *testing.T) {
	in := copyFixture(t, claudeFixture)
	// Entry 1 is the tool_use; dropping it orphans the tool_result entry 2.
	code, _, errOut := run("drop", in, "--indices", "1")
	if code != cli.ExitRefused {
		t.Fatalf("exit %d, want %d (stderr %q)", code, cli.ExitRefused, errOut)
	}
	if !strings.Contains(errOut, "answers tool call") {
		t.Fatalf("stderr = %q", errOut)
	}
	assertNoEditedFile(t, in)

	code, _, errOut = run("drop", in, "--indices", "1", "--force")
	if code != cli.ExitOK {
		t.Fatalf("--force exit %d, stderr %q", code, errOut)
	}
	edited := strings.TrimSuffix(in, ".jsonl") + ".edited.jsonl"
	if _, err := os.Stat(edited); err != nil {
		t.Fatalf("--force did not write: %v", err)
	}
}

func TestDropMissingIndicesIsUsageError(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	code, _, errOut := run("drop", in)
	if code != cli.ExitUsage || !strings.Contains(errOut, "--indices") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
}

// TestStdinClosed proves write commands need no terminal.
func TestStdinClosed(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	code, _, errOut := run("drop", in, "--indices", "0", "--json")
	if code != cli.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestPruneByCategoryIsNonDestructive(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	before, _ := os.ReadFile(in)
	cats := categorizeTo(t, in, ocCategorizeResponse)

	code, stdout, stderr := run("prune", in, "--categories-file", cats, "--categories", "2")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	const prunedID = "msg_00000000000000000000000002" // category 2
	const keptID = "msg_00000000000000000000000001"   // category 1
	if strings.Contains(stdout, prunedID) {
		t.Fatalf("pruned id still present:\n%s", stdout)
	}
	if !strings.Contains(stdout, keptID) {
		t.Fatalf("retained id missing")
	}
	after, _ := os.ReadFile(in)
	if !bytes.Equal(before, after) {
		t.Fatal("prune modified the session file")
	}
}

func TestPruneResolvesOrphanAndReports(t *testing.T) {
	in := copyFixture(t, claudeFixture)
	cats := categorizeTo(t, in, claudeCategorizeResponse)

	// Category 1 holds the tool_use; its result lives in category 2, so pruning
	// category 1 must also drop the orphaned result and say so.
	code, stdout, stderr := run("prune", in, "--categories-file", cats, "--categories", "1")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "adjustment:") {
		t.Fatalf("expected an adjustment on stderr, got %q", stderr)
	}
	for _, id := range []string{"aaaaaaaa", "bbbbbbbb", "cccccccc"} {
		if strings.Contains(stdout, id) {
			t.Fatalf("entry %s should have been pruned:\n%s", id, stdout)
		}
	}
	if !strings.Contains(stdout, "ai-title") {
		t.Fatal("non-entry line ai-title was lost")
	}
}

func TestPruneUnknownIDRefused(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	code, out, stderr := run("prune", in, "--ids", "nope")
	if code != cli.ExitRefused {
		t.Fatalf("exit %d, want %d", code, cli.ExitRefused)
	}
	if out != "" {
		t.Fatalf("stdout should be empty on refusal, got %q", out)
	}
	if !strings.Contains(stderr, "nope") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestPruneUnknownCategoryRefused(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)
	code, _, stderr := run("prune", in, "--categories-file", cats, "--categories", "9")
	if code != cli.ExitRefused || !strings.Contains(stderr, "no category") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestPruneDeterministic(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)
	_, a, _ := run("prune", in, "--categories-file", cats, "--categories", "1")
	_, b, _ := run("prune", in, "--categories-file", cats, "--categories", "1")
	if a != b {
		t.Fatal("prune output is not deterministic")
	}
}

func TestPruneMissingSelectionIsUsageError(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	code, _, stderr := run("prune", in)
	if code != cli.ExitUsage || !strings.Contains(stderr, "--ids") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestPruneStdinClosed(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	code, _, errOut := run("prune", in, "--categories-file", cats, "--categories", "1")
	if code != cli.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

// TestPluginRoleThinSubstitution proves a harness plugin only has to parse the
// emitted transcript and substitute it — no categorization or prune logic.
func TestPluginRoleThinSubstitution(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)
	_, transcript, _ := run("prune", in, "--categories-file", cats, "--categories", "2")

	var doc struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(transcript), &doc); err != nil {
		t.Fatalf("plugin cannot parse the transcript: %v", err)
	}
	for _, m := range doc.Messages {
		if m.ID == "msg_00000000000000000000000002" {
			t.Fatal("pruned message still in the plugin's mapped transcript")
		}
	}
}

func TestCompactInstructionByCategory(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	before, _ := os.ReadFile(in)
	cats := categorizeTo(t, in, ocCategorizeResponse)

	code, stdout, stderr := run("compact-instruction", in, "--categories-file", cats, "--categories", "2")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	// The guarantee-gap caution goes to stderr, so the pasteable sentence on
	// stdout stays clean.
	if !strings.Contains(stderr, "does not guarantee") {
		t.Fatalf("expected the best-effort caution on stderr, got %q", stderr)
	}
	line := strings.TrimSpace(stdout)
	if strings.Count(stdout, "\n") != 1 {
		t.Fatalf("expected exactly one instruction line, got %q", stdout)
	}
	if !strings.Contains(line, "keep the context about Context editor design discussion") {
		t.Fatalf("kept bucket not named by label:\n%s", stdout)
	}
	if !strings.Contains(line, "drop the context about Adapter implementation notes") {
		t.Fatalf("dropped bucket not named by label:\n%s", stdout)
	}
	after, _ := os.ReadFile(in)
	if !bytes.Equal(before, after) {
		t.Fatal("compact-instruction modified the session file")
	}
}

func TestCompactInstructionIsDeterministic(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)
	codeA, a, _ := run("compact-instruction", in, "--categories-file", cats, "--categories", "1")
	codeB, b, _ := run("compact-instruction", in, "--categories-file", cats, "--categories", "1")
	if codeA != cli.ExitOK || codeB != cli.ExitOK {
		t.Fatalf("exits %d, %d", codeA, codeB)
	}
	if a != b {
		t.Fatalf("not deterministic:\n%q\n%q", a, b)
	}
}

func TestCompactInstructionUnknownCategoryRefused(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)
	code, out, stderr := run("compact-instruction", in, "--categories-file", cats, "--categories", "9")
	if code != cli.ExitRefused {
		t.Fatalf("exit %d, want %d (stderr %q)", code, cli.ExitRefused, stderr)
	}
	if out != "" {
		t.Fatalf("stdout should be empty on refusal, got %q", out)
	}
	if !strings.Contains(stderr, "no category") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCompactInstructionMissingSelectionIsUsageError(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)
	code, _, stderr := run("compact-instruction", in, "--categories-file", cats)
	if code != cli.ExitUsage || !strings.Contains(stderr, "--categories") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestCompactInstructionStdinClosed(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()

	code, _, errOut := run("compact-instruction", in, "--categories-file", cats, "--categories", "1")
	if code != cli.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestPruneHonorsEditedCategoriesFile(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)

	// Move the assistant entry from category 2 into category 1, then drop
	// category 2 — the edit must be honored.
	var f map[string]any
	data, _ := os.ReadFile(cats)
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	list := f["categories"].([]any)
	c1 := list[0].(map[string]any)
	c2 := list[1].(map[string]any)
	moved := c2["entryIds"].([]any)
	c1["entryIds"] = append(c1["entryIds"].([]any), moved...)
	c2["entryIds"] = []any{}
	edited, _ := json.Marshal(f)
	if err := os.WriteFile(cats, edited, 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := run("prune", in, "--categories-file", cats, "--categories", "2")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "msg_00000000000000000000000002") {
		t.Fatal("edit not honored: the moved entry should be retained")
	}
}

func TestPruneIDsOnlyByCategory(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)

	code, stdout, stderr := run("prune", in, "--categories-file", cats, "--categories", "2", "--ids-only")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	var r struct {
		DroppedIDs []string `json:"droppedIds"`
	}
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, stdout)
	}
	want := []string{"msg_00000000000000000000000002"} // category 2
	if !reflect.DeepEqual(r.DroppedIDs, want) {
		t.Fatalf("droppedIds = %v, want %v", r.DroppedIDs, want)
	}
	if strings.Contains(stdout, `"messages"`) {
		t.Fatal("ids-only emitted a transcript instead of an id set")
	}
}

func TestPruneIDsOnlyByExplicitID(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	const id = "msg_00000000000000000000000001"
	code, stdout, stderr := run("prune", in, "--ids", id, "--ids-only")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	var r struct {
		DroppedIDs []string `json:"droppedIds"`
	}
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !reflect.DeepEqual(r.DroppedIDs, []string{id}) {
		t.Fatalf("droppedIds = %v, want [%s]", r.DroppedIDs, id)
	}
}

func TestPruneIDsOnlyReflectsOrphanResolution(t *testing.T) {
	in := copyFixture(t, claudeFixture)
	cats := categorizeTo(t, in, claudeCategorizeResponse)

	// Category 1 holds the tool_use; its result is in category 2, so the
	// resolved set must include the dependent result's id too.
	code, stdout, stderr := run("prune", in, "--categories-file", cats, "--categories", "1", "--ids-only")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "adjustment:") {
		t.Fatalf("expected an adjustment on stderr, got %q", stderr)
	}
	var r struct {
		DroppedIDs []string `json:"droppedIds"`
	}
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := []string{
		"aaaaaaaa-0000-4000-8000-0000000000a1",
		"bbbbbbbb-0000-4000-8000-0000000000b2",
		"cccccccc-0000-4000-8000-0000000000c3",
	}
	if !reflect.DeepEqual(r.DroppedIDs, want) {
		t.Fatalf("droppedIds = %v, want %v", r.DroppedIDs, want)
	}
}

func TestPruneIDsOnlyNothingDropped(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)

	// Empty the selected category so the selection resolves to no ids.
	var f map[string]any
	data, _ := os.ReadFile(cats)
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	list := f["categories"].([]any)
	list[1].(map[string]any)["entryIds"] = []any{}
	edited, _ := json.Marshal(f)
	if err := os.WriteFile(cats, edited, 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := run("prune", in, "--categories-file", cats, "--categories", "2", "--ids-only")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if got := strings.TrimSpace(stdout); got != `{"droppedIds":[],"droppedToolCallIds":[]}` {
		t.Fatalf("stdout = %q, want %q", got, `{"droppedIds":[],"droppedToolCallIds":[]}`)
	}
}

func TestPruneIDsOnlyEmitsDroppedToolCallIDs(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	// Entry 1 is a tool-call entry: dropping it must also report the tool-call
	// ids it issued, so a plugin can drop the matching result even when the live
	// result message carries no id of its own.
	code, stdout, stderr := run("prune", in, "--ids", "msg_00000000000000000000000002", "--ids-only")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	var parsed struct {
		DroppedIDs         []string `json:"droppedIds"`
		DroppedToolCallIDs []string `json:"droppedToolCallIds"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("parse: %v\n%s", err, stdout)
	}
	if len(parsed.DroppedToolCallIDs) == 0 {
		t.Fatalf("expected dropped tool-call ids for a tool-call entry, got none: %s", stdout)
	}
	// Deterministic ordering.
	if !sort.StringsAreSorted(parsed.DroppedToolCallIDs) {
		t.Fatalf("droppedToolCallIds not sorted: %v", parsed.DroppedToolCallIDs)
	}
}

func TestPruneIDsOnlyEmptySelection(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	// An explicit but empty --ids is a selection that resolves to no ids.
	code, stdout, stderr := run("prune", in, "--ids", "", "--ids-only")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if got := strings.TrimSpace(stdout); got != `{"droppedIds":[],"droppedToolCallIds":[]}` {
		t.Fatalf("stdout = %q, want %q", got, `{"droppedIds":[],"droppedToolCallIds":[]}`)
	}
}

func TestPruneIDsOnlyDeterministic(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)
	_, a, _ := run("prune", in, "--categories-file", cats, "--categories", "1", "--ids-only")
	_, b, _ := run("prune", in, "--categories-file", cats, "--categories", "1", "--ids-only")
	if a != b {
		t.Fatalf("ids-only output is not deterministic:\n%s\n%s", a, b)
	}
}

func TestPruneFromStdinIDsOnly(t *testing.T) {
	data, err := os.ReadFile(opencodeFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Build a proper categories file by categorizing the fixture (same as other
	// prune tests), then prune the same content arriving on stdin.
	in := copyFixture(t, opencodeFixture)
	cats := categorizeTo(t, in, ocCategorizeResponse)
	code, stdout, stderr := runStdin(string(data), "prune", "-",
		"--categories-file", cats, "--categories", "2", "--ids-only")
	if code != cli.ExitOK {
		t.Fatalf("prune from stdin failed: %d %q", code, stderr)
	}
	if !strings.Contains(stdout, "droppedIds") {
		t.Fatalf("stdout = %q, want a droppedIds object", stdout)
	}
	// The stdin path and the file path must resolve to the same drop set.
	_, fromFile, _ := run("prune", in, "--categories-file", cats, "--categories", "2", "--ids-only")
	if strings.TrimSpace(stdout) != strings.TrimSpace(fromFile) {
		t.Fatalf("stdin and file drop sets differ:\n%s\n%s", stdout, fromFile)
	}
}

func TestCategorizeCompactedSeesOnlyLiveEntries(t *testing.T) {
	in := copyFixture(t, claudeCompactedFixture)
	dir := filepath.Dir(in)
	prompt := filepath.Join(dir, "prompt.txt")
	response := filepath.Join(dir, "response.json")
	// The response also names a pre-boundary id; it must not reach a bucket.
	resp := `{"categories":[` +
		`{"label":"Earlier work","ids":["` + compactedSummaryID + `","` + compactedPreBoundaryID + `"]},` +
		`{"label":"Logo","ids":["` + compactedTopicFourID + `"]}]}`
	if err := os.WriteFile(response, []byte(resp), 0o644); err != nil {
		t.Fatal(err)
	}
	cats := filepath.Join(dir, "cats.json")
	code, _, stderr := run("categorize", in, "--categorizer-cmd", "cat > "+prompt+"; cat "+response, "--out", cats)
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	sent, _ := os.ReadFile(prompt)
	if strings.Contains(string(sent), compactedPreBoundaryID) || strings.Contains(string(sent), "Topic one") {
		t.Fatalf("the categorizer was shown pre-boundary entries:\n%s", sent)
	}
	if !strings.Contains(string(sent), "Topic four") {
		t.Fatalf("the categorizer was not shown the live entries:\n%s", sent)
	}
	data, _ := os.ReadFile(cats)
	if strings.Contains(string(data), compactedPreBoundaryID) {
		t.Fatalf("a category references a pre-boundary entry:\n%s", data)
	}

	code, stdout, stderr := run("prune", in, "--categories-file", cats, "--categories", "2", "--ids-only")
	if code != cli.ExitOK {
		t.Fatalf("prune: exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, compactedTopicFourID) {
		t.Fatalf("prune did not resolve the live bucket: %s", stdout)
	}

	code, stdout, stderr = run("compact-instruction", in, "--categories-file", cats, "--categories", "2")
	if code != cli.ExitOK {
		t.Fatalf("compact-instruction: exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "drop the context about Logo") {
		t.Fatalf("instruction = %q", stdout)
	}
}

func TestPruneCompactedEntryIsUnknown(t *testing.T) {
	in := copyFixture(t, claudeCompactedFixture)
	code, _, stderr := run("prune", in, "--ids", compactedPreBoundaryID, "--ids-only")
	if code != cli.ExitRefused {
		t.Fatalf("pruning a pre-boundary id: exit %d, want %d (stderr %q)", code, cli.ExitRefused, stderr)
	}
}

func TestDropCompactedKeepsHistory(t *testing.T) {
	in := copyFixture(t, claudeCompactedFixture)
	before, _ := os.ReadFile(in)
	out := filepath.Join(filepath.Dir(in), "edited.jsonl")
	// Index 6 is the "Topic four" prompt in the live view.
	if code, _, stderr := run("drop", in, "--indices", "6", "--out", out); code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	after, _ := os.ReadFile(out)
	if strings.Contains(string(after), compactedTopicFourID) {
		t.Fatal("dropped entry still present")
	}
	in0 := strings.Split(string(before), "\n")
	out0 := strings.Split(string(after), "\n")
	if len(out0) != len(in0)-1 {
		t.Fatalf("%d lines -> %d, want exactly one fewer", len(in0), len(out0))
	}
	for i, l := range in0 {
		if strings.Contains(l, compactedTopicFourID) {
			break
		}
		if out0[i] != l {
			t.Fatalf("line %d before the dropped entry changed", i)
		}
	}
}
