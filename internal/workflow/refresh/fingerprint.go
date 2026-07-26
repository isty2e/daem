package refresh

import (
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	lock "github.com/isty2e/daem/internal/realization/lock"
)

func refreshFingerprint(planned plan) (mutation.OperationFingerprint, error) {
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
	canonical, err := json.Marshal(struct {
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
	return mutation.NewOperationFingerprint(canonical), nil
}
