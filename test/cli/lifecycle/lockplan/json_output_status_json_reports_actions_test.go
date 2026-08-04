package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunStatusJSONReportsActionsAndLockOnlyResources(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "instructions\n")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: Use for oracle review.\n---\n")
	instructionHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
	skillHash := testkit.HashDirectory(t, filepath.Join(tempDir, "skills/oracle"))

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex", "opencode", "pi"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["opencode"]

[[hook]]
name = "notify"
event = "pre_apply"
command = "python3 hooks/notify.py"
targets = ["pi"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["codex"]
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
		Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash,
		Targets: []target.Target{target.TargetOpenCode}, Scope: target.ScopeProject,
	}, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if strings.Contains(stdout.String(), "status:") || strings.Contains(stdout.String(), "lock-only:") {
		t.Fatalf("stdout = %q, want JSON without human output", stdout.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	if payload.Command != "status" || payload.Mode != "status" {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.ActionCount != 2 || len(payload.Actions) != 2 {
		t.Fatalf("actions = %#v", payload.Actions)
	}
	seenActions := map[string]bool{}
	for _, action := range payload.Actions {
		if action.Kind != "create" || action.Reason != "missing_output" {
			t.Fatalf("action = %#v, want create/missing_output", action)
		}
		switch action.ResourceID {
		case "instructions/project":
			if action.PermissionPolicy != "executable-class" || action.DesiredFileMode != 0o600 || action.LiveFileMode != 0 {
				t.Fatalf("instruction permission facts = %#v", action)
			}
		case "skill/oracle":
			if action.PermissionPolicy != "none" || action.DesiredFileMode != 0 || action.LiveFileMode != 0 {
				t.Fatalf("skill permission facts = %#v", action)
			}
		}
		seenActions[action.ResourceID] = true
	}
	if !seenActions["instructions/project"] || !seenActions["skill/oracle"] {
		t.Fatalf("actions = %#v, want instruction and opencode skill creates", payload.Actions)
	}
	if len(payload.LockOnly.Skills) != 0 {
		t.Fatalf("lock-only skills = %#v", payload.LockOnly.Skills)
	}
	if len(payload.LockOnly.Hooks) != 1 || payload.LockOnly.Hooks[0].Name != "notify" || payload.LockOnly.Hooks[0].Targets[0] != "pi" {
		t.Fatalf("lock-only hooks = %#v", payload.LockOnly.Hooks)
	}
}

func TestRunStatusJSONUsesLockExpandedSelectorSkillsWithoutRediscovery(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/active/SKILL.md", "---\nname: active\ndescription: active\n---\n")

	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
targets = ["codex"]
`)
	var lockStdout bytes.Buffer
	var lockStderr bytes.Buffer
	if exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &lockStdout, &lockStderr); exitCode != 0 {
		t.Fatalf("initial lock exitCode = %d, stdout = %q stderr = %q", exitCode, lockStdout.String(), lockStderr.String())
	}
	testkit.WriteFile(t, tempDir, "skills/new/SKILL.md", "---\nname: new\ndescription: new\n---\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	if payload.ActionCount != 1 || len(payload.Actions) != 1 {
		t.Fatalf("actions = %#v, want only locked selector child", payload.Actions)
	}
	if payload.Actions[0].ResourceID != "skill/active" {
		t.Fatalf("actions = %#v, want no rediscovered skill/new", payload.Actions)
	}
	if strings.Contains(stdout.String(), "skill/new") || strings.Contains(stdout.String(), "missing_lock") {
		t.Fatalf("stdout = %q, want status to consume only lockfile-expanded selector entries", stdout.String())
	}
}

func TestRunStatusJSONReportsSkillRepairabilityDiagnostic(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	diagnostic := clijson.FindPlanDiagnostic(t, payload, skillDiagnosticRepairable, "error", "skill/oracle", "opencode")
	if diagnostic.Repairability != "mechanical" {
		t.Fatalf("diagnostic = %#v, want mechanical repairability", diagnostic)
	}
	if !strings.Contains(diagnostic.NextStep, "compat_repair = true") {
		t.Fatalf("diagnostic = %#v, want compat_repair next step", diagnostic)
	}
	if len(diagnostic.RepairActions) == 0 || !strings.Contains(strings.Join(diagnostic.RepairActions, "; "), "rename file: skill.md -> SKILL.md") {
		t.Fatalf("diagnostic = %#v, want rename repair action", diagnostic)
	}
}

func TestRunStatusJSONReportsManualDiagnosticDespiteCompatRepair(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/SKILL.md", "---\nname: oracle\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
compat_repair = true
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	diagnostic := clijson.FindPlanDiagnostic(t, payload, skillDiagnosticManual, "error", "skill/oracle", "opencode")
	if diagnostic.Repairability != "manual" {
		t.Fatalf("diagnostic = %#v, want manual repairability", diagnostic)
	}
	if strings.Contains(diagnostic.NextStep, "compat_repair") {
		t.Fatalf("diagnostic = %#v, want no compat_repair next step for manual case", diagnostic)
	}
	if len(diagnostic.ManualReasons) == 0 || !strings.Contains(strings.Join(diagnostic.ManualReasons, "; "), "description is required") {
		t.Fatalf("diagnostic = %#v, want description manual reason", diagnostic)
	}
}

func TestRunStatusJSONSuppressesDeclaredMechanicalRepairDiagnostic(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
compat_repair = true
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	for _, diagnostic := range payload.Diagnostics {
		if diagnostic.Code == skillDiagnosticRepairable {
			t.Fatalf("diagnostics = %#v, want declared mechanical repair diagnostic suppressed", payload.Diagnostics)
		}
	}
}

func TestRunStatusCheckJSONFailsBeforePlanningInvalidLockedSkillSource(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	writeCurrentInvalidOpenCodeSkillFixture(t, tempDir, manifestPath, lockfilePath, statefilePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", manifestPath, "--check", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no JSON after lock freshness preflight failure", stdout.String())
	}
	if !strings.Contains(stderr.String(), "inspect lockfile freshness") ||
		!strings.Contains(stderr.String(), "description is required") {
		t.Fatalf("stderr = %q, want invalid source preflight failure", stderr.String())
	}
}

func TestRunApplyDryRunJSONReportsSkillRepairabilityDiagnostic(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "skills/oracle/skill.md", "---\ndescription: Use for oracle review.\n---\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: string(artifact.HashFileContent([]byte("stale lock fixture")))}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	diagnostic := clijson.FindPlanDiagnostic(t, payload, skillDiagnosticRepairable, "error", "skill/oracle", "opencode")
	if diagnostic.Repairability != "mechanical" || !strings.Contains(diagnostic.NextStep, "compat_repair = true") {
		t.Fatalf("diagnostic = %#v, want mechanical compat_repair guidance", diagnostic)
	}
}

func TestRunApplyDryRunJSONFailsBeforePlanningInvalidLockedSkillSource(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	statefilePath := filepath.Join(tempDir, ".daem", "state.json")
	writeCurrentInvalidOpenCodeSkillFixture(t, tempDir, manifestPath, lockfilePath, statefilePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--dry-run", "--json"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no JSON after lock freshness preflight failure", stdout.String())
	}
	if !strings.Contains(stderr.String(), "inspect lockfile freshness") ||
		!strings.Contains(stderr.String(), "description is required") {
		t.Fatalf("stderr = %q, want invalid source preflight failure", stderr.String())
	}
}

func TestRunApplyYesBlocksErrorDiagnosticBeforeWritingSkill(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	writeInvalidOpenCodeSkillManifestAndLock(t, tempDir, manifestPath, lockfilePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1; stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "inspect lockfile freshness") ||
		!strings.Contains(stderr.String(), "description is required") {
		t.Fatalf("stderr = %q, want invalid source preflight failure", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, ".opencode", "skills", "oracle")); !os.IsNotExist(err) {
		t.Fatalf("skill output exists or stat failed unexpectedly: %v", err)
	}
}
