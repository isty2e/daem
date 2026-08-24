//go:build !darwin && !linux

package probe

import "fmt"

func validateProjectWorkingDirectoryPlatform() error {
	return fmt.Errorf("descriptor-backed working directories are unsupported on this platform")
}
