package refresh

import (
	"context"
	"errors"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestRefreshRefusesDistinctRequiredObservationFailures(t *testing.T) {
	tests := []struct {
		name       string
		state      observerelation.CorrelationState
		wantReason ReasonCode
	}{
		{
			name:       "stale evidence",
			state:      observerelation.StateStaleEvidence,
			wantReason: ReasonObservationUnavailable,
		},
		{
			name:       "unavailable evidence",
			state:      observerelation.StateUnavailableEvidence,
			wantReason: ReasonObservationUnavailable,
		},
		{
			name:       "ambiguous identity",
			state:      observerelation.StateAmbiguous,
			wantReason: ReasonRelationAmbiguous,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifestPath, inventoryPath := writeObservedRefreshFixture(t)
			prepared, err := PlanWrite(context.Background(), CommandInput{
				ManifestPath: manifestPath,
				ExtensionID:  "context7",
			}, PlanOptions{
				CommandBuilder: failIfRefreshBuilderCalled(t),
				Observer: observationStateObserver(
					t,
					inventoryPath,
					test.state,
				),
			})
			if err == nil {
				t.Fatal("PlanWrite returned nil error")
			}
			t.Cleanup(func() { _ = prepared.Close() })
			result := prepared.Disclosure()
			if result.ResultClass != ResultRefused ||
				result.ReasonCode != test.wantReason ||
				result.Observation == nil ||
				result.Observation.State != test.state {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}

	t.Run("observer failure", func(t *testing.T) {
		manifestPath, _ := writeObservedRefreshFixture(t)
		prepared, err := PlanWrite(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "context7",
		}, PlanOptions{
			CommandBuilder: failIfRefreshBuilderCalled(t),
			Observer: func(
				context.Context,
				ObservationRequest,
			) (RelationObservation, error) {
				return RelationObservation{}, errors.New("synthetic observer failure")
			},
		})
		if err == nil {
			t.Fatal("PlanWrite returned nil error")
		}
		t.Cleanup(func() { _ = prepared.Close() })
		result := prepared.Disclosure()
		if result.ResultClass != ResultRefused ||
			result.ReasonCode != ReasonObservationUnavailable {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
}

func TestRefreshStartedCancellationIsPartialPersistedAndNotRetried(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	calls := 0
	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(
				context.Context,
				subprocess.CommandRequest,
			) subprocess.CommandResult {
				calls++
				return subprocess.CommandResult{
					Started:  true,
					Canceled: true,
					Err:      context.Canceled,
				}
			},
		},
	})
	if err == nil ||
		calls != 1 ||
		result.ResultClass != ResultPartial ||
		result.ReasonCode != ReasonCommandFailed ||
		!result.Attempted ||
		result.ProcessOutcome == nil ||
		!result.ProcessOutcome.Cancelled ||
		!result.AttemptHistory.Persisted {
		t.Fatalf("result=%#v calls=%d err=%v", result, calls, err)
	}
}

func TestRefreshUnstartedCancellationIsCancelledAndNotPersisted(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	calls := 0
	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(
				_ context.Context,
				_ subprocess.CommandRequest,
			) subprocess.CommandResult {
				calls++
				return subprocess.CommandResult{
					Canceled: true,
					Err:      context.Canceled,
				}
			},
		},
	})
	if err == nil ||
		calls != 1 ||
		result.ResultClass != ResultCancelled ||
		result.ReasonCode != ReasonCancelled ||
		result.Attempted ||
		result.ProcessOutcome == nil ||
		!result.ProcessOutcome.Cancelled ||
		result.AttemptHistory.Persisted {
		t.Fatalf("result=%#v calls=%d err=%v", result, calls, err)
	}
}

func observationStateObserver(
	t *testing.T,
	inventoryPath string,
	state observerelation.CorrelationState,
) RelationObserver {
	t.Helper()
	return func(
		_ context.Context,
		input ObservationRequest,
	) (RelationObservation, error) {
		contract, found := input.Lockfile.Locked.Subject(input.Subject)
		if !found {
			return RelationObservation{}, errors.New("selected subject missing from lock")
		}
		realization, ok := contract.Realization()
		if !ok {
			return RelationObservation{}, errors.New("selected subject has no realization")
		}
		delegated, ok := realization.DelegatedRelation()
		if !ok {
			return RelationObservation{}, errors.New("selected subject is not delegated")
		}
		inventory, err := inventoryForObservationState(
			delegated.ExpectedRelation(),
			state,
		)
		if err != nil {
			return RelationObservation{}, err
		}
		authorityPath, err := observerelation.NewAuthorityPath(
			inventoryPath,
			input.Target,
			input.Scope,
		)
		if err != nil {
			return RelationObservation{}, err
		}
		return RelationObservation{
			Result: observerelation.Correlate(
				delegated.ExpectedRelation(),
				inventory,
			),
			Present:        true,
			AuthorityPaths: []observerelation.AuthorityPath{authorityPath},
		}, nil
	}
}

func inventoryForObservationState(
	expected hostrelation.ExpectedRelation,
	state observerelation.CorrelationState,
) (observerelation.Inventory, error) {
	spec := observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}
	switch state {
	case observerelation.StateStaleEvidence:
		spec.Freshness = observerelation.EvidenceStale
	case observerelation.StateUnavailableEvidence:
		spec.Availability = observerelation.InventoryUnavailable
	case observerelation.StateAmbiguous:
		row, err := observerelation.NewRow(observerelation.RowSpec{
			SubjectKey:            string(expected.SubjectKey()),
			HasManagedInstanceKey: true,
			ManagedInstanceKey:    string(expected.ManagedInstanceKey()),
		})
		if err != nil {
			return observerelation.Inventory{}, err
		}
		spec.Rows = []observerelation.Row{row, row}
	default:
		return observerelation.Inventory{}, errors.New("unsupported observation test state")
	}
	return observerelation.NewInventory(spec)
}
