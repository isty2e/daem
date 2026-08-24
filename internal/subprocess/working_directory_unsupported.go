//go:build !darwin && !linux

package subprocess

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// ValidateWorkingDirectoryCapability fails closed where descriptor-backed cwd
// launch has no admitted platform implementation.
func ValidateWorkingDirectoryCapability() error {
	return fmt.Errorf("descriptor-backed working directories are unsupported on this platform")
}

// ValidateWorkingDirectory fails closed where descriptor-backed cwd launch has no
// admitted platform implementation.
func ValidateWorkingDirectory(_ *os.File) error {
	return ValidateWorkingDirectoryCapability()
}

// PrepareCommandInWorkingDirectory fails closed where descriptor-backed cwd launch has no admitted
// platform implementation.
func PrepareCommandInWorkingDirectory(
	_ context.Context,
	_ string,
	_ []string,
	_ []string,
	_ *os.File,
) (*exec.Cmd, error) {
	return nil, fmt.Errorf("descriptor-backed working directories are unsupported on this platform")
}
