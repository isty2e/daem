//go:build darwin || linux

package nativefixture

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
)

type PiInvocation struct {
	Workspace, Manifest, Settings, BeforeCommand, Calls, LaunchState, LaunchRegistry string
	InstructionsBefore                                                               string
}

func NewPiInvocation(t *testing.T, mutation string) PiInvocation {
	t.Helper()
	return NewPiInvocationAtScope(t, mutation, target.ScopeProject)
}

func NewPiInvocationAtScope(t *testing.T, mutation string, scope target.Scope) PiInvocation {
	t.Helper()
	return NewPiInvocationForSource(t, mutation, scope, "git:github.com/acme/pi-tools@v1")
}

func NewPiInvocationForSource(t *testing.T, mutation string, scope target.Scope, source string) PiInvocation {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	SetRecoveryEnvironment(t, root)
	workspace, bin := filepath.Join(root, "workspace"), filepath.Join(root, "bin")
	manifest := filepath.Join(workspace, "daem.toml")
	settings := filepath.Join(workspace, ".pi", "settings.json")
	if scope == target.ScopeGlobal {
		settings = filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "settings.json")
	}
	beforeCommand, calls := filepath.Join(root, "command-before.json"), filepath.Join(root, "command-calls")
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
  if [ -n "${DAEM_TEST_PIN_CLAIMS:-}" ]; then /bin/cp "$DAEM_TEST_PIN_CLAIMS" "$DAEM_TEST_PIN_LAUNCH_CLAIMS"; fi
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
    pin-parent-loss) /bin/chmod 666 "$DAEM_TEST_SETTINGS"; kill -KILL "${DAEM_TEST_OPAQUE_PARENT_PID:?}" ;;
    ordinary) printf 'host-produced\n' > "$DAEM_TEST_INSTRUCTIONS_TARGET" ;;
  esac
fi
`
	testkit.WriteFile(t, bin, "pi", strings.ReplaceAll(script, "git:github.com/acme/pi-tools@v1", source))
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
	fixture := PiInvocation{Workspace: workspace, Manifest: manifest, Settings: settings, BeforeCommand: beforeCommand, Calls: calls, LaunchState: filepath.Join(root, "launch-state.json"), LaunchRegistry: filepath.Join(root, "launch-registry.json")}
	fixture.InstructionsBefore = filepath.Join(root, "instructions-before")
	t.Setenv("DAEM_TEST_INSTRUCTIONS_BEFORE", fixture.InstructionsBefore)
	t.Setenv("DAEM_TEST_INSTRUCTIONS_TARGET", filepath.Join(workspace, "AGENTS.md"))
	t.Setenv("DAEM_TEST_REGISTRY", paths.OwnershipRegistryPath)
	t.Setenv("DAEM_TEST_LAUNCH_STATE", fixture.LaunchState)
	t.Setenv("DAEM_TEST_LAUNCH_REGISTRY", fixture.LaunchRegistry)
	t.Setenv("DAEM_TEST_MUTATION", mutation)
	t.Setenv("DAEM_TEST_NATIVE_RESULT", "")
	t.Setenv("DAEM_TEST_PIN_CLAIMS", "")
	fixture.Require(t, "lock")
	fixture.Require(t, "add", "extension", "tools", source, "--target", "pi", "--scope", string(scope))
	fixture.Require(t, "lock")
	fixture.Require(t, "apply", "--yes")
	if got := string(testkit.ReadFile(t, calls)); got != "install\n" {
		t.Fatal("real extension-only fixture did not invoke its command once")
	}
	// Model a host-side uninstall while retaining the actual producer's state.
	testkit.WriteFile(t, filepath.Dir(settings), "settings.json", `{"foreign":{"retained":true},"packages":["npm:@acme/keep@1"]}`)
	declaration := string(testkit.ReadFile(t, manifest)) + fmt.Sprintf("\n[[host_config]]\nid='preferences'\nscope=%q\npath='settings.json'\nsource=%q\nownership='keys'\n", scope, filepath.Join(workspace, "preferences.json"))
	testkit.WriteFile(t, workspace, "daem.toml", declaration)
	t.Setenv("DAEM_TEST_INSTALL_PHASE", "native")
	fixture.Require(t, "lock")
	return fixture
}

func (fixture PiInvocation) Run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	args = append(args, "--manifest", fixture.Manifest)
	code := testkit.RunVerboseCLIWithOptions(args, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr})
	return code, stdout.String(), stderr.String()
}

func (fixture PiInvocation) Require(t *testing.T, args ...string) {
	t.Helper()
	if code, stdout, stderr := fixture.Run(args...); code != 0 {
		t.Fatalf("actual CLI %q failed: exit=%d stdout=%s stderr=%s", args, code, stdout, stderr)
	}
}

func (fixture PiInvocation) LaunchProtection(t *testing.T) ownership.PendingProtection {
	t.Helper()
	snapshot, err := statefile.Decode(testkit.ReadFile(t, fixture.LaunchState))
	if err != nil || len(snapshot.PendingCarrierInstalls()) != 1 {
		t.Fatal("actual command was released without its exact pending install", err)
	}
	return fixture.LaunchNativeProtection(t)
}

func (fixture PiInvocation) LaunchNativeProtection(t *testing.T) ownership.PendingProtection {
	t.Helper()
	store, err := ownershipstore.New(fixture.LaunchRegistry)
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

func (fixture PiInvocation) AddInstructions(t *testing.T, content string) {
	t.Helper()
	manifest := string(testkit.ReadFile(t, fixture.Manifest)) + "\n[instructions.project]\nsource='./instructions.md'\n"
	testkit.WriteFile(t, fixture.Workspace, "daem.toml", manifest)
	testkit.WriteFile(t, fixture.Workspace, "instructions.md", content)
	fixture.Require(t, "lock")
}
