//go:build !darwin && !linux

package subprocess

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

func TestBindRejectsUnsupportedPlatformBeforeProcessStart(t *testing.T) {
	command := exec.CommandContext(context.Background(), "unused-daem-test-command")
	_, err := BindProcessGroup(command)
	if !errors.Is(err, errProcessTreeUnsupported) {
		t.Fatalf("BindProcessGroup error = %v, want unsupported-platform classification", err)
	}
	if command.Process != nil {
		t.Fatalf("command process = %#v, want no process launch", command.Process)
	}
}
