package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/cli"
)

// stubCategorizer writes a categorizer command that reads the entry numbers
// from the prompt and puts the first in a finished topic and the rest in an open one.
func stubCategorizer(t *testing.T) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "stub.sh")
	body := `#!/bin/sh
ids=$(grep -o '^- \[[0-9]*\]' | tr -dc '0-9\n')
first=$(echo "$ids" | head -1)
rest=$(echo "$ids" | tail -n +2 | paste -sd, -)
printf '{"categories":[{"label":"Finished topic","status":"done","ids":[%s]},{"label":"Open topic","status":"in_progress","ids":[%s]}],"pending":["set the URL"]}' "$first" "$rest"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func clearSessionEnv(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("OPENCODE_SESSION_ID", "")
	t.Setenv("CTXED_CATEGORIZER_CMD", "")
}

type overviewJSON struct {
	Source    string `json:"source"`
	Entries   int    `json:"entries"`
	Tokens    int    `json:"tokens"`
	Compacted int    `json:"compacted"`
	Rows      []struct {
		Kind    string   `json:"kind"`
		Label   string   `json:"label"`
		Status  string   `json:"status"`
		Entries int      `json:"entries"`
		Tokens  int      `json:"tokens"`
		IDs     []string `json:"entryIds"`
	} `json:"rows"`
	Pending []string `json:"pending"`
}

func overviewOf(t *testing.T, args ...string) overviewJSON {
	t.Helper()
	code, out, stderr := run(append([]string{"overview", "--json"}, args...)...)
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	var r overviewJSON
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	return r
}

// inspectTotals is what `ctxed inspect` counts for the same session.
func inspectTotals(t *testing.T, file string) (entries, tokens int) {
	t.Helper()
	code, out, stderr := run("inspect", file, "--json")
	if code != cli.ExitOK {
		t.Fatalf("inspect: exit %d stderr %q", code, stderr)
	}
	var r struct {
		Entries []struct {
			Tokens int `json:"tokens"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	for _, e := range r.Entries {
		tokens += e.Tokens
	}
	return len(r.Entries), tokens
}

func checkTotals(t *testing.T, r overviewJSON, file string) {
	t.Helper()
	sumE, sumT := 0, 0
	seen := map[string]bool{}
	for _, row := range r.Rows {
		sumE += row.Entries
		sumT += row.Tokens
		for _, id := range row.IDs {
			if seen[id] {
				t.Fatalf("entry %s is in two rows", id)
			}
			seen[id] = true
		}
	}
	if sumE != r.Entries || sumT != r.Tokens {
		t.Fatalf("rows sum to %d/%d, header says %d/%d", sumE, sumT, r.Entries, r.Tokens)
	}
	ie, it := inspectTotals(t, file)
	if ie != r.Entries || it != r.Tokens {
		t.Fatalf("overview %d entries ~%d tokens, inspect %d ~%d", r.Entries, r.Tokens, ie, it)
	}
}

func TestOverviewClaudeCompacted(t *testing.T) {
	clearSessionEnv(t)
	in := copyFixture(t, claudeCompactedFixture)
	before, _ := os.ReadFile(in)
	r := overviewOf(t, in, "--categorizer-cmd", stubCategorizer(t))
	after, _ := os.ReadFile(in)
	if !bytes.Equal(before, after) {
		t.Fatal("overview changed the transcript")
	}
	checkTotals(t, r, in)
	if r.Compacted == 0 {
		t.Fatal("no pre-compaction entries reported")
	}
	last := r.Rows[len(r.Rows)-1]
	if last.Kind != "summary" || last.Entries != 1 || last.IDs[0] != compactedSummaryID {
		t.Fatalf("summary row %+v", last)
	}
	for _, row := range r.Rows {
		for _, id := range row.IDs {
			if id == compactedPreBoundaryID {
				t.Fatal("a pre-compaction entry was counted")
			}
		}
	}
}

