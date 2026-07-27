package carrier

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/topology"
)

// CarrierFactKey identifies one durable carrier fact by state authority and
// declaration-local relation. It intentionally excludes diagnostic manifest
// provenance and every fact that may contradict at the same owner relation.
type CarrierFactKey struct {
	statefileKey    stateauthority.Key
	relationSubject topology.SubjectID
}

func carrierFactKey(
	owner stateauthority.Authority,
	identity ManagedCarrierIdentity,
) CarrierFactKey {
	return CarrierFactKey{
		statefileKey:    owner.Key(),
		relationSubject: identity.RelationSubject(),
	}
}

// Validate rejects a zero or forged carrier fact key.
func (key CarrierFactKey) Validate() error {
	if err := key.statefileKey.Validate(); err != nil {
		return err
	}
	if err := key.relationSubject.Validate(); err != nil {
		return fmt.Errorf("carrier fact relation subject: %w", err)
	}
	if key.relationSubject.Kind() != topology.SubjectHostRelation {
		return fmt.Errorf("carrier fact key requires host_relation subject")
	}
	return nil
}
