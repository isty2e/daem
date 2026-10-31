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
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

type nativePiInvocationFixture struct {
	workspace, manifest, settings, beforeCommand, calls, launchState, launchRegistry string
	instructionsBefore                                                               string
}

func newNativePiInvocationFixture(t *testing.T, mutation string) nativePiInvocationFixture {
	t.Helper()
	return newNativePiInvocationFixtureAtScope(t, mutation, target.ScopeProject)
}

func newNativePiInvocationFixtureAtScope(t *testing.T, mutation string, scope target.Scope) nativePiInvocationFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setNativeRecoveryEnvironment(t, root)
	workspace, bin := filepath.Join(root, "workspace"), filepath.Join(root, "bin")
	manifest := filepath.Join(workspace, "daem.toml")
	settings := filepath.Join(workspace, ".pi", "settings.json")
	if scope == target.ScopeGlobal {
		settings = filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "settings.json")
	}
	beforeCommand, calls := filepath.Join(root, "command-before.json"), filepath.Join(root, "command-calls")
	const source = "git:github.com/acme/pi-tools@v1"
	testkit.WriteFile(t, workspace, "daem.toml", "version=1\ntargets=['pi']\n")
	testkit.WriteFile(t, workspace, "preferences.json", `{"theme":"fixture-theme"}`)
	testkit.WriteFile(t, filepath.Dir(settings), "settings.json", `{"foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
	script := `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.0.2\n'; exit 0; fi
[ "$1" = install ] && [ "$2" = 'git:github.com/acme/pi-tools@v1' ] || exit 97
if [ "$DAEM_TEST_SCOPE" = project ]; then
  [ "$#" -eq 3 ] && [ "$3" = -l ] || exit 97
else
  [ "$#" -eq 2 ] || exit 97
fi
if [ -f "$DAEM_TEST_SETTINGS" ]; then
  /bin/cp "$DAEM_TEST_SETTINGS" "$DAEM_TEST_COMMAND_BEFORE"
else
  printf '{}\n' > "$DAEM_TEST_COMMAND_BEFORE"
fi
if [ -f "$DAEM_TEST_INSTRUCTIONS_TARGET" ]; then
  { printf 'present\n'; /bin/cat "$DAEM_TEST_INSTRUCTIONS_TARGET"; } > "$DAEM_TEST_INSTRUCTIONS_BEFORE"
else
  printf 'absent\n' > "$DAEM_TEST_INSTRUCTIONS_BEFORE"
fi
printf 'install\n' >> "$DAEM_TEST_COMMAND_CALLS"
if [ "$DAEM_TEST_INSTALL_PHASE" = baseline ]; then
  printf '%s\n' '{"foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}' > "$DAEM_TEST_SETTINGS"
else
  /bin/cp .daem/state.json "$DAEM_TEST_LAUNCH_STATE"
  /bin/cp "$DAEM_TEST_REGISTRY" "$DAEM_TEST_LAUNCH_REGISTRY"
  printf '%s\n' '{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}' > "$DAEM_TEST_SETTINGS"
  if [ -n "${DAEM_TEST_NATIVE_RESULT:-}" ]; then /bin/cp "$DAEM_TEST_NATIVE_RESULT" "$DAEM_TEST_SETTINGS"; fi
  case "$DAEM_TEST_MUTATION" in
    initial) printf '%s\n' '{"theme":"fixture-theme","packages":["git:github.com/acme/pi-tools@v1"]}' > "$DAEM_TEST_SETTINGS" ;;
    key) printf '%s\n' '{"theme":"changed","foreign":{"retained":true},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}' > "$DAEM_TEST_SETTINGS" ;;
    outside) printf '%s\n' '{"theme":"fixture-theme","foreign":{"retained":false},"packages":["npm:@acme/keep@1","git:github.com/acme/pi-tools@v1"]}' > "$DAEM_TEST_SETTINGS" ;;
    malformed) printf '[' > "$DAEM_TEST_SETTINGS" ;;
    mode) /bin/chmod 666 "$DAEM_TEST_SETTINGS" ;;
    tighten) /bin/chmod 600 "$DAEM_TEST_SETTINGS" ;;
    inaccessible) /bin/chmod 000 "$DAEM_TEST_SETTINGS" ;;
    command) exit 23 ;;
    ordinary) printf 'host-produced\n' > "$DAEM_TEST_INSTRUCTIONS_TARGET" ;;
  esac
