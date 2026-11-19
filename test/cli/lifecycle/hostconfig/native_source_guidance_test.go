package cli_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestApplyExplainsStaleAgentConfigSourceForSelectedManifest(t *testing.T) {
	root := t.TempDir()
	nativefixture.SetRecoveryEnvironment(t, root)
	defaultProject := filepath.Join(root, "default-project")
	selectedProject := filepath.Join(root, "selected project")
	const manifest = "daem.toml"
	const source = "local-prefs-source.toml"
	const changed = "model = 'private-updated-value'\n"
	testkit.WriteFile(t, defaultProject, manifest, "version = 1\ntargets = ['codex']\n")
	testkit.WriteFile(t, selectedProject, manifest, `version = 1
targets = ['codex']

[[agent_config]]
id = 'a-unchanged'
path = 'unchanged.toml'
source = 'unchanged-source.toml'

[[agent_config]]
id = 'z-preferences'
path = 'prefs.toml'
source = 'local-prefs-source.toml'
`)
	testkit.WriteFile(t, selectedProject, "unchanged-source.toml", "model = 'stable-value'\n")
	testkit.WriteFile(t, selectedProject, source, "model = 'private-original-value'\n")

	t.Chdir(defaultProject)
	selector := filepath.Join("..", filepath.Base(selectedProject), manifest)
	selectedArgs := func(args ...string) []string { return append(args, "--manifest", selector) }
	nativefixture.RunLifecycleCommand(t, "lock")
	nativefixture.RunLifecycleCommand(t, selectedArgs("lock")...)
	nativefixture.RunLifecycleCommand(t, selectedArgs("apply", "--yes")...)

	paths, err := daempaths.Resolve(selector)
	if err != nil {
		t.Fatal(err)
	}
	defaultPaths, err := daempaths.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(selectedProject, ".codex", "prefs.toml")
	protected := []string{
		live, filepath.Join(selectedProject, ".codex", "unchanged.toml"),
		paths.LockfilePath, paths.StatefilePath, paths.OwnershipRegistryPath, paths.RecoveryDir,
		defaultPaths.LockfilePath,
	}
	testkit.WriteFile(t, selectedProject, source, changed)
	before := nativefixture.SnapshotRecoveryPaths(t, protected...)

	const cause = "the lockfile no longer matches agent-config/z-preferences"
	const explicitAdvice = "from the same directory and environment, run daem lock --dry-run with the same --manifest argument. If the changes are expected, run daem lock with that argument, then preview apply again."
	for _, mode := range []struct{ flag, prefix string }{
		{"--dry-run", "apply preview failed: "}, {"--yes", "apply failed: "},
	} {
		t.Run(mode.flag, func(t *testing.T) {
			message := mode.prefix + cause
			code, stdout, stderr := testkit.RunCapturedCLI(selectedArgs("apply", mode.flag)...)
			if code != 1 || stdout != "" || stderr != message+"\nnext: "+explicitAdvice+"\n" {
				t.Fatalf("want a path-neutral human refusal with relock guidance: %d\n%s%s", code, stdout, stderr)
			}

			code, stdout, stderr = testkit.RunCapturedCLI(selectedArgs("apply", mode.flag, "--json")...)
			if code != 1 || stderr != "" {
				t.Fatalf("want the common operation failure in JSON: %d\n%s%s", code, stdout, stderr)
			}
			failure := clijson.DecodeFailure(t, stdout, "apply", "operation")
			if failure.Error.Message != message || len(failure.Guidance) != 1 || failure.Guidance[0].Kind != "instruction" || failure.Guidance[0].Message != explicitAdvice {
				t.Fatalf("JSON did not preserve the human cause and advice: %+v", failure)
			}
			if !maps.Equal(before, nativefixture.SnapshotRecoveryPaths(t, protected...)) {
				t.Fatal("refused apply changed a managed output, lock, State, Registry or journal")
			}
		})
	}

	t.Run("implicit manifest with verbose evidence", func(t *testing.T) {
		t.Chdir(selectedProject)
		code, stdout, stderr := testkit.RunCapturedCLI("apply", "--dry-run", "--verbose")
		const implicitAdvice = "from the same directory and environment, run daem lock --dry-run. If the changes are expected, run daem lock, then preview apply again."
		const evidence = `apply failure evidence: "native source differs from its locked intent"`
		want := "apply preview failed: " + cause + "\nnext: " + implicitAdvice + "\n" + evidence + "\n"
		if code != 1 || stdout != "" || stderr != want {
			t.Fatalf("want one implicit-manifest instruction followed by existing verbose evidence: %d\n%s%s", code, stdout, stderr)
		}
	})

	if err := os.Remove(filepath.Join(selectedProject, source)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := testkit.RunCapturedCLI(selectedArgs("apply", "--dry-run")...)
	if code != 1 || stdout != "" || strings.Contains(stderr, cause) || strings.Contains(stderr, "daem lock --dry-run") {
		t.Fatalf("an unreadable source must not be diagnosed as a stale lock: %d\n%s%s", code, stdout, stderr)
	}
	testkit.WriteFile(t, selectedProject, source, changed)

	nativefixture.RunLifecycleCommand(t, selectedArgs("lock", "--dry-run")...)
	if !maps.Equal(before, nativefixture.SnapshotRecoveryPaths(t, protected...)) {
		t.Fatal("refusal or lock preview changed protected files")
	}
	nativefixture.RunLifecycleCommand(t, selectedArgs("lock")...)
	nativefixture.RunLifecycleCommand(t, selectedArgs("apply", "--yes")...)
	nativefixture.RunLifecycleCommand(t, selectedArgs("status", "--check")...)
	nativefixture.RequireSharedValues(t, configdocument.TOML, testkit.ReadFile(t, live), changed, true)
	if got := nativefixture.SnapshotRecoveryPaths(t, defaultPaths.LockfilePath); got[defaultPaths.LockfilePath] != before[defaultPaths.LockfilePath] {
		t.Fatal("the repair changed the default project's lockfile")
	}
	testkit.AssertNoRecoveryArtifacts(t, selectedProject)
}
