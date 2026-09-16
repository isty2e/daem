package recover

import (
	"os"
	"path/filepath"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/workflow/migrate"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func prepareLegacyUserRecoveryFixture(t *testing.T) recoveryFixture {
	t.Helper()
	fixture := prepareRecoveryFixture(t, true)
	configHome := t.TempDir()
	if err := os.Symlink(filepath.Dir(fixture.input.ManifestPath), filepath.Join(configHome, "daem")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	return fixture
}

func TestLegacyUserStateRecoveryRemainsAccessibleAfterSelectionNormalization(t *testing.T) {
	fixture := prepareLegacyUserRecoveryFixture(t)
	if _, err := migrate.PlanState(t.Context(), migrate.StateInput{ManifestPath: fixture.input.ManifestPath}); err == nil {
		t.Fatal("migration ignored legacy apply recovery")
	}
	if _, err := Plan(t.Context(), fixture.input); err == nil {
		t.Fatal("canonical recovery silently selected the old journal")
	}
	input := fixture.input
	input.LegacyUserState = true
	prepared, err := Plan(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if _, err := Execute(t.Context(), prepared, ExecuteOptions{}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(fixture.hostPath)
	if err != nil || string(content) != string(fixture.oldContent) {
		t.Fatalf("legacy recovery did not restore output: %v", err)
	}
	migration, err := migrate.PlanState(t.Context(), migrate.StateInput{ManifestPath: fixture.input.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer migration.Close()
	if _, err := migration.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := Plan(t.Context(), input); err == nil {
		t.Fatal("legacy recovery admitted a retired source")
	}
}

func TestLegacyUserStateRecoveryCannotBypassCanonicalMetadataFence(t *testing.T) {
	fixture := prepareLegacyUserRecoveryFixture(t)
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterruptedForAbsentTarget(t, paths.StateDir, filepath.Join(paths.StateDir, "unrelated"))
	input := fixture.input
	input.LegacyUserState = true
	if _, err := Plan(t.Context(), input); err == nil {
		t.Fatal("legacy recovery bypassed canonical recovery")
	}
	content, err := os.ReadFile(fixture.hostPath)
	if err != nil || string(content) != string(fixture.newContent) {
		t.Fatalf("blocked legacy recovery changed outputs: %v", err)
	}
}

func TestLegacyUserStateRecoveryRejectsOrdinaryProject(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	input := fixture.input
	input.LegacyUserState = true
	if _, err := Plan(t.Context(), input); err == nil {
		t.Fatal("legacy selector redirected ordinary project recovery")
	}
	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Close()
}
