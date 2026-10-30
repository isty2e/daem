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
	footprintcodec "github.com/isty2e/daem/internal/encoding/footprint"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/target"
)

func TestCompiledActiveRecoveryFingerprintMatchesOwnerProjection(t *testing.T) {
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
			planned := prepared.lifecycle.planned.(recoveryPreparation)
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
			owner, err := ownerActiveRecoveryFingerprint(
				planned.paths,
				active,
				planned.stateDirAuthority,
			)
			if err != nil {
				t.Fatal(err)
			}
			if !compiled.Equal(owner) || !planned.operationEvidence.Equal(owner) {
				t.Fatal("compiled active recovery fingerprint differs from the owner projection")
			}
		})
	}
}

type ownerRecoveryFingerprintFacts struct {
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
	RemovalCleanupObligations    []ownerRecoveryCleanupObligationFingerprint
	StatefileBefore              json.RawMessage
	ClaimTransitions             []ownerJournalClaimTransitionFingerprint
}

type ownerRecoveryCleanupObligationFingerprint struct {
	Scope       target.Scope
	Destination output.Destination
	Action      recovery.RemovalCleanupActionKind
	Readiness   recovery.RemovalCleanupReadiness
	Reason      recovery.RemovalCleanupReason
	Detail      string
}

type ownerJournalClaimTransitionFingerprint struct {
	Kind                    string
	PathAuthority           ownerRecoveryPathAuthorityFingerprint
	Footprint               footprintcodec.Record
	OwnerStatefileAuthority ownerRecoveryPathAuthorityFingerprint
	OwnerManifestPath       string
	OperationID             string
}

type ownerRecoveryPathAuthorityFingerprint struct {
	Key              string
	SemanticsWitness string
}

func ownerActiveRecoveryFingerprint(
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
	claimTransitions := make([]ownerJournalClaimTransitionFingerprint, 0, len(plan.ClaimTransitions()))
	for _, transition := range plan.ClaimTransitions() {
		footprint, err := footprintcodec.Encode(transition.Address().Footprint())
		if err != nil {
			return mutation.OperationFingerprint{}, err
		}
		claimTransitions = append(claimTransitions, ownerJournalClaimTransitionFingerprint{
			Kind:                    string(transition.Kind()),
			PathAuthority:           ownerRecoveryPathAuthorityFingerprintFor(transition.Address().PathAuthority()),
			Footprint:               footprint,
			OwnerStatefileAuthority: ownerRecoveryPathAuthorityFingerprintFor(transition.Owner().StatefileAuthority()),
			OwnerManifestPath:       transition.Owner().ManifestPath(),
			OperationID:             transitionOperationID(transition),
		})
	}
	fingerprint, err := operationplan.HashJSON(ownerRecoveryFingerprintFacts{
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
		RemovalCleanupObligations:    ownerRecoveryCleanupObligationFingerprints(plan.RemovalCleanupObligations()),
		StatefileBefore:              json.RawMessage(statefileBefore),
		ClaimTransitions:             claimTransitions,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery plan: %w", err)
	}
	return fingerprint, nil
}

func ownerRecoveryCleanupObligationFingerprints(
	obligations []recovery.RemovalCleanupObligation,
) []ownerRecoveryCleanupObligationFingerprint {
	result := make([]ownerRecoveryCleanupObligationFingerprint, 0, len(obligations))
	for _, obligation := range obligations {
		result = append(result, ownerRecoveryCleanupObligationFingerprint{
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

func ownerRecoveryPathAuthorityFingerprintFor(
	authority pathauthority.Exact,
) ownerRecoveryPathAuthorityFingerprint {
	return ownerRecoveryPathAuthorityFingerprint{
		Key:              authority.Key(),
		SemanticsWitness: authority.Witness(),
	}
}
