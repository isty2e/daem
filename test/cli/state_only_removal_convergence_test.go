package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

func TestStateOnlyManagedOutputsRemainVisibleAndConvergeAfterLastDeclarationRemoval(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	manifestPath := filepath.Join(root, "daem.toml")
	statefilePath := filepath.Join(root, ".daem", "state.json")

	testkit.WriteFile(t, root, "AGENTS.md", "project instructions\n")
	testkit.WriteFile(t, home, ".codex/AGENTS.md", "global instructions\n")
	testkit.WriteFile(t, root, ".agents/skills/demo/SKILL.md", "---\nname: demo\ndescription: Demo.\n---\n")
	hooks := "{\n  \"Stop\": [\n    {\n      \"hooks\": [\n        {\n          \"type\": \"command\",\n          \"command\": \"make old\"\n        }\n      ]\n    }\n  ]\n}\n"
	testkit.WriteFile(t, root, ".codex/hooks.json", "{\n  \"hooks\": "+strings.TrimSpace(hooks)+",\n  \"meta\": true\n}\n")
	testkit.WriteHookManifestAndLock(t, root, `
version = 1
targets = ["codex"]

[[hook]]
name = "old"
event = "Stop"
command = "make old"
targets = ["codex"]
`)
	hookStates := testkit.HookAggregateStatesFromLock(t, filepath.Join(root, "daem.lock.toml"))
	testkit.WriteHookManifestAndLock(t, root, "version = 1\ntargets = [\"codex\"]\n")

	projectInstructionHash := testkit.HashPath(t, filepath.Join(root, "AGENTS.md"))
	globalInstructionHash := testkit.HashPath(t, filepath.Join(home, ".codex", "AGENTS.md"))
	skillHash := testkit.HashDirectory(t, filepath.Join(root, ".agents", "skills", "demo"))
	managedPaths := []durable.ManagedPathState{
		testkit.InstructionPathState(t, "project", []string{"codex"}, "project", "AGENTS.md", projectInstructionHash),
		testkit.InstructionPathState(t, "global", []string{"codex"}, "global", "~/.codex/AGENTS.md", globalInstructionHash),
		testkit.SkillPathState(t, "demo", []string{"codex"}, "project", ".agents/skills/demo", skillHash),
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths:      managedPaths,
		ManagedAggregates: hookStates,
	})
	if err != nil {
		t.Fatalf("durable.NewSnapshot returned error: %v", err)
	}
	testkit.WriteStatefile(t, statefilePath, snapshot)
	testkit.WriteActiveOwnershipClaim(t, manifestPath, "~/.codex/AGENTS.md", "")

	var statusStdout bytes.Buffer
	var statusStderr bytes.Buffer
	statusExit := testkit.RunVerboseCLI(
		[]string{"status", "--manifest", manifestPath, "--target", "codex", "--check"},
		&statusStdout,
		&statusStderr,
	)
	if statusExit != 1 || statusStderr.Len() != 0 {
		t.Fatalf("status exit=%d stdout=%q stderr=%q, want pending cleanup", statusExit, statusStdout.String(), statusStderr.String())
	}
	for _, want := range []string{
		`delete resource="instructions/project"`,
		`delete resource="instructions/global"`,
		`delete resource="skill/demo"`,
		`delete resource="hook/old"`,
	} {
		if !strings.Contains(statusStdout.String(), want) {
			t.Fatalf("status stdout=%q, want %q", statusStdout.String(), want)
		}
	}

	var listStdout bytes.Buffer
	var listStderr bytes.Buffer
	listExit := testkit.RunVerboseCLI(
		[]string{"list", "outputs", "--manifest", manifestPath, "--target", "codex", "--json"},
		&listStdout,
		&listStderr,
	)
	if listExit != 0 || listStderr.Len() != 0 {
		t.Fatalf("list exit=%d stdout=%q stderr=%q", listExit, listStdout.String(), listStderr.String())
	}
	var inventory struct {
		ManagedCount int `json:"managed_count"`
		Managed      []struct {
			ResourceID string   `json:"resource_id"`
			Subject    string   `json:"subject"`
			Targets    []string `json:"targets"`
		} `json:"managed"`
	}
	if err := json.Unmarshal(listStdout.Bytes(), &inventory); err != nil {
		t.Fatalf("decode list outputs JSON: %v\n%s", err, listStdout.Bytes())
	}
	if inventory.ManagedCount != 4 {
		t.Fatalf("managed_count=%d, want 4", inventory.ManagedCount)
	}
	managedSkillFound := false
	managedHookFound := false
	for _, entry := range inventory.Managed {
		if entry.ResourceID == "skill/demo" {
			managedSkillFound = entry.Subject != "" && len(entry.Targets) == 1 && entry.Targets[0] == "codex"
		}
		if entry.ResourceID == "hook/old" {
			managedHookFound = entry.Subject != "" && len(entry.Targets) == 1 && entry.Targets[0] == "codex"
		}
	}
	if !managedSkillFound {
		t.Fatalf("managed inventory lost Skill resource/subject/consumer correlation: %#v", inventory.Managed)
	}
	if !managedHookFound {
		t.Fatalf("managed inventory lost Hook aggregate subject/target correlation: %#v", inventory.Managed)
	}

	var applyStdout bytes.Buffer
	var applyStderr bytes.Buffer
	applyExit := testkit.RunVerboseCLI(
		[]string{"apply", "--manifest", manifestPath, "--target", "codex", "--yes"},
		&applyStdout,
		&applyStderr,
	)
	if applyExit != 0 || applyStderr.Len() != 0 {
		t.Fatalf("apply exit=%d stdout=%q stderr=%q", applyExit, applyStdout.String(), applyStderr.String())
	}
	if !strings.Contains(applyStdout.String(), "applied: 4 actions") {
		t.Fatalf("apply stdout=%q, want four cleanup actions", applyStdout.String())
	}

	for _, removedPath := range []string{
		filepath.Join(root, "AGENTS.md"),
		filepath.Join(home, ".codex", "AGENTS.md"),
		filepath.Join(root, ".agents", "skills", "demo"),
	} {
		if _, err := os.Stat(removedPath); !os.IsNotExist(err) {
			t.Fatalf("managed output %q exists or stat failed: %v", removedPath, err)
		}
	}
	testkit.AssertFileContent(t, filepath.Join(root, ".codex", "hooks.json"), "{\n  \"meta\": true\n}\n")

	state, err := statefile.Load(t.Context(), statefilePath)
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	if len(state.ManagedPaths()) != 0 || len(state.ManagedAggregates()) != 0 {
		t.Fatalf(
			"state managed paths/aggregates=%#v/%#v, want cleanup convergence",
			state.ManagedPaths(),
			state.ManagedAggregates(),
		)
	}
	testkit.AssertNoRecoveryArtifacts(t, root)
}

