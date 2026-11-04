//go:build !darwin && !linux

package workdir

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// ValidateCapability fails closed where descriptor-backed cwd
// launch has no admitted platform implementation.
func ValidateCapability() error {
	return fmt.Errorf("descriptor-backed working directories are unsupported on this platform")
}

// Validate fails closed where descriptor-backed cwd launch has no
// admitted platform implementation.
func Validate(_ *os.File) error {
	return ValidateCapability()
}

// PrepareCommand fails closed where descriptor-backed cwd launch has no admitted
// platform implementation.
func PrepareCommand(
	_ context.Context,
	_ string,
	_ []string,
	_ []string,
	_ *os.File,
) (*exec.Cmd, error) {
	return nil, fmt.Errorf("descriptor-backed working directories are unsupported on this platform")
}
