package refresh

import (
	"context"
	"fmt"
	"testing"
	"time"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	lock "github.com/isty2e/daem/internal/realization/lock"
)

func TestCompiledRefreshFingerprintMatchesLegacyProjection(t *testing.T) {
	t.Parallel()

	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: writeNoObserverRefreshFixture(t),
		ExtensionID:  "formatter",
		Timeout:      90 * time.Second,
	}, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prepared.Close() })

	planned := prepared.lifecycle.planned
	withObservation := planned
	withObservation.result.Observation = &Observation{
		State:        observerelation.StateExactCorrelation,
		Reason:       observerelation.ReasonNone,
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	}
	for _, test := range []struct {
		name    string
		planned plan
	}{
		{name: "without observation", planned: planned},
		{name: "with observation", planned: withObservation},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiled, err := refreshFingerprint(test.planned)
			if err != nil {
				t.Fatal(err)
			}
			legacy, err := legacyRefreshFingerprint(test.planned)
			if err != nil {
				t.Fatal(err)
			}
			if !compiled.Equal(legacy) {
				t.Fatal("compiled refresh fingerprint differs from the legacy projection")
			}
		})
	}
}

func legacyRefreshFingerprint(planned plan) (mutation.OperationFingerprint, error) {
	type operationFacts struct {
		Actuation       lock.ActuationKind
		Authority       lock.AuthorityKind
		Preconditions   []string
		EffectEnvelope  lock.EffectEnvelopeClass
		Idempotency     lock.IdempotencyContract
		Verification    lock.VerificationContract
		TrustActivation lock.TrustActivationRequirement
		Recovery        lock.OperationRecoveryClass
	}
	fingerprint, err := operationplan.HashJSON(struct {
		Command       string
		Mode          Mode
		ManifestPath  string
		LockfilePath  string
		StatefilePath string
		Selection     Selection
		Route         Route
		Disclosure    Disclosure
		Observation   *Observation
		Operation     operationFacts
	}{
		Command:       "refresh extension",
		Mode:          planned.result.Mode,
		ManifestPath:  planned.result.ManifestPath,
		LockfilePath:  planned.result.LockfilePath,
		StatefilePath: planned.result.StatefilePath,
		Selection:     planned.result.Selection,
		Route:         planned.result.Route,
		Disclosure:    planned.result.Disclosure,
		Observation:   cloneObservation(planned.result.Observation),
		Operation: operationFacts{
			Actuation:       planned.operationContract.Actuation(),
			Authority:       planned.operationContract.Authority(),
			Preconditions:   planned.operationContract.Preconditions(),
			EffectEnvelope:  planned.operationContract.EffectEnvelope(),
			Idempotency:     planned.operationContract.Idempotency(),
			Verification:    planned.operationContract.Verification(),
			TrustActivation: planned.operationContract.TrustActivation(),
			Recovery:        planned.operationContract.Recovery(),
		},
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint refresh plan: %w", err)
	}
	return fingerprint, nil
}
