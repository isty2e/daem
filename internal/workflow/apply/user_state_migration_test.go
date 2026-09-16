package apply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/migrate"
)

func TestRegistryOnlyGlobalAdoptionCanMigrateUserAuthority(t *testing.T) {
	root, manifest, lockfile, _, locked, subject := writeApplyClaudePluginCarrierCommandFixtureForScope(t, target.ScopeGlobal)
	present := exactClaudeCarrierObservations(t, locked, subject, target.ScopeGlobal)
	planning, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath: manifest, LockfilePath: lockfile, TargetValues: []string{"claude-code"},
		RelationObservations: &present, ManageUnmanagedMatches: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			t.Fatal("state-only adoption invoked a host command")
			return subprocess.CommandResult{}
		},
	})
	if _, err := ExecuteWithOptions(t.Context(), planning, ExecuteOptions{RelationObservations: &present, HostRouteExecutor: executor}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".daem", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("global-only adoption unexpectedly created a snapshot: %v", err)
	}

	config := t.TempDir()
	if err := os.Symlink(root, filepath.Join(config, "daem")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	prepared, err := migrate.PlanState(t.Context(), migrate.StateInput{ManifestPath: manifest})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if prepared.Disclosure().CarrierClaims != 1 {
		t.Fatalf("disclosure = %#v", prepared.Disclosure())
	}
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}

	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.StatefilePath); err != nil {
		t.Fatal(err)
	}
	claims := loadCarrierClaimsForScope(t, root, manifest, target.ScopeGlobal)
	key, err := mutation.CanonicalDirectoryEntryKey(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || !claims[0].MatchesLockedRecord(locked.Locked.Subjects()[0]) || claims[0].Owner().StatefileKey() != key {
		t.Fatal("global-only adoption did not transfer to the selected authority")
	}
}
