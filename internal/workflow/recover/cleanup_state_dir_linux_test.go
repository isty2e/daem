//go:build linux

package recover

import (
	"os"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
)

func TestCleanupOnlyRecoveryDoesNotRequireStateDirEnumeration(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhaseFinalizing, true)
	makeCleanupStateDirSearchOnly(t, fixture.paths.StateDir)

	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatalf("Plan cleanup-only recovery: %v", err)
	}
	if got := prepared.Disclosure().AuthorityKind(); got != journal.RecoveryAuthorityJournalCleanup {
		t.Fatalf("authority kind = %q, want cleanup-only", got)
	}
	if _, err := Execute(t.Context(), prepared, ExecuteOptions{}); err != nil {
		t.Fatalf("Execute cleanup-only recovery: %v", err)
	}
	assertRecoverPathAbsent(t, fixture.controlDir)
	assertRecoverPathAbsent(t, fixture.residueDir)
}

func TestCleanupOnlyRecoveryReplanAndTerminalClassificationUseSearchOnlyStateDir(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhaseFinalizing, true)
	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatalf("Plan cleanup-only recovery: %v", err)
	}
	makeCleanupStateDirSearchOnly(t, fixture.paths.StateDir)

	if _, err := Execute(t.Context(), prepared, ExecuteOptions{}); err != nil {
		t.Fatalf("Execute cleanup-only recovery after StateDir permission change: %v", err)
	}
	assertRecoverPathAbsent(t, fixture.controlDir)
	assertRecoverPathAbsent(t, fixture.residueDir)
}

func makeCleanupStateDirSearchOnly(t *testing.T, stateDir string) {
	t.Helper()
	if err := os.Chmod(stateDir, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })
}
