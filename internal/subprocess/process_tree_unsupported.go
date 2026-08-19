//go:build !darwin && !linux

package subprocess

import (
	"fmt"
	"os/exec"
	"runtime"
)

func configureDedicatedProcessGroup(*exec.Cmd) error {
	return fmt.Errorf("%w on %s", errProcessTreeUnsupported, runtime.GOOS)
}

func terminateDedicatedProcessGroup(int, processTerminationOptions, bool, bool) (ProcessTermination, error) {
	return ProcessTermination{}, fmt.Errorf("%w on %s", errProcessTreeUnsupported, runtime.GOOS)
}
