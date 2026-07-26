package main

import (
	"context"
	"os"

	"github.com/isty2e/daem/internal/cli"
)

func main() {
	os.Exit(runWithSignalLifecycle(func(ctx context.Context) int {
		return cli.RunWithOptions(os.Args[1:], cli.RunOptions{
			Context:              ctx,
			Stdin:                os.Stdin,
			Stdout:               os.Stdout,
			Stderr:               os.Stderr,
			StdinIsTerminal:      isTerminal(os.Stdin),
			StdoutIsTerminal:     isTerminal(os.Stdout),
			StderrIsTerminal:     isTerminal(os.Stderr),
			ReadConfirmationLine: readTerminalConfirmationLine,
		})
	}))
}
