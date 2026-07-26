package testkit

import (
	"io"
	"slices"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	clipresent "github.com/isty2e/daem/internal/cli/present"
)

func RunCLI(args []string, stdout io.Writer, stderr io.Writer) int {
	return clipkg.RunWithOptions(args, clipkg.RunOptions{Stdout: stdout, Stderr: stderr})
}

// RunVerboseCLI preserves exhaustive human evidence assertions while default-output tests use RunCLI.
func RunVerboseCLI(args []string, stdout io.Writer, stderr io.Writer) int {
	return clipkg.RunWithOptions(verboseArgs(args), clipkg.RunOptions{Stdout: stdout, Stderr: stderr})
}

func RunCLIWithOptions(args []string, options clipkg.RunOptions) int {
	return clipkg.RunWithOptions(args, options)
}

// RunVerboseCLIWithOptions is RunVerboseCLI with explicit process-boundary options.
func RunVerboseCLIWithOptions(args []string, options clipkg.RunOptions) int {
	return clipkg.RunWithOptions(verboseArgs(args), options)
}

func ExpectedShellCommand(t testing.TB, argv ...string) string {
	t.Helper()
	command, err := clipresent.ShellCommand(argv...)
	if err != nil {
		t.Fatalf("ShellCommand(%q): %v", argv, err)
	}
	return command
}

func verboseArgs(args []string) []string {
	if len(args) == 0 || args[0] == "help" || slices.Contains(args, "--json") || slices.Contains(args, "--verbose") || slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		return args
	}
	result := append([]string(nil), args...)
	return append(result, "--verbose")
}
