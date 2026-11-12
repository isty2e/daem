//go:build darwin || linux

package refresh

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestRefreshWorkflowPreservesObservedStateImageAndAlias(t *testing.T) {
	base := t.TempDir()
	for key, directory := range map[string]string{
		"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_DATA_HOME": "data",
	} {
		t.Setenv(key, filepath.Join(base, directory))
	}
	manifest := writeNoObserverRefreshFixture(t)
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
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
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 17})
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
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest, ExtensionID: "formatter"}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	calls := 0
	result, err := Execute(t.Context(), prepared, ExecuteOptions{CommandOptions: subprocess.CommandOptions{
		Runner: func(_ context.Context, _ subprocess.CommandRequest) subprocess.CommandResult {
			calls++
			return subprocess.CommandResult{Started: true, HasExitCode: true}
		},
	}})
	if err != nil || calls != 1 || !result.AttemptHistory.Persisted {
		t.Fatal("refresh did not publish its observed attempt", err)
	}
	content, err = os.ReadFile(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	after, err := statefile.UnmarshalImage(content)
	if err != nil || !after.Reference().Equal(reference) || len(after.Snapshot().HostRouteAttempts()) != 1 || after.Snapshot().NativeDocumentCommit() != 17 {
		t.Fatal("refresh downgraded State, replaced its alias or lost facts", err)
	}
}
