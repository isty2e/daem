package subprocess

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestBindRejectsCommandWithoutContext(t *testing.T) {
	_, err := BindProcessGroup(exec.Command("unused-daem-test-command"))
	if err == nil || !strings.Contains(err.Error(), "exec.CommandContext") {
		t.Fatalf("BindProcessGroup error = %v, want CommandContext requirement", err)
	}
}

func TestJoinCommandProcessGroupCleanupReportsUnsignalableWithoutDescendantLanguage(t *testing.T) {
	err := joinCommandProcessGroupCleanup(
		nil,
		ProcessTermination{processesFound: true, unsignalableOccupancy: true},
		errProcessGroupUnsignalable,
	)
	if err == nil || !errors.Is(err, errProcessGroupUnsignalable) {
		t.Fatalf("cleanup error = %v, want unsignalable occupancy", err)
	}
	message := err.Error()
	if strings.Contains(message, "descendant") || strings.Contains(message, "process tree") {
		t.Fatalf("cleanup error = %q, want no spawn-tree residual claim", message)
	}
	if !strings.Contains(message, "unsignalable") {
		t.Fatalf("cleanup error = %q, want unsignalable occupancy language", message)
	}
}
