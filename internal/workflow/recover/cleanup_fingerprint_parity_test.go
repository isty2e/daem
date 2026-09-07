package recover

import (
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestCompiledCleanupRecoveryFingerprintMatchesLegacyProjection(t *testing.T) {
	for _, test := range []struct {
		name           string
		phase          retirement.Phase
		residuePresent bool
	}{
		{name: "prepared residue", phase: retirement.PhasePrepared, residuePresent: true},
		{name: "finalizing without residue", phase: retirement.PhaseFinalizing, residuePresent: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareCleanupRecoveryFixture(t, test.phase, test.residuePresent)
			prepared, err := Plan(t.Context(), fixture.input)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = prepared.Close() })
			planned := prepared.lifecycle.planned
			cleanup, ok := journal.JournalCleanupPlan(planned.plan)
			if !ok {
				t.Fatalf("authority kind = %q, want cleanup", planned.plan.AuthorityKind())
			}
			compiled, err := cleanupRecoveryOperationFingerprint(planned.paths, cleanup)
			if err != nil {
				t.Fatal(err)
			}
			legacy, err := legacyCleanupRecoveryFingerprint(planned.paths, cleanup)
			if err != nil {
				t.Fatal(err)
			}
			if !compiled.Equal(legacy) || !planned.operationEvidence.Equal(legacy) {
				t.Fatal("compiled cleanup fingerprint differs from the legacy projection")
			}
		})
	}
}

func legacyCleanupRecoveryFingerprint(
	paths daempaths.Paths,
	plan retirement.CleanupPlan,
) (mutation.OperationFingerprint, error) {
	type cleanupFingerprintFacts struct {
		RecoveryDir                 string
		OperationID                 string
		Classification              retirement.CleanupClassification
		Action                      retirement.CleanupActionKind
		JournalAuthorityFingerprint string
		Phase                       retirement.Phase
		ResiduePresent              bool
	}
	authority := plan.Authority()
	fingerprint, err := operationplan.HashJSON(cleanupFingerprintFacts{
		RecoveryDir:                 paths.RecoveryDir,
		OperationID:                 authority.OperationID(),
		Classification:              plan.Classification(),
		Action:                      plan.Action(),
		JournalAuthorityFingerprint: authority.JournalAuthorityFingerprint(),
		Phase:                       authority.Phase(),
		ResiduePresent:              authority.ResiduePresent(),
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf(
			"fingerprint journal cleanup plan: %w",
			err,
		)
	}
	return fingerprint, nil
}
