package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunLockWritesInstructionLocksForNewInstructionSurfaces(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/project.md", "project instructions\n")
	testkit.WriteFile(t, tempDir, "instructions/global.md", "global instructions\n")
	globalSourcePath := filepath.Join(tempDir, "instructions", "global.md")
	testkit.WriteFile(t, tempDir, "daem.toml", fmt.Sprintf(`
version = 1
targets = ["opencode", "pi", "antigravity-cli"]

[instructions.project]
source = "instructions/project.md"
targets = ["opencode", "pi", "antigravity-cli"]

[instructions.global]
source = %q
scope = "global"
targets = ["opencode", "pi"]
`, filepath.ToSlash(globalSourcePath)))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	locked, err := lockfile.Load(t.Context(), lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(testkit.LockedInstructions(t, locked)) != 2 {
		t.Fatalf("locked instructions = %#v, want project and global", testkit.LockedInstructions(t, locked))
	}
	lockedByName := map[string]testkit.LockedExactSupplyView{}
	for _, resource := range testkit.LockedInstructions(t, locked) {
		lockedByName[resource.Name] = resource
	}
	if _, ok := lockedByName["project"]; !ok {
		t.Fatalf("locked instructions = %#v, want project entry", testkit.LockedInstructions(t, locked))
	}
	if _, ok := lockedByName["global"]; !ok {
		t.Fatalf("locked instructions = %#v, want global entry", testkit.LockedInstructions(t, locked))
	}
}

func TestRunStatusJSONReportsSharedInstructionTargetsForOpenCodePiAntigravity(t *testing.T) {
	fixture := writeSharedInstructionFixture(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"status", "--manifest", fixture.manifestPath, "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	payload := clijson.DecodePlan(t, stdout.Bytes())
	if payload.ActionCount != 1 || len(payload.Actions) != 1 {
		t.Fatalf("actions = %#v, want one shared instruction action", payload.Actions)
	}
	action := payload.Actions[0]
	if action.Kind != "create" ||
		action.Reason != "missing_output" ||
		action.ResourceID != "instructions/project" ||
		action.Target != "" ||
		action.Destination != "AGENTS.md" {
		t.Fatalf("action = %#v, want shared instruction create", action)
	}
	assertStringSlice(t, action.Targets, []string{"antigravity-cli", "opencode", "pi"})
}

func TestRunApplyWritesOneSharedInstructionOutputForOpenCodePiAntigravity(t *testing.T) {
	fixture := writeSharedInstructionFixture(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", fixture.manifestPath, "--yes", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	payload := clijson.DecodeApplyResult(t, stdout.Bytes())
	if payload.ActionCount != 1 || len(payload.Actions) != 1 {
		t.Fatalf("actions = %#v, want one shared instruction apply action", payload.Actions)
	}
	action := payload.Actions[0]
	if action.Kind != "create" ||
		action.Reason != "missing_output" ||
		action.ResourceID != "instructions/project" ||
		action.Target != "" ||
		action.Destination != "AGENTS.md" {
		t.Fatalf("action = %#v, want shared instruction create", action)
	}
	assertStringSlice(t, action.Targets, []string{"antigravity-cli", "opencode", "pi"})
	testkit.AssertFileContent(t, filepath.Join(fixture.root, "AGENTS.md"), "shared instructions\n")

	state, err := statefile.Load(t.Context(), filepath.Join(fixture.root, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	if len(state.ManagedPaths()) != 1 {
		t.Fatalf("managed paths = %#v, want one shared resource", state.ManagedPaths())
	}
	testkit.AssertManagedPathState(
		t, state, entity.KindInstructions, "project",
		[]string{"antigravity-cli", "opencode", "pi"},
		"project", "AGENTS.md", fixture.contentHash, "file",
	)
}

func TestRunApplyTransitionsSharedInstructionOwnershipAcrossPrimaryTargets(t *testing.T) {
	tests := []struct {
		name           string
		initialTargets []string
		nextTargets    []string
		wantAction     []string
		wantState      []string
	}{
		{
			name:           "remove primary OpenCode consumer",
			initialTargets: []string{"opencode", "pi"},
			nextTargets:    []string{"pi"},
			wantAction:     []string{"pi"},
			wantState:      []string{"pi"},
		},
		{
			name:           "remove secondary Pi consumer",
			initialTargets: []string{"opencode", "pi"},
			nextTargets:    []string{"opencode"},
			wantAction:     []string{"opencode"},
			wantState:      []string{"opencode"},
		},
		{
			name:           "expand with earlier primary OpenCode consumer",
			initialTargets: []string{"pi"},
			nextTargets:    []string{"opencode", "pi"},
			wantAction:     []string{"opencode", "pi"},
			wantState:      []string{"opencode", "pi"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifestPath := filepath.Join(root, "daem.toml")
			testkit.WriteFile(t, root, "instructions/shared.md", "shared instructions\n")
			contentHash := testkit.HashPath(t, filepath.Join(root, "instructions", "shared.md"))
			writeSharedInstructionManifest(t, root, test.initialTargets)
			writeSharedInstructionLock(t, root, contentHash, test.initialTargets)
			runSharedInstructionApply(t, manifestPath)

			writeSharedInstructionManifest(t, root, test.nextTargets)
			writeSharedInstructionLock(t, root, contentHash, test.nextTargets)
			payload := runSharedInstructionApply(t, manifestPath)
			if payload.ActionCount != 1 || len(payload.Actions) != 1 {
				t.Fatalf("actions = %#v, want one ownership transition", payload.Actions)
			}
			action := payload.Actions[0]
			if action.Kind != "record" || action.Reason != "state_stale" || action.Target != "" {
				t.Fatalf("action = %#v, want subject-owned record transition", action)
			}
			assertStringSlice(t, action.Targets, test.wantAction)
			testkit.AssertFileContent(t, filepath.Join(root, "AGENTS.md"), "shared instructions\n")

			state, err := statefile.Load(t.Context(), filepath.Join(root, ".daem", "state.json"))
			if err != nil {
				t.Fatalf("statefile.Load returned error: %v", err)
			}
			managedPaths := state.ManagedPaths()
			if len(managedPaths) != 1 {
				t.Fatalf("managed paths = %#v, want one subject-owned path", managedPaths)
			}
			consumers := make([]string, 0, len(managedPaths[0].ConsumerTargets()))
			for _, consumer := range managedPaths[0].ConsumerTargets() {
				consumers = append(consumers, string(consumer))
			}
			assertStringSlice(t, consumers, test.wantState)
			testkit.AssertNoRecoveryArtifacts(t, root)
		})
	}
}

func TestRunApplyRejectsUnmanagedSharedInstructionOutputForNewTargets(t *testing.T) {
	fixture := writeSharedInstructionFixture(t)
	testkit.WriteFile(t, fixture.root, "AGENTS.md", "foreign instructions\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", fixture.manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stderr.String(), "unmanaged_output_exists") {
		t.Fatalf("stderr = %q, want unmanaged output diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	testkit.AssertFileContent(t, filepath.Join(fixture.root, "AGENTS.md"), "foreign instructions\n")
	assertPathMissing(t, filepath.Join(fixture.root, ".daem"))
}

func TestRunApplyWritesGlobalOpenCodePiAndAntigravityInstructionOutputs(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)

	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/global.md", "global instructions\n")
	globalSourcePath := filepath.Join(tempDir, "instructions", "global.md")
	contentHash := testkit.HashPath(t, globalSourcePath)
	testkit.WriteFile(t, tempDir, "daem.toml", fmt.Sprintf(`
version = 1
	targets = ["opencode", "pi", "antigravity-cli"]

[instructions.global]
source = %q
scope = "global"
	targets = ["opencode", "pi", "antigravity-cli"]
	`, filepath.ToSlash(globalSourcePath)))
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "global", SourceID: "local:" + filepath.ToSlash(globalSourcePath) + "?mode=vendor", ContentHash: contentHash, Targets: []target.Target{target.TargetOpenCode, target.TargetPi, target.TargetAntigravityCLI}, Scope: target.ScopeGlobal}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes", "--json"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	payload := clijson.DecodeApplyResult(t, stdout.Bytes())
	if payload.ActionCount != 3 || len(payload.Actions) != 3 {
		t.Fatalf("actions = %#v, want opencode, pi, and antigravity global writes", payload.Actions)
	}

	testkit.AssertFileContent(t, filepath.Join(homeDir, ".config", "opencode", "AGENTS.md"), "global instructions\n")
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".pi", "agent", "AGENTS.md"), "global instructions\n")
	testkit.AssertFileContent(t, filepath.Join(homeDir, ".gemini", "GEMINI.md"), "global instructions\n")
	state, err := statefile.Load(t.Context(), filepath.Join(tempDir, ".daem", "state.json"))
	if err != nil {
		t.Fatalf("statefile.Load returned error: %v", err)
	}
	testkit.AssertStateResourceNamed(t, state, "global", "opencode", "global", "~/.config/opencode/AGENTS.md", contentHash)
	testkit.AssertStateResourceNamed(t, state, "global", "pi", "global", "~/.pi/agent/AGENTS.md", contentHash)
	testkit.AssertStateResourceNamed(t, state, "global", "antigravity-cli", "global", "~/.gemini/GEMINI.md", contentHash)
}

func TestRunApplyRejectsAntigravitySymlinkInstructionModeBeforeMutation(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/AGENTS.md", "project instructions\n")
	contentHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/AGENTS.md"))
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["antigravity-cli"]

[instructions.project]
source = "instructions/AGENTS.md"
targets = ["antigravity-cli"]

[instructions.project.target.antigravity-cli]
mode = "symlink"
`)
	testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: contentHash, Targets: []target.Target{target.TargetAntigravityCLI}, InstallMode: "symlink"}))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("symlink")) {
		t.Fatalf("stderr = %q, want symlink diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	assertPathMissing(t, filepath.Join(tempDir, "AGENTS.md"))
	assertPathMissing(t, filepath.Join(tempDir, ".daem"))
}

func TestRunLockRejectsDistinctInstructionResourcesSharingAGENTS(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "instructions/shared.md", "shared instructions\n")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode", "pi"]

[instructions.opencode]
source = "instructions/shared.md"
targets = ["opencode"]

[instructions.pi]
source = "instructions/shared.md"
targets = ["pi"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		"lock failed: duplicate managed path occupancy",
		`scope="project" destination="AGENTS.md"`,
		"instructions:opencode",
		"instructions:pi",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("failed lock wrote lockfile or stat failed: %v", err)
	}
}

type sharedInstructionFixture struct {
	root         string
	manifestPath string
	lockfilePath string
	contentHash  string
}

func writeSharedInstructionFixture(t *testing.T) sharedInstructionFixture {
	t.Helper()

	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
	testkit.WriteFile(t, tempDir, "instructions/shared.md", "shared instructions\n")
	contentHash := testkit.HashPath(t, filepath.Join(tempDir, "instructions/shared.md"))
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["opencode", "pi", "antigravity-cli"]

[instructions.project]
source = "instructions/shared.md"
targets = ["opencode", "pi", "antigravity-cli"]
`)
	writeSharedInstructionLock(t, tempDir, contentHash, []string{"opencode", "pi", "antigravity-cli"})

	return sharedInstructionFixture{
		root:         tempDir,
		manifestPath: manifestPath,
		lockfilePath: lockfilePath,
		contentHash:  contentHash,
	}
}

func writeSharedInstructionManifest(t *testing.T, root string, targets []string) {
	t.Helper()
	quotedTargets := make([]string, 0, len(targets))
	for _, selected := range targets {
		quotedTargets = append(quotedTargets, fmt.Sprintf("%q", selected))
	}
	testkit.WriteFile(t, root, "daem.toml", fmt.Sprintf(`
version = 1
targets = [%s]

[instructions.project]
source = "instructions/shared.md"
targets = [%s]
`, strings.Join(quotedTargets, ", "), strings.Join(quotedTargets, ", ")))
}

func writeSharedInstructionLock(t *testing.T, root string, contentHash string, values []string) {
	t.Helper()
	targets := make([]target.Target, 0, len(values))
	for _, value := range values {
		parsed, err := target.ParseTarget(value)
		if err != nil {
			t.Fatalf("target.ParseTarget(%q): %v", value, err)
		}
		targets = append(targets, parsed)
	}
	testkit.WriteLockfile(
		t,
		filepath.Join(root, "daem.lock.toml"),
		testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{
			Kind: testkit.ExactSupplyInstructions, Name: "project",
			SourceID: "local:instructions/shared.md?mode=vendor", ContentHash: contentHash,
			Targets: targets,
		}),
	)
}

func runSharedInstructionApply(t *testing.T, manifestPath string) clijson.ApplyResult {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"apply", "--manifest", manifestPath, "--yes", "--json"}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("apply exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	return clijson.DecodeApplyResult(t, stdout.Bytes())
}

func assertStringSlice(t *testing.T, got []string, want []string) {
	t.Helper()

	if !slices.Equal(got, want) {
		t.Fatalf("values = %#v, want %#v", got, want)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path %q exists or stat failed unexpectedly: %v", path, err)
	}
}
