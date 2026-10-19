package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateConfig points every config lookup at a temporary home so the machine's
// real opencode.json / settings.json can never leak into a resolution.
func isolateConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude-config"))
	t.Setenv("CTXED_CATEGORIZER_MODEL", "")
	t.Setenv("CTXED_MODEL", "")
	return home
}

func writeJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantResolved(t *testing.T, got ModelResolution, err error, model string, source ModelSource) {
	t.Helper()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Model != model || got.Source != source {
		t.Fatalf("got {%q, %q}, want {%q, %q}", got.Model, got.Source, model, source)
	}
}

// Rung 1: --model beats every other source.
func TestResolveModelFlagWins(t *testing.T) {
	isolateConfig(t)
	writeJSON(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode", "opencode.json"), `{"model":"config-model"}`)
	t.Setenv("CTXED_CATEGORIZER_MODEL", "categorizer-env")
	t.Setenv("CTXED_MODEL", "model-env")
	wantResolved(t, mustResolve(t, "flag-model", "session-model", "opencode"), nil, "flag-model", SourceFlag)
}

// Rung 2: $CTXED_CATEGORIZER_MODEL beats $CTXED_MODEL, the session, and config.
func TestResolveModelCategorizerEnvWins(t *testing.T) {
	isolateConfig(t)
	writeJSON(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode", "opencode.json"), `{"model":"config-model"}`)
	t.Setenv("CTXED_CATEGORIZER_MODEL", "categorizer-env")
	t.Setenv("CTXED_MODEL", "model-env")
	wantResolved(t, mustResolve(t, "", "session-model", "opencode"), nil, "categorizer-env", SourceCategorizerEnv)
}

// Rung 3: $CTXED_MODEL beats the session model and config.
func TestResolveModelModelEnvWins(t *testing.T) {
	isolateConfig(t)
	writeJSON(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode", "opencode.json"), `{"model":"config-model"}`)
	t.Setenv("CTXED_MODEL", "model-env")
	wantResolved(t, mustResolve(t, "", "session-model", "opencode"), nil, "model-env", SourceModelEnv)
}

// Rung 4: the session's own recorded model beats the harness config default.
func TestResolveModelSessionWins(t *testing.T) {
	isolateConfig(t)
	writeJSON(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode", "opencode.json"), `{"model":"config-model"}`)
	wantResolved(t, mustResolve(t, "", "session-model", "opencode"), nil, "session-model", SourceSession)
}

// Rung 5: with no override and no session model, OpenCode's configured default.
func TestResolveModelOpenCodeConfigWins(t *testing.T) {
	isolateConfig(t)
	writeJSON(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode", "opencode.json"), `{"model":"config-model"}`)
	wantResolved(t, mustResolve(t, "", "", "opencode"), nil, "config-model", SourceHarnessConfig)
}

// Rung 5 also works for Claude Code, from its own settings.json.
func TestResolveModelClaudeConfigWins(t *testing.T) {
	isolateConfig(t)
	writeJSON(t, filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json"), `{"model":"claude-sonnet"}`)
	wantResolved(t, mustResolve(t, "", "", "claude-code"), nil, "claude-sonnet", SourceHarnessConfig)
}

// Rung 6: nothing resolves; the error names every source, and no literal model
// is ever returned.
func TestResolveModelNoSource(t *testing.T) {
	isolateConfig(t)
	got, err := ResolveCategorizerModel("", "", "opencode")
	if err == nil {
		t.Fatalf("want an error, got model %q", got.Model)
	}
	if got.Model != "" {
		t.Fatalf("want no model, got %q", got.Model)
	}
	for _, want := range []string{"--model", "CTXED_CATEGORIZER_MODEL", "CTXED_MODEL", "session", "opencode.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
}

// Whitespace-only values do not count as a source.
func TestResolveModelIgnoresBlankValues(t *testing.T) {
	isolateConfig(t)
	t.Setenv("XDG_CONFIG_HOME", "") // fall back to $HOME/.config
	writeJSON(t, filepath.Join(os.Getenv("HOME"), ".config", "opencode", "opencode.json"), `{"model":"config-model"}`)
	t.Setenv("CTXED_CATEGORIZER_MODEL", "   ")
	t.Setenv("CTXED_MODEL", "\t")
	wantResolved(t, mustResolve(t, "  ", "  ", "opencode"), nil, "config-model", SourceHarnessConfig)
}

// mustResolve adapts the two-value call into the wantResolved shape.
func mustResolve(t *testing.T, flag, session, source string) ModelResolution {
	t.Helper()
	res, err := ResolveCategorizerModel(flag, session, source)
	if err != nil {
		t.Fatalf("resolve(%q, %q, %q): %v", flag, session, source, err)
	}
	return res
}
