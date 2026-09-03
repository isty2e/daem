package operationplan

import (
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
)

// RefreshPersistenceRevisions selects the manifest, lockfile, and statefile
// revision role from one compiled refresh authority plan.
func RefreshPersistenceRevisions(
	plan Plan,
	manifestPath string,
	lockfilePath string,
	statefilePath string,
) []mutation.RevisionRequest {
	return plan.RevisionsForPaths(map[string]struct{}{
		manifestPath:  {},
		lockfilePath:  {},
		statefilePath: {},
	})
}

// RefreshSelection is the canonical selected relation identity projected into
// the refresh operation fingerprint.
type RefreshSelection struct {
	ID      string
	Target  string
	Scope   string
	Carrier string
}

// RefreshRoute is the canonical locked route identity projected into the
// refresh operation fingerprint.
type RefreshRoute struct {
	Operation              string
	RouteID                string
	AdapterContractVersion string
	RequestHash            string
	ExecutionSubject       string
	ObservationPosture     string
}

// RefreshInvocation is the deterministic secret-free command disclosure.
type RefreshInvocation struct {
	Kind           string
	Command        string
	Args           []string
	EnvNames       []string
	CWDPolicy      string
	TimeoutSeconds int
}

// RefreshDisclosure is the adapter-owned effect disclosure projected into the
// refresh operation fingerprint.
type RefreshDisclosure struct {
	Invocation            RefreshInvocation
	EffectClasses         []string
	RetainedEffectClasses []string
	NonClaims             []string
}

// RefreshObservation is one bounded relation-observation summary.
type RefreshObservation struct {
	State        string
	Reason       string
	Availability string
	Freshness    string
}

// RefreshOperationContract is the locked operation contract projection.
type RefreshOperationContract struct {
	Actuation       string
	Authority       string
	Preconditions   []string
	EffectEnvelope  string
	Idempotency     string
	Verification    string
	TrustActivation string
	Recovery        string
}

// RefreshIdentityInput contains the already-normalized facts whose exact JSON
// projection is the refresh operation identity. It grants no effect authority.
type RefreshIdentityInput struct {
	Mode          string
	ManifestPath  string
	LockfilePath  string
	StatefilePath string
	Selection     RefreshSelection
	Route         RefreshRoute
	Disclosure    RefreshDisclosure
	Observation   *RefreshObservation
	Operation     RefreshOperationContract
}

type refreshIdentityPayload struct {
	Command       string
	Mode          string
	ManifestPath  string
	LockfilePath  string
	StatefilePath string
	Selection     RefreshSelection
	Route         RefreshRoute
	Disclosure    RefreshDisclosure
	Observation   *RefreshObservation
	Operation     RefreshOperationContract
}

// RefreshOperationFingerprint hashes the exact historical refresh operation
// projection without observing state or acquiring authority.
func RefreshOperationFingerprint(input RefreshIdentityInput) (mutation.OperationFingerprint, error) {
	payload, err := json.Marshal(refreshIdentityPayload{
		Command:       "refresh extension",
		Mode:          input.Mode,
		ManifestPath:  input.ManifestPath,
		LockfilePath:  input.LockfilePath,
		StatefilePath: input.StatefilePath,
		Selection:     input.Selection,
		Route:         input.Route,
		Disclosure:    input.Disclosure,
		Observation:   input.Observation,
		Operation:     input.Operation,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint refresh plan: %w", err)
	}
	return mutation.NewOperationFingerprint(payload), nil
}