func TestAuthoringRemovalOfLastInstructionConvergesThroughStatusInventoryAndApply(t *testing.T) {
	root := t.TempDir()
	testkit.WithWorkingDirectory(t, root)
	manifestPath := filepath.Join(root, "daem.toml")

	runCUXJourney(t, []string{"init"}, "created: manifest")
	testkit.WriteFile(t, root, "instructions/project.md", "project instructions\n")
	runCUXJourney(t, []string{
		"add", "instruction", "project", "./instructions/project.md",
		"--target", "codex",
	}, "added: instructions/project", "lockfile: wrote")
	runCUXJourney(t, []string{"apply", "--yes"}, "applied: 1 actions")
	testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), "project instructions\n")

	runCUXJourney(t, []string{"remove", "instruction", "project"}, "removed: instructions/project", "lockfile: wrote")

	var statusStdout bytes.Buffer
	var statusStderr bytes.Buffer
	statusExit := testkit.RunCLI([]string{"status", "--target", "codex", "--check"}, &statusStdout, &statusStderr)
	if statusExit != 1 || statusStderr.Len() != 0 {
		t.Fatalf("status exit=%d stdout=%q stderr=%q, want pending removal", statusExit, statusStdout.String(), statusStderr.String())
	}
	if !strings.Contains(statusStdout.String(), `remove managed output resource="instructions/project"`) {
		t.Fatalf("status stdout=%q, want managed removal", statusStdout.String())
	}

	var listStdout bytes.Buffer
	var listStderr bytes.Buffer
	listExit := testkit.RunCLI([]string{"list", "outputs", "--target", "codex", "--json"}, &listStdout, &listStderr)
	if listExit != 0 || listStderr.Len() != 0 {
		t.Fatalf("list exit=%d stdout=%q stderr=%q", listExit, listStdout.String(), listStderr.String())
	}
	var inventory struct {
		ManagedCount int `json:"managed_count"`
	}
	if err := json.Unmarshal(listStdout.Bytes(), &inventory); err != nil {
		t.Fatalf("decode list outputs JSON: %v\n%s", err, listStdout.Bytes())
	}
	if inventory.ManagedCount != 1 {
		t.Fatalf("managed_count=%d, want removed instruction still inventoried", inventory.ManagedCount)
	}

	runCUXJourney(t, []string{"apply", "--target", "codex", "--dry-run"}, "remove managed output")
	runCUXJourney(t, []string{"apply", "--target", "codex", "--yes"}, "applied: 1 actions")
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("AGENTS.md exists or stat failed after removal: %v", err)
	}
	state, err := statefile.Load(t.Context(), filepath.Join(root, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	if len(state.ManagedPaths()) != 0 || len(state.ManagedAggregates()) != 0 {
		t.Fatalf(
			"state managed paths/aggregates=%#v/%#v, want final convergence",
			state.ManagedPaths(),
			state.ManagedAggregates(),
		)
	}

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if strings.Contains(string(content), "[instructions.") {
		t.Fatalf("manifest still contains removed instruction:\n%s", content)
	}
}

func TestStateOnlyRemovalPreservesMissingAndDriftedSafetyFailures(t *testing.T) {
	tests := []struct {
		name        string
		liveContent string
		stateHash   string
		wantReason  string
	}{
		{
			name:       "missing output",
			stateHash:  "sha256:missing-baseline",
			wantReason: "reason=missing_output",
		},
		{
			name:        "drifted output",
			liveContent: "edited after apply\n",
			stateHash:   "sha256:old-baseline",
			wantReason:  "reason=drifted_output",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifestPath := filepath.Join(root, "daem.toml")
			statefilePath := filepath.Join(root, ".daem", "state.json")
			testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
			testkit.WriteLockfile(t, filepath.Join(root, "daem.lock.toml"), testkit.ExactSupplyLockfile(t))
			if test.liveContent != "" {
				testkit.WriteFile(t, root, "AGENTS.md", test.liveContent)
			}
			testkit.WriteStatefile(t, statefilePath, testkit.Snapshot(
				t,
				testkit.InstructionPathState(t, "removed", []string{"codex"}, "project", "AGENTS.md", test.stateHash),
			))
			stateBefore, err := os.ReadFile(statefilePath)
			if err != nil {
				t.Fatalf("read state before dry-run: %v", err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(
				[]string{"apply", "--manifest", manifestPath, "--target", "codex", "--dry-run"},
				&stdout,
				&stderr,
			)
			if exitCode != 1 || stderr.Len() != 0 {
				t.Fatalf("apply exit=%d stdout=%q stderr=%q, want safety failure", exitCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), test.wantReason) {
				t.Fatalf("apply stdout=%q, want %q", stdout.String(), test.wantReason)
			}

			stateAfter, err := os.ReadFile(statefilePath)
			if err != nil {
				t.Fatalf("read state after dry-run: %v", err)
			}
			if !bytes.Equal(stateAfter, stateBefore) {
				t.Fatal("state-only safety failure mutated statefile")
			}
			if test.liveContent != "" {
				testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), test.liveContent)
			}
		})
	}
}

