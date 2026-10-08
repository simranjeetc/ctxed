package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/simranjeetc/ctxed"
	"github.com/simranjeetc/ctxed/internal/skill"
)

func runSkill(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "install" {
		_, _ = fmt.Fprintln(stderr, "ctxed skill: expected `install`")
		return ExitUsage
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(stderr, err)
	}
	written, err := skill.Install(home, ctxed.OverviewSkill, stdout)
	if err != nil {
		return fail(stderr, err)
	}
	if len(written) == 0 {
		_, _ = fmt.Fprintln(stdout, "no Claude Code or OpenCode install found; nothing to install")
	}
	return ExitOK
}
