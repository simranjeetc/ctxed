//go:build !parked

package cli

import "io"

// RunWithParked in the default build has no parked commands to dispatch.
func RunWithParked(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return RunWithStdin(args, stdin, stdout, stderr)
}
