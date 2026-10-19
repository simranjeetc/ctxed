package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simranjeetc/ctxed/internal/cli"
)

// clearModelTestEnv removes every model override and points config lookups at an
// empty temporary home, so a resolution can only succeed from the input under
// test.
func clearModelTestEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	t.Setenv("CTXED_CATEGORIZER_CMD", "")
	t.Setenv("CTXED_CATEGORIZER_MODEL", "")
	t.Setenv("CTXED_MODEL", "")
}

// openCodeSession writes an OpenCode export carrying model (a "provider/id"
// reference) on its info object. An empty model leaves the field out.
func openCodeSession(t *testing.T, model string) string {
	t.Helper()
	info := map[string]any{"id": "ses_test"}
	if model != "" {
		provider, id, _ := strings.Cut(model, "/")
		info["model"] = map[string]any{"id": id, "providerID": provider, "variant": "default"}
	}
	doc := map[string]any{
		"info": info,
		"messages": []map[string]any{
			{"type": "user", "id": "m1", "text": "first question"},
			{"type": "assistant", "id": "m2", "content": []map[string]any{{"type": "text", "text": "first answer"}}},
			{"type": "user", "id": "m3", "text": "second question"},
		},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeOpenCode writes an `opencode` that records the --model it was handed and
// answers with the given categories JSON, so a test can see which model the
// overview actually used.
func fakeOpenCode(t *testing.T, answer string) (bin, logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "model.log")
	text, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(dir, "events.jsonl")
	stream := `{"type":"step_start","sessionID":"ses_fake00000001"}` + "\n" +
		`{"type":"text","sessionID":"ses_fake00000001","part":{"type":"text","text":` + string(text) + "}}\n"
	if err := os.WriteFile(events, []byte(stream), 0o644); err != nil {
		t.Fatal(err)
	}
	bin = filepath.Join(dir, "opencode")
	body := "#!/bin/sh\n" +
		"prev=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--model\" ]; then printf '%s' \"$a\" > '" + logPath + "'; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"case \"$1\" in\n" +
		"  run) cat >/dev/null; cat '" + events + "' ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, logPath
}

func usedModel(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("the harness was never called with --model: %v", err)
	}
	return string(data)
}

const answerOneTopic = `{"categories":[{"label":"Session topic","ids":["m1","m2","m3"]}]}`

// overviewFor resolves and prints the overview for session, returning the JSON.
func overviewFor(t *testing.T, session string, args ...string) (overviewJSON, string) {
	t.Helper()
	code, out, stderr := run(append([]string{"overview", session, "--json"}, args...)...)
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	var r overviewJSON
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	return r, out
}

func hasTopic(r overviewJSON, label string) bool {
	for _, row := range r.Rows {
		if row.Kind == "topic" && row.Label == label {
			return true
		}
	}
	return false
}

// Rung 4: with no override at all, the model recorded on the session is used.
func TestOverviewUsesSessionModel(t *testing.T) {
	clearModelTestEnv(t)
	bin, logPath := fakeOpenCode(t, answerOneTopic)
	t.Setenv("CTXED_OPENCODE_BIN", bin)
	session := openCodeSession(t, "prov/session-model")

	r, out := overviewFor(t, session)
	if got := usedModel(t, logPath); got != "prov/session-model" {
		t.Fatalf("used model %q, want the session's model", got)
	}
	if !hasTopic(r, "Session topic") {
		t.Fatalf("no topics came back:\n%s", out)
	}
	if r.Tokens <= 0 {
		t.Fatalf("token sizes missing: %s", out)
	}
}

// Rung 1: --model wins over the environment and the session.
func TestOverviewModelFlagWins(t *testing.T) {
	clearModelTestEnv(t)
	bin, logPath := fakeOpenCode(t, answerOneTopic)
	t.Setenv("CTXED_OPENCODE_BIN", bin)
	t.Setenv("CTXED_CATEGORIZER_MODEL", "cat-env")
	t.Setenv("CTXED_MODEL", "model-env")
	session := openCodeSession(t, "prov/session-model")

	r, _ := overviewFor(t, session, "--model", "flag/model")
	if got := usedModel(t, logPath); got != "flag/model" {
		t.Fatalf("used model %q, want --model", got)
	}
	if !hasTopic(r, "Session topic") {
		t.Fatal("no topics came back")
	}
}

// Rung 2: $CTXED_CATEGORIZER_MODEL wins over $CTXED_MODEL and the session.
func TestOverviewCategorizerModelEnvWins(t *testing.T) {
	clearModelTestEnv(t)
	bin, logPath := fakeOpenCode(t, answerOneTopic)
	t.Setenv("CTXED_OPENCODE_BIN", bin)
	t.Setenv("CTXED_CATEGORIZER_MODEL", "cat/model")
	t.Setenv("CTXED_MODEL", "model-env")
	session := openCodeSession(t, "prov/session-model")

	overviewFor(t, session)
	if got := usedModel(t, logPath); got != "cat/model" {
		t.Fatalf("used model %q, want $CTXED_CATEGORIZER_MODEL", got)
	}
}

