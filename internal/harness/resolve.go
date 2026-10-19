package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModelSource names where a resolved categorizer model came from.
type ModelSource string

// The model sources, in the order they are tried.
const (
	SourceFlag           ModelSource = "--model"
	SourceCategorizerEnv ModelSource = "CTXED_CATEGORIZER_MODEL"
	SourceModelEnv       ModelSource = "CTXED_MODEL"
	SourceSession        ModelSource = "session"
	SourceHarnessConfig  ModelSource = "harness config"
)

// ModelResolution is a resolved categorizer model and where it came from.
type ModelResolution struct {
	Model  string
	Source ModelSource
}

// ResolveCategorizerModel picks the model that names topics, first hit wins:
//
//  1. flagModel (--model on the command);
//  2. $CTXED_CATEGORIZER_MODEL;
//  3. $CTXED_MODEL;
//  4. sessionModel — the model recorded on the session being inspected;
//  5. the harness's own configured default model (OpenCode:
//     ~/.config/opencode/opencode.json "model"; Claude Code:
//     ~/.claude/settings.json "model").
//
// source is the session's adapter name ("opencode" or "claude-code"); it selects
// the config file for the last rung. When nothing resolves it returns an error
// naming every source it tried, and never falls back to a literal
// provider/model string.
func ResolveCategorizerModel(flagModel, sessionModel, source string) (ModelResolution, error) {
	if m := strings.TrimSpace(flagModel); m != "" {
		return ModelResolution{Model: m, Source: SourceFlag}, nil
	}
	if m := strings.TrimSpace(os.Getenv("CTXED_CATEGORIZER_MODEL")); m != "" {
		return ModelResolution{Model: m, Source: SourceCategorizerEnv}, nil
	}
	if m := strings.TrimSpace(os.Getenv("CTXED_MODEL")); m != "" {
		return ModelResolution{Model: m, Source: SourceModelEnv}, nil
	}
	if m := strings.TrimSpace(sessionModel); m != "" {
		return ModelResolution{Model: m, Source: SourceSession}, nil
	}
	if m := configDefaultModel(source); m != "" {
		return ModelResolution{Model: m, Source: SourceHarnessConfig}, nil
	}
	return ModelResolution{}, fmt.Errorf(
		"no categorizer model: tried --model, $CTXED_CATEGORIZER_MODEL, $CTXED_MODEL, the session's own model, and %s",
		configPath(source))
}

// configDefaultModel reads the default model out of the harness's own
// configuration: OpenCode's opencode.json "model", or Claude Code's
// settings.json "model". It returns "" when the file, the key, or the value is
// missing, so the caller can keep walking the resolution order.
func configDefaultModel(source string) string {
	path := configPath(source)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var top struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(data, &top) != nil {
		return ""
	}
	return strings.TrimSpace(top.Model)
}

// configPath is the configuration file rung 5 of the resolution reads: OpenCode
// uses $XDG_CONFIG_HOME/opencode/opencode.json (default ~/.config/opencode),
// Claude Code uses $CLAUDE_CONFIG_DIR/settings.json (default ~/.claude).
func configPath(source string) string {
	switch source {
	case "opencode":
		if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
			return filepath.Join(dir, "opencode", "opencode.json")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "~/.config/opencode/opencode.json"
		}
		return filepath.Join(home, ".config", "opencode", "opencode.json")
	case "claude-code":
		if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
			return filepath.Join(dir, "settings.json")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "~/.claude/settings.json"
		}
		return filepath.Join(home, ".claude", "settings.json")
	}
	return "the harness's configured default model"
}
