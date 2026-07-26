package subprocess

import (
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
