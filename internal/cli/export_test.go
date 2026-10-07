//go:build parked

package cli

import "io"

// RunWithParked is RunWithStdin plus the parked pruning commands. It exists
// only in tests, so the parked code stays tested while the binary cannot
// reach it.
func RunWithParked(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "drop":
			return runDrop(args[1:], stdout, stderr)
		case "prune":
			return runPrune(args[1:], stdin, stdout, stderr)
		case "compact-instruction":
			return runCompactInstruction(args[1:], stdout, stderr)
		case "opencode":
			return runOpenCode(args[1:], stdout, stderr)
		}
	}
	return RunWithStdin(args, stdin, stdout, stderr)
}
