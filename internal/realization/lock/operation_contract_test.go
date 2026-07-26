package lock

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/topology"
)

func TestParseOperationKindUsesClosedCanonicalVocabulary(t *testing.T) {
	for _, operation := range []OperationKind{
		OperationWriteProjection,
		OperationRemoveProjection,
		OperationInstall,
		OperationEnable,
		OperationDisable,
		OperationRemove,
		OperationRefresh,
		OperationPrune,
		OperationObserve,
		OperationAdopt,
	} {
		parsed, err := ParseOperationKind(string(operation))
		if err != nil || parsed != operation {
			t.Fatalf("ParseOperationKind(%q) = %q, %v", operation, parsed, err)
		}
	}
	for _, invalid := range []string{"", " install", "install ", "upgrade"} {
		if _, err := ParseOperationKind(invalid); err == nil {
			t.Fatalf("ParseOperationKind(%q) returned nil error", invalid)
		}
	}
}

func TestDirectProjectionOperationContractCanBeOrdinaryMutationEligible(t *testing.T) {
	contract := mustOperationContract(t, OperationContractInput{
		Operation: OperationWriteProjection,
		Actuation: ActuationDirectProjection,
		Authority: AuthorityManage,
		Route: RouteContractRef{
			RouteID:                "snapshot.resource.write",
			AdapterContractVersion: "v1",
		},
		Preconditions:   []string{"content_hash_locked", "source_identity_locked"},
		EffectEnvelope:  EffectEnvelopeComplete,
		Idempotency:     Idempotent,
		Verification:    VerificationExactArtifact,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryAtomic,
	})

	if !contract.OrdinaryMutationEligible() {
		t.Fatal("direct projection contract should be ordinary mutation eligible")
	}
}

func TestObserveNoMutationContractCannotGrantRemoveAuthority(t *testing.T) {
	_, err := NewOperationContract(OperationContractInput{
		Operation:       OperationObserve,
		Actuation:       ActuationNoMutation,
		Authority:       AuthorityRemove,
		EffectEnvelope:  EffectEnvelopeNotApplicable,
		Idempotency:     IdempotencyNotApplicable,
		Verification:    VerificationExactArtifact,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryNotApplicable,
	})
	if err == nil || !strings.Contains(err.Error(), "no-mutation operation must not grant mutation authority") {
		t.Fatalf("NewOperationContract error = %v, want no-mutation authority diagnostic", err)
	}
}

func TestUnknownDestructiveDelegatedRouteIsNotOrdinaryMutationEligible(t *testing.T) {
	contract := mustOperationContract(t, OperationContractInput{
		Operation: OperationRemove,
		Actuation: ActuationDelegatedHostRoute,
		Authority: AuthorityRemove,
		Route: RouteContractRef{
			RouteID:                "codex.plugin.remove",
			AdapterContractVersion: "v1",
		},
		EffectEnvelope:  EffectEnvelopeUnknown,
		Idempotency:     IdempotencyUnknown,
		Verification:    VerificationHostRelation,
		TrustActivation: TrustActivationRequired,
		Recovery:        OperationRecoveryUnknown,
	})

	if contract.OrdinaryMutationEligible() {
		t.Fatal("unknown destructive delegated route must not be ordinary mutation eligible")
	}
}

func TestCarrierEffectPostconditionRequiresExactRemovalContract(t *testing.T) {
	base := OperationContractInput{
		Operation: OperationRemove,
		Actuation: ActuationDelegatedHostRoute,
		Authority: AuthorityRemove,
		Route: RouteContractRef{
			RouteID:                "pi.package-carrier.remove",
			AdapterContractVersion: "pi-package-remove-v1",
		},
		EffectEnvelope: EffectEnvelopeComplete,
		EffectPostconditions: []effectpostcondition.Requirement{
			effectpostcondition.CarrierArtifactsAbsent,
		},
		Idempotency:     ConditionallyIdempotent,
		Verification:    VerificationHostRelation,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoverySafeRetry,
	}
	contract := mustOperationContract(t, base)
	if !contract.OrdinaryMutationEligible() {
		t.Fatal("exact removal contract should be ordinary mutation eligible")
	}
	requirements := contract.EffectPostconditions().Requirements()
	requirements[0] = effectpostcondition.Requirement("forged")
	if got := contract.EffectPostconditions().Requirements(); len(got) != 1 ||
		got[0] != effectpostcondition.CarrierArtifactsAbsent {
		t.Fatalf("caller mutation changed effect postconditions: %#v", got)
	}

	tests := []struct {
		name   string
		mutate func(*OperationContractInput)
	}{
		{
			name: "wrong operation",
			mutate: func(input *OperationContractInput) {
				input.Operation = OperationInstall
			},
		},
		{
			name: "wrong actuation",
			mutate: func(input *OperationContractInput) {
				input.Actuation = ActuationDirectProjection
			},
		},
		{
			name: "wrong authority",
			mutate: func(input *OperationContractInput) {
				input.Authority = AuthorityManage
			},
		},
		{
			name: "incomplete envelope",
			mutate: func(input *OperationContractInput) {
				input.EffectEnvelope = EffectEnvelopeIncomplete
			},
		},
		{
			name: "wrong verification",
			mutate: func(input *OperationContractInput) {
				input.Verification = VerificationExactArtifact
			},
		},
		{
			name: "duplicate postcondition",
			mutate: func(input *OperationContractInput) {
				input.EffectPostconditions = append(
					input.EffectPostconditions,
					effectpostcondition.CarrierArtifactsAbsent,
				)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			input.EffectPostconditions = append(
				[]effectpostcondition.Requirement(nil),
				base.EffectPostconditions...,
			)
			test.mutate(&input)
			if _, err := NewOperationContract(input); err == nil {
				t.Fatal("NewOperationContract succeeded")
			}
		})
	}
}

func TestLockedSubjectContractRejectsDuplicateOperationContracts(t *testing.T) {
	replay, err := NewReplayCoverage(ReplayUnavailable, ReplayExact, ReplayNotApplicable, nil)
	if err != nil {
		t.Fatalf("NewReplayCoverage returned error: %v", err)
	}
	exact := mustExactArtifactIdentity(t, "local:skill", "rev", "sha256:artifact")
	derivation, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatalf("NewDirectResolutionDerivation returned error: %v", err)
	}
	contract := mustOperationContract(t, OperationContractInput{
		Operation: OperationWriteProjection,
		Actuation: ActuationDirectProjection,
		Authority: AuthorityManage,
		Route: RouteContractRef{
			RouteID:                "snapshot.resource.write",
			AdapterContractVersion: "v1",
		},
		EffectEnvelope:  EffectEnvelopeComplete,
		Idempotency:     Idempotent,
		Verification:    VerificationExactArtifact,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryAtomic,
	})

	_, err = NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID:           mustContractEntityID(t, entity.KindSkill, "review"),
		SubjectID:          mustTopologySubjectID(t, topology.SubjectResource, "skill", "review"),
		ExactSupply:        &exact,
		Derivation:         &derivation,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentApply,
		Replay:             replay,
		OperationContracts: []OperationContract{contract, contract},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate operation contract") {
		t.Fatalf("NewLockedSubjectContract error = %v, want duplicate operation contract diagnostic", err)
	}
}

func mustOperationContract(t *testing.T, input OperationContractInput) OperationContract {
	t.Helper()
	contract, err := NewOperationContract(input)
	if err != nil {
		t.Fatalf("NewOperationContract returned error: %v", err)
	}
	return contract
}