fi
`
	testkit.WriteFile(t, bin, "pi", script)
	if err := os.Chmod(filepath.Join(bin, "pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DAEM_TEST_COMMAND_BEFORE", beforeCommand)
	t.Setenv("DAEM_TEST_COMMAND_CALLS", calls)
	t.Setenv("DAEM_TEST_INSTALL_PHASE", "baseline")
	t.Setenv("DAEM_TEST_SCOPE", string(scope))
	t.Setenv("DAEM_TEST_SETTINGS", settings)
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture := nativePiInvocationFixture{workspace: workspace, manifest: manifest, settings: settings, beforeCommand: beforeCommand, calls: calls, launchState: filepath.Join(root, "launch-state.json"), launchRegistry: filepath.Join(root, "launch-registry.json")}
	fixture.instructionsBefore = filepath.Join(root, "instructions-before")
	t.Setenv("DAEM_TEST_INSTRUCTIONS_BEFORE", fixture.instructionsBefore)
	t.Setenv("DAEM_TEST_INSTRUCTIONS_TARGET", filepath.Join(workspace, "AGENTS.md"))
	t.Setenv("DAEM_TEST_REGISTRY", paths.OwnershipRegistryPath)
	t.Setenv("DAEM_TEST_LAUNCH_STATE", fixture.launchState)
	t.Setenv("DAEM_TEST_LAUNCH_REGISTRY", fixture.launchRegistry)
	t.Setenv("DAEM_TEST_MUTATION", mutation)
	t.Setenv("DAEM_TEST_NATIVE_RESULT", "")
	fixture.require(t, "lock")
	fixture.require(t, "add", "extension", "tools", source, "--target", "pi", "--scope", string(scope))
	fixture.require(t, "lock")
	fixture.require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, calls)); got != "install\n" {
		t.Fatal("real extension-only fixture did not invoke its command once")
	}
	// Model a host-side uninstall while retaining the actual producer's state.
	testkit.WriteFile(t, filepath.Dir(settings), "settings.json", `{"foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
	declaration := string(testkit.ReadFile(t, manifest)) + fmt.Sprintf("\n[[host_config]]\nid='preferences'\nscope=%q\npath='settings.json'\nsource=%q\nownership='keys'\n", scope, filepath.Join(workspace, "preferences.json"))
	testkit.WriteFile(t, workspace, "daem.toml", declaration)
	t.Setenv("DAEM_TEST_INSTALL_PHASE", "native")
	fixture.require(t, "lock")
	return fixture
}

func (fixture nativePiInvocationFixture) run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	args = append(args, "--manifest", fixture.manifest)
	code := testkit.RunVerboseCLIWithOptions(args, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr})
	return code, stdout.String(), stderr.String()
}

func (fixture nativePiInvocationFixture) require(t *testing.T, args ...string) {
	t.Helper()
	if code, stdout, stderr := fixture.run(args...); code != 0 {
		t.Fatalf("actual CLI %q failed: exit=%d stdout=%s stderr=%s", args, code, stdout, stderr)
	}
}

func (fixture nativePiInvocationFixture) launchProtection(t *testing.T) ownership.PendingProtection {
	t.Helper()
	snapshot, err := statefile.Decode(testkit.ReadFile(t, fixture.launchState))
	if err != nil || len(snapshot.PendingCarrierInstalls()) != 1 {
		t.Fatal("actual command was released without its exact pending install", err)
	}
	store, err := ownershipstore.New(fixture.launchRegistry)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(registry.PendingProtections()) != 1 {
		t.Fatal("actual command was released without a durable safety record", err)
	}
	row := registry.PendingProtections()[0]
	if _, bound := row.Execution(); !bound || row.Phase() != ownership.ProtectionBound {
		t.Fatal("actual command was released before its execution binding was durable")
	}
	return row
}

