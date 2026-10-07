package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ClaudeModel is the categorizer model when none is configured.
const ClaudeModel = "haiku"

var claudeSessionRe = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

// ErrBadClaudeSession reports a session id that cannot safely name a file.
var ErrBadClaudeSession = errors.New("claude code session id must be a UUID")

// ValidClaudeSession checks a Claude Code session id before it names a file.
func ValidClaudeSession(id string) error {
	if !claudeSessionRe.MatchString(id) {
		return fmt.Errorf("%w (got %q)", ErrBadClaudeSession, id)
	}
	return nil
}

// ClaudeTranscript finds <id>.jsonl under the projects directory of
// $CLAUDE_CONFIG_DIR (default ~/.claude). It looks in the folder for cwd
// first, then in every project folder, so a renamed or differently encoded
// working directory still resolves.
func ClaudeTranscript(id, cwd string) (string, error) {
	if err := ValidClaudeSession(id); err != nil {
		return "", err
	}
	root := os.Getenv("CLAUDE_CONFIG_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".claude")
	}
	projects := filepath.Join(root, "projects")
	name := id + ".jsonl"
	if cwd != "" {
		p := filepath.Join(projects, claudeProjectDir(cwd), name)
		if isFile(p) {
			return p, nil
		}
	}
	matches, _ := filepath.Glob(filepath.Join(projects, "*", name))
	for _, m := range matches {
		if isFile(m) {
			return m, nil
		}
	}
	return "", fmt.Errorf("no transcript %s under %s", name, projects)
}

// claudeProjectDir is Claude Code's folder name for a working directory:
// every character other than a letter or digit becomes "-".
func claudeProjectDir(cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// ClaudePrint categorizes through `claude -p`: no tools, nothing saved, the
// prompt on stdin.
type ClaudePrint struct{ Model string }

func (c ClaudePrint) Complete(ctx context.Context, prompt string) (string, error) {
	bin := os.Getenv("CTXED_CLAUDE_BIN")
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("claude"); err != nil {
			return "", fmt.Errorf("claude binary not found; set CTXED_CLAUDE_BIN")
		}
	}
	model := c.Model
	if model == "" {
		model = ClaudeModel
	}
	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "-p", "--model", model, "--no-session-persistence", "--tools", "")
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude -p (%s): %v: %s", model, err, strings.TrimSpace(stderr.String()))
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", fmt.Errorf("claude -p (%s) returned no text", model)
	}
	return out.String(), nil
}
