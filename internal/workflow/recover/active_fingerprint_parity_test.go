package recover

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/target"
)

func TestCompiledActiveRecoveryFingerprintMatchesLegacyProjection(t *testing.T) {
	for _, test := range []struct {
		name    string
		fixture func(*testing.T) recoveryFixture
	}{
		{name: "rollback", fixture: func(t *testing.T) recoveryFixture { return prepareRecoveryFixture(t, false) }},
		{name: "finalize", fixture: func(t *testing.T) recoveryFixture { return prepareRecoveryFixture(t, true) }},
		{name: "removal continuation", fixture: prepareRemovalRecoveryFixture},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := test.fixture(t)
			prepared, err := Plan(t.Context(), fixture.input)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = prepared.Close() })
			planned := prepared.lifecycle.planned
			active, ok := journal.ActiveRecoveryPlan(planned.plan)
			if !ok {
				t.Fatalf("authority kind = %q, want active", planned.plan.AuthorityKind())
			}
			compiled, err := activeRecoveryOperationFingerprint(
				planned.paths,
				active,
				planned.stateDirAuthority,
			)
			if err != nil {
				t.Fatal(err)
			}
			legacy, err := legacyActiveRecoveryFingerprint(
				planned.paths,
				active,
				planned.stateDirAuthority,
			)
			if err != nil {
				t.Fatal(err)
			}
			if !compiled.Equal(legacy) || !planned.operationEvidence.Equal(legacy) {
				t.Fatal("compiled active recovery fingerprint differs from the legacy projection")
			}
		})
	}
}

type legacyRecoveryFingerprintFacts struct {
	ManifestRoot                 string
	StatefilePath                string
	RecoveryDir                  string
	OperationID                  string
	OperationDir                 string
	Classification               recovery.Classification
	JournalAuthorityFingerprint  string
	StateDirAuthorityFingerprint string
	Actions                      []recovery.Action
	GuardedActions               []recovery.Action
	RemovalCleanupObligations    []legacyRecoveryCleanupObligationFingerprint
	StatefileBefore              json.RawMessage
	ClaimTransitions             []legacyJournalClaimTransitionFingerprint
}

type legacyRecoveryCleanupObligationFingerprint struct {
	Scope       target.Scope
	Destination output.Destination
	Action      recovery.RemovalCleanupActionKind
	Readiness   recovery.RemovalCleanupReadiness
	Reason      recovery.RemovalCleanupReason
	Detail      string
}

type legacyJournalClaimTransitionFingerprint struct {
	Kind                    string
	PathAuthority           legacyRecoveryPathAuthorityFingerprint
	ContentPath             string
	OwnerStatefileAuthority legacyRecoveryPathAuthorityFingerprint
	OwnerManifestPath       string
	OperationID             string
}

type legacyRecoveryPathAuthorityFingerprint struct {
	Key              string
	SemanticsWitness string
}

func legacyActiveRecoveryFingerprint(
	paths daempaths.Paths,
	plan recovery.Plan,
	stateDir recoverygate.StateDirAuthority,
) (mutation.OperationFingerprint, error) {
	journalAuthorityFingerprint, err := plan.JournalAuthorityFingerprint()
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	stateDirFingerprint, err := stateDir.IdentityFingerprint()
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	statefileBefore, err := statefile.Marshal(plan.StatefileBefore())
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery statefile before: %w", err)
	}
	claimTransitions := make([]legacyJournalClaimTransitionFingerprint, 0, len(plan.ClaimTransitions()))
	for _, transition := range plan.ClaimTransitions() {
		claimTransitions = append(claimTransitions, legacyJournalClaimTransitionFingerprint{
			Kind:                    string(transition.Kind()),
			PathAuthority:           legacyRecoveryPathAuthorityFingerprintFor(transition.Address().PathAuthority()),
			ContentPath:             transition.Address().ContentPath(),
			OwnerStatefileAuthority: legacyRecoveryPathAuthorityFingerprintFor(transition.Owner().StatefileAuthority()),
			OwnerManifestPath:       transition.Owner().ManifestPath(),
			OperationID:             transitionOperationID(transition),
		})
	}
	fingerprint, err := operationplan.HashJSON(legacyRecoveryFingerprintFacts{
		ManifestRoot:                 paths.ManifestRoot,
		StatefilePath:                paths.StatefilePath,
		RecoveryDir:                  paths.RecoveryDir,
		OperationID:                  plan.OperationID(),
		OperationDir:                 plan.OperationDir(),
		Classification:               plan.Classification(),
		JournalAuthorityFingerprint:  journalAuthorityFingerprint,
		StateDirAuthorityFingerprint: stateDirFingerprint,
		Actions:                      plan.Actions(),
		GuardedActions:               plan.GuardedActions(),
		RemovalCleanupObligations:    legacyRecoveryCleanupObligationFingerprints(plan.RemovalCleanupObligations()),
		StatefileBefore:              json.RawMessage(statefileBefore),
		ClaimTransitions:             claimTransitions,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery plan: %w", err)
	}
	return fingerprint, nil
}

func legacyRecoveryCleanupObligationFingerprints(
	obligations []recovery.RemovalCleanupObligation,
) []legacyRecoveryCleanupObligationFingerprint {
	result := make([]legacyRecoveryCleanupObligationFingerprint, 0, len(obligations))
	for _, obligation := range obligations {
		result = append(result, legacyRecoveryCleanupObligationFingerprint{
			Scope:       obligation.Scope(),
			Destination: obligation.Destination(),
			Action:      obligation.Action(),
			Readiness:   obligation.Readiness(),
			Reason:      obligation.Reason(),
			Detail:      obligation.Detail(),
		})
	}
	return result
}

func legacyRecoveryPathAuthorityFingerprintFor(
	authority pathauthority.Exact,
) legacyRecoveryPathAuthorityFingerprint {
	return legacyRecoveryPathAuthorityFingerprint{
		Key:              authority.Key(),
		SemanticsWitness: authority.Witness(),
	}
}
