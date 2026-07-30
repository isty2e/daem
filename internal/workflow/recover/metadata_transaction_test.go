package recover

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestRecoverRefusesInterruptedMetadataTransaction(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)

	_, err = Plan(context.Background(), PlanInput{ManifestPath: manifestPath})
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
}

func TestCleanupOnlyRecoveryIgnoresUnrelatedMetadataTransaction(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhaseFinalizing, true)
	metadatatx.WriteInterrupted(t, fixture.paths.StateDir)

	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatalf("Plan cleanup-only recovery: %v", err)
	}
	if got := prepared.Disclosure().AuthorityKind(); got != journal.RecoveryAuthorityJournalCleanup {
		t.Fatalf("authority kind = %q, want journal cleanup", got)
	}
	if err := Execute(t.Context(), prepared); err != nil {
		t.Fatalf("Execute cleanup-only recovery: %v", err)
	}
	assertRecoverPathAbsent(t, fixture.controlDir)
	assertRecoverPathAbsent(t, fixture.residueDir)
}

func TestBlockedJournalPreservesMetadataTransactionPrecedence(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhaseFinalizing, true)
	foreign := filepath.Join(fixture.controlDir, "foreign")
	if err := os.WriteFile(foreign, []byte("foreign\n"), retirement.RecordMode); err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, fixture.paths.StateDir)

	_, err := Plan(t.Context(), fixture.input)
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("Plan error = %v, want metadata transaction precedence", err)
	}
	assertRecoverPathPresent(t, fixture.controlDir)
	assertRecoverPathPresent(t, fixture.residueDir)
	assertRecoverPathAbsent(t, fixture.garbageDir)
}
