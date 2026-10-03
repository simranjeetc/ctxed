// Command ctxed inspects and edits an agent session's context window.
package main

import (
	"os"

	_ "github.com/simranjeetc/ctxed/internal/adapter/builtin"
	"github.com/simranjeetc/ctxed/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
