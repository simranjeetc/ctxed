// Package skill installs the embedded ctxed-overview skill into the agent
// harnesses present on this machine (Claude Code and OpenCode).
package skill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// harnesses are the harnesses the skill can be installed into. root is the
// directory whose presence means the harness is installed; dir is the skill
// directory, relative to the user's home.
var harnesses = []struct {
	name string
	root string
	dir  string
}{
	{name: "Claude Code", root: ".claude", dir: ".claude/skills/ctxed-overview"},
	{name: "OpenCode", root: ".config/opencode", dir: ".config/opencode/skills/ctxed-overview"},
}

// Install writes data as SKILL.md into every installed harness and returns the
// harness names it wrote to. It writes nothing when no harness is found.
func Install(home string, data []byte, w io.Writer) ([]string, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("skill: no skill content to install")
	}
	var written []string
	for _, h := range harnesses {
		if fi, err := os.Stat(filepath.Join(home, h.root)); err != nil || !fi.IsDir() {
			continue
		}
		dst := filepath.Join(home, h.dir, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return written, fmt.Errorf("skill: %w", err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return written, fmt.Errorf("skill: %w", err)
		}
		if w != nil {
			_, _ = fmt.Fprintf(w, "installed %s skill: %s\n", h.name, dst)
		}
		written = append(written, h.name)
	}
	return written, nil
}