// Rung 3: $CTXED_MODEL wins over the session model.
func TestOverviewModelEnvWins(t *testing.T) {
	clearModelTestEnv(t)
	bin, logPath := fakeOpenCode(t, answerOneTopic)
	t.Setenv("CTXED_OPENCODE_BIN", bin)
	t.Setenv("CTXED_MODEL", "model/env")
	session := openCodeSession(t, "prov/session-model")

	overviewFor(t, session)
	if got := usedModel(t, logPath); got != "model/env" {
		t.Fatalf("used model %q, want $CTXED_MODEL", got)
	}
}

// Rung 5: a session with no model falls back to OpenCode's configured default.
func TestOverviewFallsBackToConfiguredDefault(t *testing.T) {
	clearModelTestEnv(t)
	bin, logPath := fakeOpenCode(t, answerOneTopic)
	t.Setenv("CTXED_OPENCODE_BIN", bin)
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{"model":"configured/default"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	session := openCodeSession(t, "")

	overviewFor(t, session)
	if got := usedModel(t, logPath); got != "configured/default" {
		t.Fatalf("used model %q, want the configured default", got)
	}
}

// A bad override fails loudly: the model and the harness's stderr, not a bare
// "topics unavailable", while the token sizes still print.
func TestOverviewBadOverrideNamesModelAndKeepsSizes(t *testing.T) {
	clearModelTestEnv(t)
	bin := filepath.Join(t.TempDir(), "opencode")
	body := "#!/bin/sh\ncat >/dev/null\necho 'Model unavailable: flag/model' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CTXED_OPENCODE_BIN", bin)
	session := openCodeSession(t, "prov/session-model")

	code, out, stderr := run("overview", session, "--model", "flag/model")
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "topics unavailable (model flag/model:") {
		t.Fatalf("stderr does not name the model: %q", stderr)
	}
	if !strings.Contains(stderr, "Model unavailable: flag/model") {
		t.Fatalf("stderr swallowed the harness error: %q", stderr)
	}
	if !strings.Contains(out, "Whole session") {
		t.Fatalf("token sizes did not print:\n%s", out)
	}
}

// With no source at all, the failure names every source it tried.
func TestOverviewNoModelSourceNamesThemAll(t *testing.T) {
	clearModelTestEnv(t)
	t.Setenv("CTXED_OPENCODE_BIN", "true")
	session := openCodeSession(t, "")

	code, out, stderr := run("overview", session)
	if code != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	for _, want := range []string{"topics unavailable", "--model", "CTXED_CATEGORIZER_MODEL", "CTXED_MODEL", "session", "opencode.json"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr %q does not mention %q", stderr, want)
		}
	}
	if !strings.Contains(out, "Whole session") {
		t.Fatalf("token sizes did not print:\n%s", out)
	}
}

// Rung 4 for Claude Code: the model recorded on the transcript's latest
// assistant line is used when nothing overrides it.
func TestOverviewUsesClaudeSessionModel(t *testing.T) {
	clearModelTestEnv(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "model.log")
	bin := filepath.Join(dir, "claude")
	body := "#!/bin/sh\n" +
		"prev=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--model\" ]; then printf '%s' \"$a\" > '" + logPath + "'; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"cat >/dev/null\n" +
		"printf '%s' '" + `{"categories":[{"label":"Session topic","ids":["u1","a1"]}]}` + "'\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CTXED_CLAUDE_BIN", bin)
	session := filepath.Join(dir, "transcript.jsonl")
	transcript := `{"type":"user","uuid":"u1","message":{"role":"user","content":"first question"}}` + "\n" +
		`{"type":"assistant","uuid":"a1","message":{"role":"assistant","model":"claude-session-model","content":[{"type":"text","text":"first answer"}]}}` + "\n"
	if err := os.WriteFile(session, []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}

	r, out := overviewFor(t, session)
	if got := usedModel(t, logPath); got != "claude-session-model" {
		t.Fatalf("used model %q, want the session's model", got)
	}
	if !hasTopic(r, "Session topic") {
		t.Fatalf("no topics came back:\n%s", out)
	}
}

// A categorizer command still wins outright: no model is needed, and the
// harness CLI is never invoked.
func TestOverviewCategorizerCmdWinsWithoutModel(t *testing.T) {
	clearModelTestEnv(t)
	t.Setenv("CTXED_OPENCODE_BIN", "/no/such/opencode")
	session := openCodeSession(t, "")

	r, _ := overviewFor(t, session, "--categorizer-cmd", stubCategorizer(t))
	if !hasTopic(r, "Finished topic") {
		t.Fatal("the categorizer command's topics did not come back")
	}
}
