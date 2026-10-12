package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunApplyRejectsYesWithDryRun(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"apply", "--dry-run", "--yes"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "--dry-run and --yes are mutually exclusive") {
		t.Fatalf("stderr = %q, want mutually exclusive diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyRequiresYesForNonInteractiveMutatingApply(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"apply"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "non-interactive apply requires --yes") {
		t.Fatalf("stderr = %q, want --yes diagnostic", stderr.String())
	}
	if !strings.Contains(stderr.String(), "daem apply --dry-run") {
		t.Fatalf("stderr = %q, want dry-run hint", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyDelegatedRoutesRequireYesForNonInteractiveApply(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"apply"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "non-interactive apply requires --yes") {
		t.Fatalf("stderr = %q, want --yes diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyJSONRequiresDryRunOrYes(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"apply", "--json"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "--json requires --dry-run or --yes") {
		t.Fatalf("stderr = %q, want json mode diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyRejectsUnknownFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"apply", "--unknown", "--yes"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -unknown") {
		t.Fatalf("stderr = %q, want unknown flag diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyRejectsRemovedAdoptExistingFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"apply", "--adopt-existing", "--dry-run"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -adopt-existing") {
		t.Fatalf("stderr = %q, want removed flag diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunApplyKeepsDryRunYesRejection(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"apply", "--dry-run", "--yes"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "--dry-run and --yes are mutually exclusive") {
		t.Fatalf("stderr = %q, want mutually exclusive diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no runtime/readiness wording", stdout.String())
	}
}
