//go:build darwin || linux

package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativePiKeysPrecedeActualExtensionInvocation(t *testing.T) {
	fixture := nativefixture.NewPiInvocation(t, "")
	code, stdout, stderr := fixture.Run("apply", "--yes", "--json")
	if code != 0 {
		t.Fatalf("mixed Apply failed: %d %s %s", code, stdout, stderr)
	}
	var result struct {
		ActionCount int `json:"action_count"`
		Actions     []struct {
			Kind string `json:"kind"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.ActionCount != 1 || len(result.Actions) != 1 || result.Actions[0].Kind != "create" {
		t.Fatalf("completed key prefix was lost or counted twice in the result: %s", stdout)
	}
	fixture.LaunchProtection(t)
	type fixtureSettings struct {
		Theme   string `json:"theme"`
		Foreign struct {
			Retained bool `json:"retained"`
		} `json:"foreign"`
	}
	var before, after fixtureSettings
	if err := json.Unmarshal(testkit.ReadFile(t, fixture.BeforeCommand), &before); err != nil || before.Theme != "fixture-theme" || !before.Foreign.Retained {
		t.Fatal("selected keys were not settled before the actual host command", err)
	}
	if err := json.Unmarshal(testkit.ReadFile(t, fixture.Settings), &after); err != nil || after.Theme != "fixture-theme" || !after.Foreign.Retained {
		t.Fatal("actual command did not preserve selected and unrelated values", err)
	}
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
		t.Fatal("mixed execution did not invoke exactly its selected command")
	}
	fixture.Require(t, "status", "--check", "--json")
	fixture.Require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
		t.Fatal("converged mixed execution replayed the host command")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiInvocationWithInitiallyCurrentKeys(t *testing.T) {
	fixture := nativefixture.NewPiInvocation(t, "")
	fixture.Require(t, "apply", "--yes")
	testkit.WriteFile(t, fixture.Workspace, ".pi/settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
	fixture.Require(t, "apply", "--yes")
	fixture.LaunchProtection(t)
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\ninstall\n" {
		t.Fatal("initially-current keys suppressed or duplicated their required host invocation")
	}
	fixture.Require(t, "status", "--check", "--json")
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiInvocationWithOrdinaryFileChanges(t *testing.T) {
	fixture := nativefixture.NewPiInvocation(t, "")
	fixture.AddInstructions(t, "first instructions\n")
	for index, content := range []string{"first instructions\n", "updated instructions\n"} {
		if index != 0 {
			testkit.WriteFile(t, fixture.Workspace, "instructions.md", content)
			testkit.WriteFile(t, filepath.Dir(fixture.Settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
			fixture.Require(t, "lock")
		}
		fixture.Require(t, "apply", "--yes")
		fixture.LaunchProtection(t)
		beforeInstructions := "absent\n"
		if index != 0 {
			beforeInstructions = "present\nfirst instructions\n"
		}
		if got := string(testkit.ReadFile(t, fixture.InstructionsBefore)); got != beforeInstructions {
			t.Fatal("ordinary file was changed before the host command")
		}
		if got := string(testkit.ReadFile(t, filepath.Join(fixture.Workspace, "AGENTS.md"))); got != content {
			t.Fatal("mixed installation did not settle its ordinary file")
		}
		var before struct {
			Theme string `json:"theme"`
		}
		if err := json.Unmarshal(testkit.ReadFile(t, fixture.BeforeCommand), &before); err != nil || before.Theme != "fixture-theme" {
			t.Fatal("mixed installation did not establish its keys before the command", err)
		}
		fixture.Require(t, "status", "--check", "--json")
		calls := string(testkit.ReadFile(t, fixture.Calls))
		fixture.Require(t, "apply", "--yes")
		if got := string(testkit.ReadFile(t, fixture.Calls)); got != calls {
			t.Fatal("converged mixed installation replayed its command")
		}
	}
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\ninstall\n" {
		t.Fatal("ordinary file changes suppressed or duplicated their selected installations")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiInvocationFailureStopsOrdinaryFileChanges(t *testing.T) {
	for _, mutation := range []string{"key", "command"} {
		t.Run(mutation, func(t *testing.T) {
			fixture := nativefixture.NewPiInvocation(t, mutation)
			fixture.AddInstructions(t, "pending instructions\n")
			if code, stdout, stderr := fixture.Run("apply", "--yes"); code == 0 {
				t.Fatalf("failed invocation was accepted with pending ordinary work: %s %s", stdout, stderr)
			}
			fixture.LaunchProtection(t)
			if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
				t.Fatal("failed invocation was skipped or replayed")
			}
			if _, err := os.Stat(filepath.Join(fixture.Workspace, "AGENTS.md")); !os.IsNotExist(err) {
				t.Fatal("failed invocation did not stop the ordinary file write", err)
			}
			paths, err := daempaths.Resolve(fixture.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			state, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, paths.StatefilePath))
			if err != nil || len(state.ManagedPaths()) != 0 {
				t.Fatal("failed invocation claimed an unapplied ordinary file", err)
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiInvocationCreatesGlobalRootBeforeOrdinaryAcquisition(t *testing.T) {
	fixture := nativefixture.NewPiInvocationAtScope(t, "initial", target.ScopeGlobal)
	agentRoot := filepath.Join(os.Getenv("HOME"), ".pi", "agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	fixture.Settings = filepath.Join(agentRoot, "settings.json")
	t.Setenv("DAEM_TEST_SETTINGS", fixture.Settings)
	t.Setenv("DAEM_TEST_INSTRUCTIONS_TARGET", filepath.Join(agentRoot, "AGENTS.md"))
	source := filepath.Join(fixture.Workspace, "instructions.md")
	testkit.WriteFile(t, fixture.Workspace, "instructions.md", "global instructions\n")
	manifest := string(testkit.ReadFile(t, fixture.Manifest)) + fmt.Sprintf("\n[instructions.global]\nsource=%q\n", source)
	testkit.WriteFile(t, fixture.Workspace, "daem.toml", manifest)
	if err := os.RemoveAll(filepath.Dir(fixture.Settings)); err != nil {
		t.Fatal(err)
	}
	fixture.Require(t, "lock")
	fixture.Require(t, "apply", "--yes")
	fixture.LaunchProtection(t)
	if got := string(testkit.ReadFile(t, fixture.InstructionsBefore)); got != "absent\n" {
		t.Fatal("global ordinary file was created before the host command")
	}
	if got := string(testkit.ReadFile(t, filepath.Join(filepath.Dir(fixture.Settings), "AGENTS.md"))); got != "global instructions\n" {
		t.Fatal("post-prefix global acquisition lost the ordinary file")
	}
	fixture.Require(t, "status", "--check", "--json")
	fixture.Require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
		t.Fatal("global acquisition replayed the completed command")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiInvocationWithSkillDirectoryChanges(t *testing.T) {
	fixture := nativefixture.NewPiInvocation(t, "")
	source := filepath.Join(fixture.Workspace, "sample")
	const skill = "---\nname: sample\ndescription: mixed invocation fixture\n---\n"
	testkit.WriteFile(t, source, "SKILL.md", skill)
	testkit.WriteFile(t, source, "references/detail.txt", "skill detail\n")
	manifest := string(testkit.ReadFile(t, fixture.Manifest)) + "\n[[skill]]\nname='sample'\nscope='project'\nsource={path='./sample',mode='vendor'}\n"
	testkit.WriteFile(t, fixture.Workspace, "daem.toml", manifest)
	fixture.Require(t, "lock")
	fixture.Require(t, "apply", "--yes")
	for path, expected := range map[string]string{"SKILL.md": skill, "references/detail.txt": "skill detail\n"} {
		if got := string(testkit.ReadFile(t, filepath.Join(fixture.Workspace, ".pi", "skills", "sample", path))); got != expected {
			t.Fatal("mixed invocation lost ordinary directory content", path)
		}
	}
	fixture.Require(t, "status", "--check", "--json")
	fixture.Require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
		t.Fatal("directory convergence replayed its command")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiInvocationDoesNotAdoptChangedOrdinaryFile(t *testing.T) {
	fixture := nativefixture.NewPiInvocation(t, "ordinary")
	fixture.AddInstructions(t, "planned instructions\n")
	if code, stdout, stderr := fixture.Run("apply", "--yes"); code == 0 {
		t.Fatalf("unexpected ordinary-file mutation was adopted: %s %s", stdout, stderr)
	}
	if got := string(testkit.ReadFile(t, filepath.Join(fixture.Workspace, "AGENTS.md"))); got != "host-produced\n" {
		t.Fatal("fresh suffix silently replaced an unplanned host mutation")
	}
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
		t.Fatal("ordinary-file conflict skipped or replayed the command")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiOrdinarySuffixProcessLossRecoversWithoutCommandReplay(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := nativefixture.NewPiInvocation(t, "")
			fixture.AddInstructions(t, "recoverable instructions\n")
			recoveryFixture := nativefixture.RecoveryFixture{Manifest: fixture.Manifest, Destination: filepath.Join(fixture.Workspace, "AGENTS.md")}
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, recoveryFixture, "native-ordinary-"+boundary, "apply", "--yes")
			if code != nativefixture.RecoveryExit {
				t.Fatalf("actual ordinary suffix did not reach its process-loss boundary: %d %s %s", code, stdout, stderr)
			}
			fixture.LaunchProtection(t)
			fixture.Require(t, "recover", "--dry-run")
			fixture.Require(t, "recover", "--yes")
			fixture.Require(t, "apply", "--yes")
			if got := string(testkit.ReadFile(t, recoveryFixture.Destination)); got != "recoverable instructions\n" {
				t.Fatal("suffix recovery lost its ordinary file")
			}
			if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
				t.Fatal("suffix recovery/retry replayed a completed host command")
			}
			fixture.Require(t, "status", "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}

func TestNativePiGlobalKeysPrecedeActualExtensionInvocation(t *testing.T) {
	fixture := nativefixture.NewPiInvocationAtScope(t, "", target.ScopeGlobal)
	fixture.Require(t, "apply", "--yes")
	fixture.LaunchProtection(t)
	var before struct {
		Theme string `json:"theme"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, fixture.BeforeCommand), &before); err != nil || before.Theme != "fixture-theme" {
		t.Fatal("global selected keys were not established before the global command", err)
	}
	fixture.Require(t, "status", "--check", "--json")
	fixture.Require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
		t.Fatal("converged global invocation repeated its command")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiInvocationRetainsTighterPermissions(t *testing.T) {
	fixture := nativefixture.NewPiInvocation(t, "tighten")
	fixture.Require(t, "apply", "--yes")
	info, err := os.Stat(fixture.Settings)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("admitted permission restriction was not retained", err)
	}
	fixture.Require(t, "apply", "--yes")
	fixture.Require(t, "status", "--check", "--json")
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiInvocationCreatesItsInitialNativeState(t *testing.T) {
	fixture := nativefixture.NewPiInvocation(t, "initial")
	paths, err := daempaths.Resolve(fixture.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.StatefilePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.Settings); err != nil {
		t.Fatal(err)
	}
	fixture.Require(t, "apply", "--yes")
	fixture.LaunchProtection(t)
	fixture.Require(t, "status", "--check", "--json")
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiInvocationReportsPendingProtection(t *testing.T) {
	fixture := nativefixture.NewPiInvocation(t, "inaccessible")
	code, stdout, stderr := fixture.Run("apply", "--yes", "--json")
	if code == 0 {
		t.Fatalf("unsupported owner-mode change succeeded: stdout=%s stderr=%s", stdout, stderr)
	}
	row := fixture.LaunchProtection(t)
	var result struct {
		Protection *struct {
			Records []struct {
				Invocation string `json:"invocation"`
			} `json:"observed_records"`
		} `json:"permission_protection"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.Protection == nil || len(result.Protection.Records) != 1 || result.Protection.Records[0].Invocation != row.Invocation().String() {
		t.Fatalf("actual pending invocation is absent from the command result: %s", stdout)
	}
	if code, _, _ := fixture.Run("apply", "--yes"); code == 0 {
		t.Fatal("pending safety work did not block another Apply")
	}
	if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
		t.Fatal("pending safety work allowed another command")
	}
	if err := os.Chmod(fixture.Settings, os.FileMode(row.Documents()[0].ModeCap())); err != nil {
		t.Fatal(err)
	}
	fixture.Require(t, "recover", "--yes")
	testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
}

func TestNativePiInvocationRejectsChangedSettingsWithoutRepair(t *testing.T) {
	for _, mutation := range []string{"key", "outside", "malformed", "mode"} {
		t.Run(mutation, func(t *testing.T) {
			fixture := nativefixture.NewPiInvocation(t, mutation)
			code, stdout, stderr := fixture.Run("apply", "--yes", "--json")
			if code == 0 {
				t.Fatalf("changed settings were accepted: stdout=%s stderr=%s", stdout, stderr)
			}
			protection := fixture.LaunchProtection(t)
			var result struct {
				Attempts []struct {
					Reason   string `json:"reason"`
					ExitCode *int   `json:"exit_code"`
				} `json:"host_route_attempts"`
			}
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Attempts) != 1 || result.Attempts[0].Reason != "observation_unavailable" || result.Attempts[0].ExitCode == nil || *result.Attempts[0].ExitCode != 0 {
				t.Fatalf("qualification failure lost or misclassified mechanical success: %s", stdout)
			}
			if got := string(testkit.ReadFile(t, fixture.Calls)); got != "install\ninstall\n" {
				t.Fatal("failed native qualification repeated its command")
			}
			actual := testkit.ReadFile(t, fixture.Settings)
			if mutation == "key" && !bytes.Contains(actual, []byte(`"theme":"changed"`)) || mutation == "outside" && !bytes.Contains(actual, []byte(`"retained":false`)) || mutation == "malformed" && !bytes.Equal(actual, []byte("[")) {
				t.Fatal("failed qualification silently repaired host-selected values")
			}
			if mutation == "mode" {
				info, err := os.Stat(fixture.Settings)
				if err != nil || uint32(info.Mode().Perm()) != protection.Documents()[0].ModeCap() {
					t.Fatal("expanded permissions were not restricted to their invocation-start cap", err)
				}
			}
			paths, err := daempaths.Resolve(fixture.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			state, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, paths.StatefilePath))
			if err != nil {
				t.Fatal(err)
			}
			launched, err := statefile.DecodePersistedFacts(t.Context(), testkit.ReadFile(t, fixture.LaunchState))
			if err != nil || len(state.PendingCarrierInstalls()) != 1 || !state.PendingCarrierInstalls()[0].ExactEqual(launched.PendingCarrierInstalls()[0]) {
				t.Fatal("failed qualification discarded the exact pending installation", err)
			}
			store, err := ownershipstore.New(paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := store.LoadProtectionRegistry(t.Context())
			if err != nil || len(registry.PendingProtections()) != 0 {
				t.Fatal("completed metadata settlement retained a safety obligation", err)
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.Workspace)
		})
	}
}
