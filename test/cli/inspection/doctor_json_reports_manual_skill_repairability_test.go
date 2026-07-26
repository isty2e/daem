package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/doctorenv"
)

func TestRunDoctorJSONReportsManualSkillRepairabilityFields(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
`)
	t.Setenv("HOME", homeDir)
	doctorenv.WithFakeGit(t, "git version test")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"doctor", "--manifest", manifestPath, "--target", "opencode", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}

	payload := decodeDoctorJSONTestPayload(t, stdout.Bytes())
	check := findDoctorJSONCheck(t, payload, "error", "target=opencode skill=oracle compatibility")
	if check.Repairability != "manual" {
		t.Fatalf("check = %#v, want manual repairability", check)
	}
	if strings.Contains(check.NextStep, "compat_repair") {
		t.Fatalf("check = %#v, want no compat_repair next step for manual case", check)
	}
	if len(check.ManualReasons) == 0 || !strings.Contains(strings.Join(check.ManualReasons, "; "), "description is required") {
		t.Fatalf("check = %#v, want description manual reason", check)
	}
}
