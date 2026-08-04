package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/test/testkit"
)

type recoverJSONTestPayload struct {
	SchemaVersion  int    `json:"schema_version"`
	Command        string `json:"command"`
	Mode           string `json:"mode"`
	AuthorityKind  string `json:"authority_kind"`
	OperationID    string `json:"operation_id"`
	OperationDir   string `json:"operation_dir"`
	Classification string `json:"classification"`
	ActionCount    int    `json:"action_count"`
	HasErrors      bool   `json:"has_errors"`
	Actions        []struct {
		Kind    string `json:"kind"`
		Reason  string `json:"reason"`
		Subject struct {
			Kind      string `json:"kind"`
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"subject"`
		ResourceID string `json:"resource_id"`
		Resource   struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"resource"`
		Target      string   `json:"target"`
		Targets     []string `json:"targets"`
		Scope       string   `json:"scope"`
		Destination string   `json:"destination"`
		ContentKind string   `json:"content_kind"`
		BackupPath  string   `json:"backup_path"`
		BackupHash  string   `json:"backup_hash"`
		BackupKind  string   `json:"backup_kind"`
		Detail      string   `json:"detail"`
	} `json:"actions"`
}

func TestRunRecoverDryRunJSONPreservesManagedSkillSubjectAndConsumers(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, _, _, _ := captureCLIRecoverySkillUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, tempDir, ".agents/skills/oracle/SKILL.md", "---\nname: oracle\nversion: new\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("recover exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	payload := decodeRecoverJSONTestPayload(t, stdout.Bytes())
	if payload.SchemaVersion != contractversion.RecoveryJSON ||
		payload.AuthorityKind != "active_journal" ||
		len(payload.Actions) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	action := payload.Actions[0]
	if action.ResourceID != "skill/oracle" || action.Resource.Kind != "skill" || action.Resource.Name != "oracle" ||
		action.Subject.Kind != "projection" || action.Subject.Namespace == "" || action.Subject.Name == "" ||
		action.Target != "" || len(action.Targets) != 1 || action.Targets[0] != "codex" ||
		action.ContentKind != "directory" {
		t.Fatalf("managed Skill recovery action = %#v", action)
	}
}

func TestRunRecoverDryRunJSONReportsRollbackPlan(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, _, _, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "new instructions\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "recover:") {
		t.Fatalf("stdout = %q, want JSON without human output", stdout.String())
	}

	payload := decodeRecoverJSONTestPayload(t, stdout.Bytes())
	if payload.SchemaVersion != contractversion.RecoveryJSON ||
		payload.Command != "recover" ||
		payload.Mode != "dry-run" ||
		payload.AuthorityKind != "active_journal" {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.Classification != "needs_rollback" || payload.HasErrors {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.OperationID != "20260621T120000.000000000Z-apply" || payload.OperationDir == "" {
		t.Fatalf("operation = %#v", payload)
	}
	if payload.ActionCount != len(payload.Actions) || len(payload.Actions) == 0 {
		t.Fatalf("actions = %#v", payload.Actions)
	}
	action := payload.Actions[0]
	if action.Kind != "restore_write" || action.Reason != "restore_file" || action.ResourceID != "instructions/project" || action.BackupPath != "files/000001" {
		t.Fatalf("action = %#v", action)
	}
}

func TestRunRecoverDryRunJSONReportsBlockedPlan(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths, currentState, _, _, _ := captureCLIRecoveryUpdateJournal(t, manifestPath)
	testkit.WriteStatefile(t, paths.StatefilePath, currentState)
	testkit.WriteFile(t, tempDir, "AGENTS.md", "new instructions\n")
	backupPath := filepath.Join(paths.RecoveryDir, "20260621T120000.000000000Z-apply", "files", "000001")
	if err := os.Remove(backupPath); err != nil {
		t.Fatalf("Remove backup returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	payload := decodeRecoverJSONTestPayload(t, stdout.Bytes())
	if payload.Classification != "blocked" || !payload.HasErrors {
		t.Fatalf("payload = %#v, want blocked errors", payload)
	}
	if len(payload.Actions) == 0 || payload.Actions[0].Kind != "error" || payload.Actions[0].Reason != "backup_mismatch" {
		t.Fatalf("actions = %#v", payload.Actions)
	}
}

func TestRunRecoverJSONRequiresNonInteractiveMode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"recover", "--json"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--json requires --dry-run or --yes") {
		t.Fatalf("stderr = %q, want json mode diagnostic", stderr.String())
	}
}

func decodeRecoverJSONTestPayload(t *testing.T, content []byte) recoverJSONTestPayload {
	t.Helper()

	var payload recoverJSONTestPayload
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("Decode returned error: %v\ncontent=%s", err, content)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("unexpected trailing JSON content: %s", content)
	}

	return payload
}
