package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/simranjeetc/ctxed/internal/skill"
	"github.com/simranjeetc/ctxed/internal/update"
)

func runUpdate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	check := fs.Bool("check", false, "report whether a newer release exists, without installing")
	force := fs.Bool("force", false, "reinstall the latest release even if it is not newer")
	if err := fs.Parse(args); err != nil {
		return parseErrExit(err, stderr)
	}

	c := &update.Client{}
	if *check {
		rel, err := c.Latest(ctx)
		if err != nil {
			return fail(stderr, err)
		}
		if update.Compare(version, rel.Tag) < 0 {
			_, _ = fmt.Fprintf(stdout, "ctxed %s available (current %s)\n", strings.TrimPrefix(rel.Tag, "v"), version)
		} else {
			_, _ = fmt.Fprintf(stdout, "ctxed %s is up to date\n", version)
		}
		return ExitOK
	}

	self, err := os.Executable()
	if err != nil {
		return fail(stderr, fmt.Errorf("update: cannot locate the running binary: %w", err))
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}

	res, err := c.Update(ctx, version, runtime.GOOS, runtime.GOARCH, self, *force)
	if err != nil {
		return fail(stderr, err)
	}
	if !res.Changed {
		_, _ = fmt.Fprintf(stdout, "ctxed %s is already the latest release\n", version)
		return ExitOK
	}
	_, _ = fmt.Fprintf(stdout, "updated ctxed %s -> %s\n", res.From, res.To)
	if len(res.Skill) > 0 {
		if home, err := os.UserHomeDir(); err == nil {
			if _, err := skill.Install(home, res.Skill, stdout); err != nil {
				_, _ = fmt.Fprintf(stderr, "ctxed: could not refresh the skill: %v\n", err)
			}
		}
	}
	return ExitOK
}