func TestNativePiKeysPrecedeActualExtensionInvocation(t *testing.T) {
	fixture := newNativePiInvocationFixture(t, "")
	code, stdout, stderr := fixture.run("apply", "--yes", "--json")
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
	fixture.launchProtection(t)
	type fixtureSettings struct {
		Theme   string `json:"theme"`
		Foreign struct {
			Retained bool `json:"retained"`
		} `json:"foreign"`
	}
	var before, after fixtureSettings
	if err := json.Unmarshal(testkit.ReadFile(t, fixture.beforeCommand), &before); err != nil || before.Theme != "fixture-theme" || !before.Foreign.Retained {
		t.Fatal("selected keys were not settled before the actual host command", err)
	}
	if err := json.Unmarshal(testkit.ReadFile(t, fixture.settings), &after); err != nil || after.Theme != "fixture-theme" || !after.Foreign.Retained {
		t.Fatal("actual command did not preserve selected and unrelated values", err)
	}
	if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\n" {
		t.Fatal("mixed execution did not invoke exactly its selected command")
	}
	fixture.require(t, "status", "--check", "--json")
	fixture.require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\n" {
		t.Fatal("converged mixed execution replayed the host command")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiInvocationWithInitiallyCurrentKeys(t *testing.T) {
	fixture := newNativePiInvocationFixture(t, "")
	fixture.require(t, "apply", "--yes")
	testkit.WriteFile(t, fixture.workspace, ".pi/settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
	fixture.require(t, "apply", "--yes")
	fixture.launchProtection(t)
	if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\ninstall\n" {
		t.Fatal("initially-current keys suppressed or duplicated their required host invocation")
	}
	fixture.require(t, "status", "--check", "--json")
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func (fixture nativePiInvocationFixture) addInstructions(t *testing.T, content string) {
	t.Helper()
	manifest := string(testkit.ReadFile(t, fixture.manifest)) + "\n[instructions.project]\nsource='./instructions.md'\n"
	testkit.WriteFile(t, fixture.workspace, "daem.toml", manifest)
	testkit.WriteFile(t, fixture.workspace, "instructions.md", content)
	fixture.require(t, "lock")
}

func TestNativePiInvocationWithOrdinaryFileChanges(t *testing.T) {
	fixture := newNativePiInvocationFixture(t, "")
	fixture.addInstructions(t, "first instructions\n")
	for index, content := range []string{"first instructions\n", "updated instructions\n"} {
		if index != 0 {
			testkit.WriteFile(t, fixture.workspace, "instructions.md", content)
			testkit.WriteFile(t, filepath.Dir(fixture.settings), "settings.json", `{"theme":"fixture-theme","foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
			fixture.require(t, "lock")
		}
		fixture.require(t, "apply", "--yes")
		fixture.launchProtection(t)
		beforeInstructions := "absent\n"
		if index != 0 {
			beforeInstructions = "present\nfirst instructions\n"
		}
		if got := string(testkit.ReadFile(t, fixture.instructionsBefore)); got != beforeInstructions {
			t.Fatal("ordinary file was changed before the host command")
		}
		if got := string(testkit.ReadFile(t, filepath.Join(fixture.workspace, "AGENTS.md"))); got != content {
			t.Fatal("mixed installation did not settle its ordinary file")
		}
		var before struct {
			Theme string `json:"theme"`
		}
		if err := json.Unmarshal(testkit.ReadFile(t, fixture.beforeCommand), &before); err != nil || before.Theme != "fixture-theme" {
			t.Fatal("mixed installation did not establish its keys before the command", err)
		}
		fixture.require(t, "status", "--check", "--json")
		calls := string(testkit.ReadFile(t, fixture.calls))
		fixture.require(t, "apply", "--yes")
		if got := string(testkit.ReadFile(t, fixture.calls)); got != calls {
			t.Fatal("converged mixed installation replayed its command")
		}
	}
	if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\ninstall\n" {
		t.Fatal("ordinary file changes suppressed or duplicated their selected installations")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiInvocationFailureStopsOrdinaryFileChanges(t *testing.T) {
	for _, mutation := range []string{"key", "command"} {
		t.Run(mutation, func(t *testing.T) {
			fixture := newNativePiInvocationFixture(t, mutation)
			fixture.addInstructions(t, "pending instructions\n")
			if code, stdout, stderr := fixture.run("apply", "--yes"); code == 0 {
				t.Fatalf("failed invocation was accepted with pending ordinary work: %s %s", stdout, stderr)
			}
			fixture.launchProtection(t)
			if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\n" {
				t.Fatal("failed invocation was skipped or replayed")
			}
			if _, err := os.Stat(filepath.Join(fixture.workspace, "AGENTS.md")); !os.IsNotExist(err) {
				t.Fatal("failed invocation did not stop the ordinary file write", err)
			}
			paths, err := daempaths.Resolve(fixture.manifest)
			if err != nil {
				t.Fatal(err)
			}
			state, err := statefile.Decode(testkit.ReadFile(t, paths.StatefilePath))
			if err != nil || len(state.ManagedPaths()) != 0 {
				t.Fatal("failed invocation claimed an unapplied ordinary file", err)
			}
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}

func TestNativePiInvocationCreatesGlobalRootBeforeOrdinaryAcquisition(t *testing.T) {
	fixture := newNativePiInvocationFixtureAtScope(t, "initial", target.ScopeGlobal)
	agentRoot := filepath.Join(os.Getenv("HOME"), ".pi", "agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	fixture.settings = filepath.Join(agentRoot, "settings.json")
	t.Setenv("DAEM_TEST_SETTINGS", fixture.settings)
	t.Setenv("DAEM_TEST_INSTRUCTIONS_TARGET", filepath.Join(agentRoot, "AGENTS.md"))
	source := filepath.Join(fixture.workspace, "instructions.md")
	testkit.WriteFile(t, fixture.workspace, "instructions.md", "global instructions\n")
	manifest := string(testkit.ReadFile(t, fixture.manifest)) + fmt.Sprintf("\n[instructions.global]\nsource=%q\n", source)
	testkit.WriteFile(t, fixture.workspace, "daem.toml", manifest)
	if err := os.RemoveAll(filepath.Dir(fixture.settings)); err != nil {
		t.Fatal(err)
	}
	fixture.require(t, "lock")
	fixture.require(t, "apply", "--yes")
	fixture.launchProtection(t)
	if got := string(testkit.ReadFile(t, fixture.instructionsBefore)); got != "absent\n" {
		t.Fatal("global ordinary file was created before the host command")
	}
	if got := string(testkit.ReadFile(t, filepath.Join(filepath.Dir(fixture.settings), "AGENTS.md"))); got != "global instructions\n" {
		t.Fatal("post-prefix global acquisition lost the ordinary file")
	}
	fixture.require(t, "status", "--check", "--json")
	fixture.require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\n" {
		t.Fatal("global acquisition replayed the completed command")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiInvocationWithSkillDirectoryChanges(t *testing.T) {
	fixture := newNativePiInvocationFixture(t, "")
	source := filepath.Join(fixture.workspace, "sample")
	const skill = "---\nname: sample\ndescription: mixed invocation fixture\n---\n"
	testkit.WriteFile(t, source, "SKILL.md", skill)
	testkit.WriteFile(t, source, "references/detail.txt", "skill detail\n")
	manifest := string(testkit.ReadFile(t, fixture.manifest)) + "\n[[skill]]\nname='sample'\nscope='project'\nsource={path='./sample',mode='vendor'}\n"
	testkit.WriteFile(t, fixture.workspace, "daem.toml", manifest)
	fixture.require(t, "lock")
	fixture.require(t, "apply", "--yes")
	for path, expected := range map[string]string{"SKILL.md": skill, "references/detail.txt": "skill detail\n"} {
		if got := string(testkit.ReadFile(t, filepath.Join(fixture.workspace, ".pi", "skills", "sample", path))); got != expected {
			t.Fatal("mixed invocation lost ordinary directory content", path)
		}
	}
	fixture.require(t, "status", "--check", "--json")
	fixture.require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\n" {
		t.Fatal("directory convergence replayed its command")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiInvocationDoesNotAdoptChangedOrdinaryFile(t *testing.T) {
	fixture := newNativePiInvocationFixture(t, "ordinary")
	fixture.addInstructions(t, "planned instructions\n")
	if code, stdout, stderr := fixture.run("apply", "--yes"); code == 0 {
		t.Fatalf("unexpected ordinary-file mutation was adopted: %s %s", stdout, stderr)
	}
	if got := string(testkit.ReadFile(t, filepath.Join(fixture.workspace, "AGENTS.md"))); got != "host-produced\n" {
		t.Fatal("fresh suffix silently replaced an unplanned host mutation")
	}
	if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\n" {
		t.Fatal("ordinary-file conflict skipped or replayed the command")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiOrdinarySuffixProcessLossRecoversWithoutCommandReplay(t *testing.T) {
	for _, boundary := range []string{"host", "state"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newNativePiInvocationFixture(t, "")
			fixture.addInstructions(t, "recoverable instructions\n")
			recoveryFixture := nativeRecoveryFixture{manifest: fixture.manifest, destination: filepath.Join(fixture.workspace, "AGENTS.md")}
			code, stdout, stderr := invokeNativeRecoveryCommand(t, recoveryFixture, "native-ordinary-"+boundary, "apply", "--yes")
			if code != nativeRecoveryExit {
				t.Fatalf("actual ordinary suffix did not reach its process-loss boundary: %d %s %s", code, stdout, stderr)
			}
			fixture.launchProtection(t)
			fixture.require(t, "recover", "--dry-run")
			fixture.require(t, "recover", "--yes")
			fixture.require(t, "apply", "--yes")
			if got := string(testkit.ReadFile(t, recoveryFixture.destination)); got != "recoverable instructions\n" {
				t.Fatal("suffix recovery lost its ordinary file")
			}
			if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\n" {
				t.Fatal("suffix recovery/retry replayed a completed host command")
			}
			fixture.require(t, "status", "--check", "--json")
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}

func TestNativePiGlobalKeysPrecedeActualExtensionInvocation(t *testing.T) {
	fixture := newNativePiInvocationFixtureAtScope(t, "", target.ScopeGlobal)
	fixture.require(t, "apply", "--yes")
	fixture.launchProtection(t)
	var before struct {
		Theme string `json:"theme"`
	}
	if err := json.Unmarshal(testkit.ReadFile(t, fixture.beforeCommand), &before); err != nil || before.Theme != "fixture-theme" {
		t.Fatal("global selected keys were not established before the global command", err)
	}
	fixture.require(t, "status", "--check", "--json")
	fixture.require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\n" {
		t.Fatal("converged global invocation repeated its command")
	}
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiInvocationRetainsTighterPermissions(t *testing.T) {
	fixture := newNativePiInvocationFixture(t, "tighten")
	fixture.require(t, "apply", "--yes")
	info, err := os.Stat(fixture.settings)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("admitted permission restriction was not retained", err)
	}
	fixture.require(t, "apply", "--yes")
	fixture.require(t, "status", "--check", "--json")
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiInvocationCreatesItsInitialNativeState(t *testing.T) {
	fixture := newNativePiInvocationFixture(t, "initial")
	paths, err := daempaths.Resolve(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.StatefilePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.settings); err != nil {
		t.Fatal(err)
	}
	fixture.require(t, "apply", "--yes")
	fixture.launchProtection(t)
	fixture.require(t, "status", "--check", "--json")
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiInvocationReportsPendingProtection(t *testing.T) {
	fixture := newNativePiInvocationFixture(t, "inaccessible")
	code, stdout, stderr := fixture.run("apply", "--yes", "--json")
	if code == 0 {
		t.Fatalf("unsupported owner-mode change succeeded: stdout=%s stderr=%s", stdout, stderr)
	}
	row := fixture.launchProtection(t)
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
	if code, _, _ := fixture.run("apply", "--yes"); code == 0 {
		t.Fatal("pending safety work did not block another Apply")
	}
	if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\n" {
		t.Fatal("pending safety work allowed another command")
	}
	if err := os.Chmod(fixture.settings, os.FileMode(row.Documents()[0].ModeCap())); err != nil {
		t.Fatal(err)
	}
	fixture.require(t, "recover", "--yes")
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativePiInvocationRejectsChangedSettingsWithoutRepair(t *testing.T) {
	for _, mutation := range []string{"key", "outside", "malformed", "mode"} {
		t.Run(mutation, func(t *testing.T) {
			fixture := newNativePiInvocationFixture(t, mutation)
			code, stdout, stderr := fixture.run("apply", "--yes", "--json")
			if code == 0 {
				t.Fatalf("changed settings were accepted: stdout=%s stderr=%s", stdout, stderr)
			}
			protection := fixture.launchProtection(t)
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
			if got := string(testkit.ReadFile(t, fixture.calls)); got != "install\ninstall\n" {
				t.Fatal("failed native qualification repeated its command")
			}
			actual := testkit.ReadFile(t, fixture.settings)
			if mutation == "key" && !bytes.Contains(actual, []byte(`"theme":"changed"`)) || mutation == "outside" && !bytes.Contains(actual, []byte(`"retained":false`)) || mutation == "malformed" && !bytes.Equal(actual, []byte("[")) {
				t.Fatal("failed qualification silently repaired host-selected values")
			}
			if mutation == "mode" {
				info, err := os.Stat(fixture.settings)
				if err != nil || uint32(info.Mode().Perm()) != protection.Documents()[0].ModeCap() {
					t.Fatal("expanded permissions were not restricted to their invocation-start cap", err)
				}
			}
			paths, err := daempaths.Resolve(fixture.manifest)
			if err != nil {
				t.Fatal(err)
			}
			state, err := statefile.Decode(testkit.ReadFile(t, paths.StatefilePath))
			if err != nil {
				t.Fatal(err)
			}
			launched, err := statefile.Decode(testkit.ReadFile(t, fixture.launchState))
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
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}
