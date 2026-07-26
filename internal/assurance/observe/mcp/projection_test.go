package mcp

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestObserveAggregateProjectionClassifiesCurrentPassiveAxes(t *testing.T) {
	contract := claudeMCPRecord(t)
	projected := projectionStateForContract(t, contract, true, lockedCanonicalContribution(t, contract))

	observation, err := ObserveAggregateProjection(AggregateProjectionObservationInput{
		Contract:   contract,
		Projection: projected,
		Observed:   true,
		Ownership:  OwnershipManaged,
		Shadowing:  ShadowCarrierCollision,
	})
	if err != nil {
		t.Fatalf("ObserveAggregateProjection returned error: %v", err)
	}
	if observation.Subject != contract.SubjectID() ||
		observation.Projection.State != ProjectionProjected ||
		observation.Ownership.State != OwnershipManaged ||
		observation.Shadowing.State != ShadowCarrierCollision ||
		observation.Shadowing.Reason != ReasonConfigShadowed {
		t.Fatalf("observation = %#v, want projected managed collision", observation)
	}
}

func TestObserveAggregateProjectionClassifiesMissingDriftOwnershipAndFailure(t *testing.T) {
	contract := antigravityMCPRecord(t)
	tests := []struct {
		name          string
		input         AggregateProjectionObservationInput
		wantState     ProjectionState
		wantReason    ReasonCode
		wantOwnership OwnershipState
	}{
		{
			name: "missing",
			input: AggregateProjectionObservationInput{
				Contract:   contract,
				Projection: projectionStateForContract(t, contract, false, ""),
				Observed:   true,
				Ownership:  OwnershipUnknown,
			},
			wantState:     ProjectionMissing,
			wantOwnership: OwnershipUnknown,
		},
		{
			name: "drifted",
			input: AggregateProjectionObservationInput{
				Contract:   contract,
				Projection: projectionStateForContract(t, contract, true, `{"different":true}`),
				Observed:   true,
				Ownership:  OwnershipManaged,
			},
			wantState:     ProjectionDrifted,
			wantOwnership: OwnershipManaged,
		},
		{
			name: "same name unmanaged takes precedence",
			input: AggregateProjectionObservationInput{
				Contract:   contract,
				Projection: projectionStateForContract(t, contract, true, `{"different":true}`),
				Observed:   true,
				Ownership:  OwnershipUnmanagedSameName,
			},
			wantState:     ProjectionUnmanagedSameName,
			wantReason:    ReasonRoutePreexistingUnowned,
			wantOwnership: OwnershipUnmanagedSameName,
		},
		{
			name: "malformed observation failure",
			input: AggregateProjectionObservationInput{
				Contract:      contract,
				FailureReason: aggregate.CodecFailureDocumentMalformed,
				Ownership:     OwnershipUnknown,
			},
			wantState:     ProjectionMalformed,
			wantReason:    ReasonConfigMalformed,
			wantOwnership: OwnershipUnknown,
		},
		{
			name: "unsupported transport failure",
			input: AggregateProjectionObservationInput{
				Contract:      contract,
				FailureReason: aggregate.CodecFailureUnsupportedTransport,
				Ownership:     OwnershipUnknown,
			},
			wantState:     ProjectionUnsupported,
			wantReason:    ReasonUnsupportedTransport,
			wantOwnership: OwnershipUnknown,
		},
		{
			name: "unsupported managed field failure",
			input: AggregateProjectionObservationInput{
				Contract:      contract,
				FailureReason: aggregate.CodecFailureUnsupportedManagedField,
				Ownership:     OwnershipUnknown,
			},
			wantState:     ProjectionUnsupported,
			wantReason:    ReasonUnsupportedManagedField,
			wantOwnership: OwnershipUnknown,
		},
		{
			name: "secret literal failure",
			input: AggregateProjectionObservationInput{
				Contract:      contract,
				FailureReason: aggregate.CodecFailureSecretLiteralForbidden,
				Ownership:     OwnershipUnknown,
			},
			wantState:     ProjectionUnsupported,
			wantReason:    ReasonSecretLiteralForbidden,
			wantOwnership: OwnershipUnknown,
		},
		{
			name: "unsupported alternate config",
			input: AggregateProjectionObservationInput{
				Contract:                   contract,
				Projection:                 projectionStateForContract(t, contract, true, lockedCanonicalContribution(t, contract)),
				Observed:                   true,
				UnsupportedAlternateConfig: true,
				Ownership:                  OwnershipManaged,
			},
			wantState:     ProjectionUnsupported,
			wantReason:    ReasonUnsupportedAlternateConfig,
			wantOwnership: OwnershipManaged,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation, err := ObserveAggregateProjection(test.input)
			if err != nil {
				t.Fatalf("ObserveAggregateProjection returned error: %v", err)
			}
			if observation.Projection.State != test.wantState ||
				observation.Projection.Reason != test.wantReason ||
				observation.Ownership.State != test.wantOwnership {
				t.Fatalf(
					"observation = projection %#v ownership %#v, want %q/%q and %q",
					observation.Projection,
					observation.Ownership,
					test.wantState,
					test.wantReason,
					test.wantOwnership,
				)
			}
		})
	}
}

func TestObserveAggregateProjectionRejectsContradictoryOrForeignEvidence(t *testing.T) {
	contract := claudeMCPRecord(t)
	projected := projectionStateForContract(t, contract, true, lockedCanonicalContribution(t, contract))
	foreignContract := openCodeMCPRecord(t)
	foreign := projectionStateForContract(
		t,
		foreignContract,
		true,
		lockedCanonicalContribution(t, foreignContract),
	)

	tests := []AggregateProjectionObservationInput{
		{Contract: contract},
		{
			Contract:      contract,
			Projection:    projected,
			Observed:      true,
			FailureReason: aggregate.CodecFailureDocumentMalformed,
		},
		{Contract: contract, Projection: foreign, Observed: true},
	}
	for index, input := range tests {
		if _, err := ObserveAggregateProjection(input); err == nil {
			t.Fatalf("input %d returned nil error", index)
		}
	}

	resource := snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind:         entity.KindSkill,
		Name:         "review",
		SourceID:     "local:skills/review?mode=vendor",
		ArtifactKind: artifact.ArtifactKindDirectory,
		ContentHash:  artifact.HashFileContent([]byte("review")),
	})
	if _, err := ObserveAggregateProjection(AggregateProjectionObservationInput{
		Contract:      resource,
		FailureReason: aggregate.CodecFailureEquivalenceUndefined,
	}); err == nil {
		t.Fatal("ObserveAggregateProjection accepted a non-MCP subject")
	}
}

func projectionStateForContract(
	t *testing.T,
	contract lock.LockedSubjectContract,
	present bool,
	canonical string,
) aggregate.ProjectionState {
	t.Helper()
	item, ok, err := contract.ManagedAggregateContribution()
	if err != nil || !ok {
		t.Fatalf("ManagedAggregateContribution = %#v, %t, %v", item, ok, err)
	}
	state, err := aggregate.NewProjectionState(item.Contribution().Contract(), present, present, canonical)
	if err != nil {
		t.Fatalf("NewProjectionState returned error: %v", err)
	}
	return state
}

func lockedCanonicalContribution(
	t *testing.T,
	contract lock.LockedSubjectContract,
) string {
	t.Helper()
	item, ok, err := contract.ManagedAggregateContribution()
	if err != nil || !ok {
		t.Fatalf("ManagedAggregateContribution = %#v, %t, %v", item, ok, err)
	}
	return item.Contribution().CanonicalContribution()
}
