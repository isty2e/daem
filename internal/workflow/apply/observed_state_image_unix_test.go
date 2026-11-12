//go:build darwin || linux

package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestApplyWorkflowsPreserveObservedStateImageAndAlias(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "ordinary"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			for key, directory := range map[string]string{
				"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state",
				"XDG_DATA_HOME": "data", "PI_CODING_AGENT_DIR": "pi", "CODEX_HOME": "codex",
			} {
				t.Setenv(key, filepath.Join(base, directory))
			}
			root := filepath.Join(base, "workspace")
			paths := applyTestPaths(t, root)
			host := filepath.Join(root, "AGENTS.md")
			if native {
				writeApplyFile(t, paths.ManifestPath, "version=1\ntargets=['pi']\n[[host_config]]\nid='preferences'\nscope='project'\npath='settings.json'\nsource='./preferences.json'\nownership='keys'\n")
				writeApplyFile(t, filepath.Join(root, "preferences.json"), `{"theme":"located-state"}`)
				if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: paths.ManifestPath}); err != nil {
					t.Fatal(err)
				}
				host = filepath.Join(root, ".pi", "settings.json")
			} else {
				writeApplyManifestFile(t, paths.ManifestPath)
				source := filepath.Join(root, "instructions", "AGENTS.md")
				writeApplyFile(t, source, "image-preserving instruction\n")
				writeApplyLockfile(t, paths.LockfilePath, applyInstructionLockfile(t, "project", "local:instructions/AGENTS.md?mode=vendor", hashApplyPath(t, source)))
			}
			reference := seedWorkflowStateImage(t, paths)
			prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath, LockfilePath: paths.LockfilePath})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if prepared.Reconciliation.ContainsHostConfig() != native {
				t.Fatal("fixture did not select its intended apply writer")
			}
			result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{PlanWasDisclosed: true})
			if err != nil || result.ActionCount != 1 {
				t.Fatal("observed image workflow did not perform its necessary write", err)
			}
			if _, err := os.Stat(host); err != nil {
				t.Fatal("workflow did not publish the selected host effect", err)
			}
			content, err := os.ReadFile(paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			image, err := statefile.UnmarshalImage(content)
			if err != nil || !image.Reference().Equal(reference) {
				t.Fatal("workflow downgraded State or replaced its observed alias", err)
			}
			if !native && image.Snapshot().NativeDocumentCommit() != 21 {
				t.Fatal("ordinary apply changed unrelated native commit facts")
			}
		})
	}
}

func seedWorkflowStateImage(t *testing.T, paths daempaths.Paths) pathauthority.Reference {
	t.Helper()
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(paths.ManifestRoot, "state-lookup")
	if err := os.Symlink(paths.StateDir, alias); err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := pathauthority.NewReference(observed.Exact(), filepath.Join(alias, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 21})
	if err != nil {
		t.Fatal(err)
	}
	image, err := statefile.NewStateImage(snapshot, reference)
	if err != nil {
		t.Fatal(err)
	}
	content, err := statefile.MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StatefilePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return reference
}
