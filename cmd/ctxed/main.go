// Command ctxed shows what an agent session's context is made of.
package main

import (
	"os"

	_ "github.com/simranjeetc/ctxed/internal/adapter/builtin"
	"github.com/simranjeetc/ctxed/internal/cli"
)

func main() {
	os.Exit(cli.RunWithStdin(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
