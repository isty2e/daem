package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestRunRecoverRejectsTamperedJournalStatefileBefore(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, nextState, _, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "new instructions\n")
	journalPath := filepath.Join(paths.RecoveryDir, "20260621T120000.000000000Z-apply", "journal.json")
	journal := loadCLIRecoveryJournalForTest(t, journalPath)
	var statefileBefore map[string]any
	if err := json.Unmarshal(journal.StatefileBefore, &statefileBefore); err != nil {
		t.Fatalf("Unmarshal statefile_before returned error: %v", err)
	}
	managedPaths, ok := statefileBefore["managed_paths"].([]any)
	if !ok || len(managedPaths) != 1 {
		t.Fatalf("managed_paths = %#v, want one", statefileBefore["managed_paths"])
	}
	managedPath, ok := managedPaths[0].(map[string]any)
	if !ok {
		t.Fatalf("managed_paths[0] = %#v, want object", managedPaths[0])
	}
	managedPath["content_hash"] = string(singleCLIManagedPath(t, nextState).ContentHash())
	encodedBefore, err := json.Marshal(statefileBefore)
	if err != nil {
		t.Fatalf("Marshal statefile_before returned error: %v", err)
	}
	journal.StatefileBefore = encodedBefore
	content, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent returned error: %v", err)
	}
	if err := os.WriteFile(journalPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile journal returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"recover failed:",
		"statefile_before does not match",
		"expected state hash",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(tempDir, "AGENTS.md"), "new instructions\n")
	assertCLIRecoveryJournalActive(t, paths.RecoveryDir)
}

func TestRunApplyAndStatusBlockOnActiveRecoveryJournal(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	captureCLIRecoveryUpdateJournal(t, manifestPath)

	for _, command := range []string{"apply", "status"} {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			args := []string{command, "--manifest", manifestPath}
			if command == "apply" {
				args = append(args, "--dry-run")
			}
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), "interrupted apply operation found") ||
				!strings.Contains(stderr.String(), "daem recover --dry-run") {
				t.Fatalf("stderr = %q, want active recovery diagnostic", stderr.String())
			}
		})
	}
}

func TestRunRecoverRejectsMissingAndMultipleActiveJournals(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		tempDir := t.TempDir()
		manifestPath := filepath.Join(tempDir, "daem.toml")
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
		if exitCode != 1 {
			t.Fatalf("exitCode = %d, want 1", exitCode)
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want empty", stdout.String())
		}
		if !strings.Contains(stderr.String(), "no active recovery journal") {
			t.Fatalf("stderr = %q, want no active recovery diagnostic", stderr.String())
		}
	})

	t.Run("additional malformed operation", func(t *testing.T) {
		tempDir := t.TempDir()
		manifestPath := filepath.Join(tempDir, "daem.toml")
		paths, _, _, _, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
		secondOperation := filepath.Join(paths.RecoveryDir, "20260621T120001.000000000Z-apply")
		if err := os.MkdirAll(secondOperation, 0o700); err != nil {
			t.Fatalf("MkdirAll second recovery returned error: %v", err)
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
		if exitCode != 1 {
			t.Fatalf("exitCode = %d, want 1", exitCode)
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want empty", stdout.String())
		}
		if !strings.Contains(stderr.String(), "recovery inventory is blocked") ||
			!strings.Contains(stderr.String(), "has no journal.json") {
			t.Fatalf("stderr = %q, want malformed recovery diagnostic", stderr.String())
		}
	})
}