const ocCompacted = `{"info":{"id":"ses_x"},"messages":[
{"type":"user","id":"m1","text":"OLD topic, before compaction"},
{"type":"assistant","id":"m2","content":[{"type":"text","text":"old answer"}]},
{"type":"compaction","id":"c1","summary":"summary text","recent":"[User]: a long kept tail of the old conversation"},
{"type":"user","id":"m3","text":"first new question"},
{"type":"assistant","id":"m4","content":[{"type":"text","text":"first new answer"}]},
{"type":"user","id":"m5","text":"TODO: set the URL"}]}`

func TestOverviewOpenCodeCompacted(t *testing.T) {
	clearSessionEnv(t)
	in := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(in, []byte(ocCompacted), 0o644); err != nil {
		t.Fatal(err)
	}
	r := overviewOf(t, in, "--categorizer-cmd", stubCategorizer(t))
	checkTotals(t, r, in)
	if r.Compacted != 2 || r.Entries != 4 {
		t.Fatalf("compacted %d entries %d, want 2 and 4", r.Compacted, r.Entries)
	}
	last := r.Rows[len(r.Rows)-1]
	if last.Kind != "summary" || last.Label != "Compaction summary + kept tail" || last.IDs[0] != "c1" {
		t.Fatalf("summary row %+v", last)
	}
	if r.Rows[0].Kind != "topic" || len(r.Pending) != 1 {
		t.Fatalf("rows %+v pending %q", r.Rows, r.Pending)
	}
}

func TestOverviewTable(t *testing.T) {
	clearSessionEnv(t)
	in := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(in, []byte(ocCompacted), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := run("overview", in, "--categorizer-cmd", stubCategorizer(t))
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	for _, want := range []string{"live context: 4 messages", "(estimate)", "Finished topic", "done",
		"Open topic", "in progress", "Compaction summary + kept tail", "Pending: set the URL",
		"Not counted: 2 messages from before the last compaction."} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestOverviewCategorizerFailureStillShowsSizes(t *testing.T) {
	clearSessionEnv(t)
	in := copyFixture(t, claudeCompactedFixture)
	code, out, stderr := run("overview", in, "--categorizer-cmd", "exit 1")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "topics unavailable") || !strings.Contains(out, "Whole session") {
		t.Fatalf("stdout %q stderr %q", out, stderr)
	}
}

func TestOverviewFindsClaudeSessionFromEnv(t *testing.T) {
	clearSessionEnv(t)
	root := t.TempDir()
	id := "11111111-2222-3333-4444-555555555555"
	dir := filepath.Join(root, "projects", "-some-other-dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(claudeCompactedFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	t.Setenv("CLAUDE_CODE_SESSION_ID", id)
	r := overviewOf(t, "--categorizer-cmd", stubCategorizer(t))
	if r.Source != "claude-code" || r.Entries != 9 {
		t.Fatalf("source %q entries %d", r.Source, r.Entries)
	}
}

func TestOverviewNoSession(t *testing.T) {
	clearSessionEnv(t)
	code, _, stderr := run("overview")
	if code != cli.ExitUsage || !strings.Contains(stderr, "--session") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestOverviewBothHarnessesSetIsAmbiguous(t *testing.T) {
	clearSessionEnv(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "11111111-2222-3333-4444-555555555555")
	t.Setenv("OPENCODE_SESSION_ID", "ses_abc")
	code, _, stderr := run("overview")
	if code != cli.ExitUsage {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestOverviewUnsafeSessionRefused(t *testing.T) {
	clearSessionEnv(t)
	for _, id := range []string{"../../etc/passwd", "ses_a/b"} {
		code, _, _ := run("overview", "--session", id)
		if code != cli.ExitUsage {
			t.Fatalf("%q: exit %d", id, code)
		}
	}
}

// The binary's own dispatch does not reach the parked pruning commands.
func TestParkedCommandsAreUnknown(t *testing.T) {
	for _, c := range []string{"drop", "prune", "compact-instruction", "opencode"} {
		var out, errBuf bytes.Buffer
		if code := cli.Run([]string{c}, &out, &errBuf); code != cli.ExitUsage || !strings.Contains(errBuf.String(), "unknown command") {
			t.Fatalf("%s: exit %d stderr %q", c, code, errBuf.String())
		}
	}
}
