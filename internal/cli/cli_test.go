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
	opencodeFixture      = "../../testdata/opencode_session.json"
	claudeFixture        = "../../testdata/claude_session.jsonl"
	ocCategorizeResponse = "../../testdata/opencode_session.categorize.json"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = cli.RunWithParked(args, strings.NewReader(""), &out, &errBuf)
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

// runStdin is run() with an explicit stdin, for the piped-transcript path.
func runStdin(stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errBuf bytes.Buffer
	code = cli.RunWithParked(args, strings.NewReader(stdin), &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestCategorizeFromStdinMatchesFile(t *testing.T) { // The same session and response must produce the same categories whether the
	// session arrives as a path or on stdin.
	data, err := os.ReadFile(opencodeFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	fileOut := filepath.Join(t.TempDir(), "from-file.json")
	code, _, stderr := run("categorize", opencodeFixture,
		"--categorizer-cmd", "cat "+ocCategorizeResponse, "--out", fileOut)
	if code != cli.ExitOK {
		t.Fatalf("file categorize failed: %d %q", code, stderr)
	}
	stdinOut := filepath.Join(t.TempDir(), "from-stdin.json")
	code, _, stderr = runStdin(string(data), "categorize", "-",
		"--categorizer-cmd", "cat "+ocCategorizeResponse, "--out", stdinOut)
	if code != cli.ExitOK {
		t.Fatalf("stdin categorize failed: %d %q", code, stderr)
	}
	a, _ := os.ReadFile(fileOut)
	b, _ := os.ReadFile(stdinOut)
	// The session field records the source name, so compare the categories body.
	var fa, fb map[string]any
	if err := json.Unmarshal(a, &fa); err != nil {
		t.Fatalf("parse file out: %v", err)
	}
	if err := json.Unmarshal(b, &fb); err != nil {
		t.Fatalf("parse stdin out: %v", err)
	}
	delete(fa, "session")
	delete(fb, "session")
	if !reflect.DeepEqual(fa, fb) {
		t.Fatalf("categories differ between file and stdin:\n%v\n%v", fa, fb)
	}
}

func TestCategorizeOutDashPrintsJSONToStdout(t *testing.T) {
	in := copyFixture(t, opencodeFixture)
	// `--out -` must print the categories document on stdout, not write a file
	// named "-", so a plugin can read the buckets without a temp file.
	code, stdout, stderr := run("categorize", in,
		"--categorizer-cmd", "cat "+ocCategorizeResponse, "--out", "-")
	if code != cli.ExitOK {
		t.Fatalf("categorize --out - failed: exit %d, stderr %q", code, stderr)
	}
	if strings.Contains(stdout, "wrote -") {
		t.Fatalf("--out - wrote a file instead of printing: %q", stdout)
	}
	var parsed struct {
		Categories []struct {
			Label string `json:"label"`
			ID    int    `json:"id"`
		} `json:"categories"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("stdout is not the categories JSON: %v\n%s", err, stdout)
	}
	if len(parsed.Categories) != 2 || parsed.Categories[0].ID != 1 {
		t.Fatalf("unexpected categories: %+v", parsed.Categories)
	}
	// No file named "-" may be created in the working directory.
	if _, err := os.Stat("-"); err == nil {
		t.Fatal("--out - created a literal '-' file")
	}
}

// claudeCompactedFixture was compacted twice; only the summary, the turn the
// last compaction preserved, and what follows are live.
const claudeCompactedFixture = "../../testdata/claude_session_compacted.jsonl"

const (
	compactedPreBoundaryID = "0519c9a1-e3b5-4f8d-bd2e-726597806b31" // "Topic one", before both boundaries
	compactedSummaryID     = "e28825bc-c506-46e0-aeeb-2e3c0c87f548" // the last compaction's summary
	compactedTopicFourID   = "dc7cd06f-0bd8-4ef0-9884-48be47a7d4ce" // after the last boundary
)

func TestInspectCompactedShowsLiveContext(t *testing.T) {
	code, out, errOut := run("inspect", claudeCompactedFixture, "--json")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	var r struct {
		Entries []struct {
			Kind string `json:"kind"`
		} `json:"entries"`
		TotalEntries int `json:"totalEntries"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if r.TotalEntries != 9 || len(r.Entries) != 9 {
		t.Fatalf("total %d rows %d, want 9/9", r.TotalEntries, len(r.Entries))
	}
	if r.Entries[0].Kind != "summary" {
		t.Fatalf("entry 0 kind %q, want summary", r.Entries[0].Kind)
	}
	if strings.Contains(out, "Topic one") {
		t.Fatalf("inspect shows compacted history:\n%s", out)
	}
}
