package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	_ "github.com/simranjeetc/ctxed/internal/adapter/builtin"
	"github.com/simranjeetc/ctxed/internal/cli"
)

const (
	opencodeFixture          = "../../testdata/opencode_session.json"
	claudeFixture            = "../../testdata/claude_session.jsonl"
	ocCategorizeResponse     = "../../testdata/opencode_session.categorize.json"
	claudeCategorizeResponse = "../../testdata/claude_session.categorize.json"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = cli.Run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func copyFixture(t *testing.T, src string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dst := filepath.Join(t.TempDir(), filepath.Base(src))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dst
}

func snapshot(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	m := map[string]int64{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			m[p] = info.ModTime().UnixNano()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return m
}

func TestNoArgsIsUsageError(t *testing.T) {
	code, out, _ := run()
	if code != cli.ExitUsage {
		t.Fatalf("exit %d, want %d", code, cli.ExitUsage)
	}
	if out != "" {
		t.Fatalf("usage error wrote to stdout: %q", out)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	code, _, errOut := run("frobnicate")
	if code != cli.ExitUsage {
		t.Fatalf("exit %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := run("version")
	if code != cli.ExitOK || !strings.HasPrefix(out, "ctxed ") {
		t.Fatalf("version: exit %d out %q", code, out)
	}
}

func TestInspectTable(t *testing.T) {
	code, out, errOut := run("inspect", opencodeFixture)
	if code != cli.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if errOut != "" {
		t.Fatalf("success wrote to stderr: %q", errOut)
	}
	for _, want := range []string{"IDX", "ROLE", "KIND", "TOKENS", "PREVIEW", "TOTAL", "tokenizer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("inspect output missing %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "\n") - 1; got != 4 { // header + 3 entries
		t.Fatalf("expected header + 3 rows, got %d non-total lines:\n%s", got, out)
	}
}

func TestInspectJSON(t *testing.T) {
	code, out, _ := run("inspect", opencodeFixture, "--json")
	if code != cli.ExitOK {
		t.Fatalf("exit %d", code)
	}
	var r struct {
		Source       string `json:"source"`
		TotalEntries int    `json:"totalEntries"`
		TotalTokens  int    `json:"totalTokens"`
		Tokenizer    struct {
			Name        string `json:"name"`
			Approximate bool   `json:"approximate"`
		} `json:"tokenizer"`
		Entries []struct {
			Index   int    `json:"index"`
			Role    string `json:"role"`
			Kind    string `json:"kind"`
			Tokens  int    `json:"tokens"`
			Preview string `json:"preview"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("stdout is not a JSON stats object: %v\n%s", err, out)
	}
	if r.Source != "opencode" || r.TotalEntries != 3 || len(r.Entries) != 3 {
		t.Fatalf("unexpected report: %+v", r)
	}
	if !r.Tokenizer.Approximate {
		t.Fatal("default run should use the labelled approximation")
	}
	sum := 0
	for _, e := range r.Entries {
		sum += e.Tokens
	}
	if sum != r.TotalTokens {
		t.Fatalf("total %d != sum of entries %d", r.TotalTokens, sum)
	}
}

func TestInspectIsReadOnly(t *testing.T) {
	before := snapshot(t, "../../testdata")
	if code, _, errOut := run("inspect", claudeFixture); code != cli.ExitOK {
		t.Fatalf("inspect failed: %d %s", code, errOut)
	}
	after := snapshot(t, "../../testdata")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("inspect modified the working tree:\nbefore %v\nafter  %v", before, after)
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

func TestMissingFileIsRuntimeError(t *testing.T) {
	code, _, errOut := run("inspect", "/no/such/session.json")
	if code != cli.ExitError {
		t.Fatalf("exit %d, want %d", code, cli.ExitError)
	}
	if errOut == "" {
		t.Fatal("expected an error message")
	}
}

func TestUnknownFormatIsRuntimeError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "junk.txt")
	if err := os.WriteFile(p, []byte("not a session file"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run("inspect", p)
	if code != cli.ExitError {
		t.Fatalf("exit %d, want %d", code, cli.ExitError)
	}
	if !strings.Contains(errOut, "supported formats") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestInspectClaude(t *testing.T) {
	code, out, errOut := run("inspect", claudeFixture, "--json")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if !strings.Contains(out, "claude-code") || !strings.Contains(out, "tool-result") {
		t.Fatalf("unexpected claude inspect output:\n%s", out)
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

func categorizeTo(t *testing.T, in, response string) string {
	t.Helper()
	out := filepath.Join(filepath.Dir(in), "cats.json")
	code, _, stderr := run("categorize", in, "--categorizer-cmd", "cat "+response, "--out", out)
	if code != cli.ExitOK {
		t.Fatalf("categorize failed: exit %d, stderr %q", code, stderr)
	}
	return out
}

func TestCategorizeWritesFileAndLeavesSessionUnchanged(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	before, _ := os.ReadFile(in)
	out := filepath.Join(filepath.Dir(in), "cats.json")

	code, stdout, stderr := run("categorize", in, "--categorizer-cmd", "cat "+ocCategorizeResponse, "--out", out)
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "CAT") || !strings.Contains(stdout, "wrote") {
		t.Fatalf("stdout = %q", stdout)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("categories file missing: %v", err)
	}
	var f struct {
		Categories []struct {
			Label      string `json:"label"`
			EntryCount int    `json:"entryCount"`
			Tokens     int    `json:"tokens"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("categories file invalid: %v", err)
	}
	if len(f.Categories) != 2 {
		t.Fatalf("categories = %d, want 2", len(f.Categories))
	}
	for _, c := range f.Categories {
		if c.Label == "" || c.Tokens <= 0 {
			t.Fatalf("category missing label or tokens: %+v", c)
		}
	}
	after, _ := os.ReadFile(in)
	if !bytes.Equal(before, after) {
		t.Fatal("categorize modified the session file")
	}
}

func TestCategorizeNoModelConfigured(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("CTXED_MODEL", "")
	in := copyFixture(t, opencodeFixture)
	code, out, stderr := run("categorize", in, "--out", filepath.Join(t.TempDir(), "c.json"))
	if code != cli.ExitError {
		t.Fatalf("exit %d, want %d", code, cli.ExitError)
	}
	if out != "" {
		t.Fatalf("stdout should be empty, got %q", out)
	}
	if !strings.Contains(stderr, "no model configured") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCategorizeOversizedInput(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	code, _, stderr := run("categorize", in, "--categorizer-cmd", "cat "+ocCategorizeResponse,
		"--max-input-bytes", "10", "--out", filepath.Join(t.TempDir(), "c.json"))
	if code != cli.ExitError || !strings.Contains(stderr, "limit") {
		t.Fatalf("exit %d stderr %q", code, stderr)
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
	const prunedID = "msg_101f9625c001NLzjIh2rzpuhNj" // category 2
	const keptID = "msg_101f96175001KNBHKRACZI25DD"   // category 1
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
	for _, id := range []string{"61e80e18", "46cb1fcf", "c3990fee"} {
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
		if m.ID == "msg_101f9625c001NLzjIh2rzpuhNj" {
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
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
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
	if !strings.Contains(stdout, "msg_101f9625c001NLzjIh2rzpuhNj") {
		t.Fatal("edit not honored: the moved entry should be retained")
	}
}

func assertNoEditedFile(t *testing.T, in string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(in), "*.edited.*"))
	if len(matches) != 0 {
		t.Fatalf("expected no edited file, found %v", matches)
	}
}
