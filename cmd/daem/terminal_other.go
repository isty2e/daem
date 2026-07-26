//go:build !darwin && !linux

package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

func isTerminal(*os.File) bool {
	return false
}

func readTerminalConfirmationLine(context.Context, io.Reader, int) (string, error) {
	return "", fmt.Errorf("interactive terminal confirmation is unsupported on this platform")
}