func TestStateOnlyCleanupTargetIsNotHiddenByDifferentManifestTarget(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "instructions/active.md", "claude instructions\n")
	testkit.WriteFile(t, root, "AGENTS.md", "old codex instructions\n")
	activeHash := testkit.HashPath(t, filepath.Join(root, "instructions", "active.md"))
	oldHash := testkit.HashPath(t, filepath.Join(root, "AGENTS.md"))
	testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["claude-code"]

[instructions.active]
source = "instructions/active.md"
`)
	testkit.WriteLockfile(t, filepath.Join(root, "daem.lock.toml"), testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "active", SourceID: "local:instructions/active.md?mode=vendor", ContentHash: activeHash, Targets: []target.Target{target.TargetClaudeCode}}))
	testkit.WriteStatefile(t, filepath.Join(root, ".daem", "state.json"), testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "old", []string{"codex"}, "project", "AGENTS.md", oldHash),
	))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"status", "--manifest", manifestPath, "--check"},
		&stdout,
		&stderr,
	)
	if exitCode != 1 || stderr.Len() != 0 {
		t.Fatalf("status exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		`create resource="instructions/active" target=claude-code`,
		`delete resource="instructions/old" target=codex`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("status stdout=%q, want %q", stdout.String(), want)
		}
	}
}
