package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/cli"
	"github.com/simranjeetc/ctxed/internal/ocprune"
)

const ocExport = `{"info":{"id":"ses_cli1"},"messages":[
{"type":"user","id":"msg_old","text":"before compaction"},
{"type":"compaction","id":"msg_cmp","status":"completed","summary":"s"},
{"type":"system","id":"msg_sys","text":"Instructions updated"},
{"type":"user","id":"msg_a_u","text":"TOPIC-ALPHA"},
{"type":"assistant","id":"msg_a_a","content":[{"type":"tool","id":"call_1","name":"read","state":{}}]},
{"type":"user","id":"msg_b_u","text":"TOPIC-BETA"},
{"type":"assistant","id":"msg_b_a","content":[{"type":"text","text":"ok"}]}
]}`

const ocResponse = `{"categories":[
{"label":"alpha","ids":["msg_a_u","msg_a_a","msg_old"]},
{"label":"beta","ids":["msg_b_u","msg_b_a"]},
{"label":"system","ids":["msg_sys"]}]}`

// ocSetup writes the export and a stub categorizer response, and points the
// state directory at a temp dir.
func ocSetup(t *testing.T) (export, categorizer, stateDir string) {
	t.Helper()
	dir := t.TempDir()
	export = filepath.Join(dir, "export.json")
	resp := filepath.Join(dir, "response.json")
	if err := os.WriteFile(export, []byte(ocExport), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resp, []byte(ocResponse), 0o644); err != nil {
		t.Fatal(err)
	}
	stateDir = filepath.Join(dir, "state")
	t.Setenv("CTXED_STATE_DIR", stateDir)
	t.Setenv("OPENCODE_SESSION_ID", "")
	t.Setenv("CTXED_CATEGORIZER_CMD", "")
	return export, "cat " + resp, stateDir
}

func ocListing(t *testing.T, out string) []ocprune.Category {
	t.Helper()
	var l []ocprune.Category
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		t.Fatalf("listing is not JSON: %v\n%s", err, out)
	}
	return l
}

func TestOpenCodeCategorizeListsOnlyLiveContext(t *testing.T) {
	export, cat, _ := ocSetup(t)
	code, out, errOut := run("opencode", "categorize", "--session", "ses_cli1", "--export", export, "--categorizer-cmd", cat, "--json")
	if code != cli.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	l := ocListing(t, out)
	if len(l) != 3 {
		t.Fatalf("want 3 categories, got %+v", l)
	}
	for _, c := range l {
		for _, id := range c.EntryIDs {
			if id == "msg_old" {
				t.Fatalf("pre-compaction entry listed: %+v", c)
			}
		}
	}
}

func TestOpenCodeSecondPruneSkipsDropped(t *testing.T) {
	export, cat, stateDir := ocSetup(t)
	t.Setenv("OPENCODE_SESSION_ID", "ses_cli1") // the session comes from OpenCode's env
	if code, _, e := run("opencode", "categorize", "--export", export, "--categorizer-cmd", cat); code != cli.ExitOK {
		t.Fatalf("categorize: %s", e)
	}
	code, out, e := run("opencode", "drop", "1")
	if code != cli.ExitOK || !strings.Contains(out, `"alpha"`) {
		t.Fatalf("drop exit %d: %s %s", code, out, e)
	}

	code, out, e = run("opencode", "categorize", "--export", export, "--categorizer-cmd", cat, "--json")
	if code != cli.ExitOK {
		t.Fatalf("second categorize: %s", e)
	}
	for _, c := range ocListing(t, out) {
		for _, id := range c.EntryIDs {
			if id == "msg_a_u" || id == "msg_a_a" {
				t.Fatalf("dropped entry listed again: %+v", c)
			}
		}
	}

	// Pick from the second listing; the first prune's ids must stay dropped.
	if code, _, e := run("opencode", "drop", "2"); code != cli.ExitOK {
		t.Fatalf("second drop: %s", e)
	}
	st, err := ocprune.Load(stateDir, "ses_cli1")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(st.Dropped.IDs, ",")
	for _, id := range []string{"msg_a_u", "msg_a_a", "msg_sys"} {
		if !strings.Contains(got, id) {
			t.Fatalf("drop list %s lacks %s", got, id)
		}
	}
	if len(st.Dropped.ToolCallIDs) != 1 || st.Dropped.ToolCallIDs[0] != "call_1" {
		t.Fatalf("tool-call ids %v", st.Dropped.ToolCallIDs)
	}
}

func TestOpenCodeDropUnknownNumberRefused(t *testing.T) {
	export, cat, stateDir := ocSetup(t)
	run("opencode", "categorize", "--session", "ses_cli1", "--export", export, "--categorizer-cmd", cat)
	code, _, _ := run("opencode", "drop", "--session", "ses_cli1", "1,9")
	if code != cli.ExitRefused {
		t.Fatalf("exit %d, want %d", code, cli.ExitRefused)
	}
	st, _ := ocprune.Load(stateDir, "ses_cli1")
	if len(st.Dropped.IDs) != 0 {
		t.Fatalf("drop list changed: %v", st.Dropped.IDs)
	}
}

func TestOpenCodeDropBeforeCategorizeRefused(t *testing.T) {
	ocSetup(t)
	if code, _, _ := run("opencode", "drop", "--session", "ses_cli1", "1"); code != cli.ExitRefused {
		t.Fatalf("exit %d, want %d", code, cli.ExitRefused)
	}
}

func TestOpenCodeBadSessionIsUsageError(t *testing.T) {
	_, _, stateDir := ocSetup(t)
	for _, args := range [][]string{
		{"opencode", "drop", "--session", "../x", "1"},
		{"opencode", "categorize", "--session", "nope"},
		{"opencode", "drop", "1"}, // no session at all
	} {
		if code, _, _ := run(args...); code != cli.ExitUsage {
			t.Fatalf("%v: exit %d, want %d", args, code, cli.ExitUsage)
		}
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal("a refused command created the state dir")
	}
}

func TestOpenCodeNothingToPrune(t *testing.T) {
	_, _, _ = ocSetup(t)
	dir := t.TempDir()
	export := filepath.Join(dir, "small.json")
	os.WriteFile(export, []byte(`{"info":{},"messages":[{"type":"user","id":"m1","text":"hi"}]}`), 0o644)
	// A categorizer that fails proves no model is called.
	code, out, e := run("opencode", "categorize", "--session", "ses_cli1", "--export", export, "--categorizer-cmd", "false")
	if code != cli.ExitOK || !strings.Contains(out, "Nothing to prune") {
		t.Fatalf("exit %d: %s %s", code, out, e)
	}
}
