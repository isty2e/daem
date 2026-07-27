package carrier

import (
	"cmp"
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// CarrierConsumer identifies one daem-known managed relation consuming a carrier.
type CarrierConsumer struct {
	owner              stateauthority.Authority
	relationSubject    topology.SubjectID
	managedInstanceKey hostrelation.ManagedInstanceKey
}

// Validate rejects zero or internally inconsistent consumer identity.
func (consumer CarrierConsumer) Validate() error {
	if err := consumer.owner.Validate(); err != nil {
		return fmt.Errorf("carrier consumer owner: %w", err)
	}
	if err := consumer.relationSubject.Validate(); err != nil {
		return fmt.Errorf("carrier consumer relation subject: %w", err)
	}
	if consumer.relationSubject.Kind() != topology.SubjectHostRelation {
		return fmt.Errorf("carrier consumer requires host_relation subject")
	}
	if _, err := hostrelation.NewManagedInstanceKey(string(consumer.managedInstanceKey)); err != nil {
		return fmt.Errorf("carrier consumer managed instance key: %w", err)
	}
	return nil
}

func carrierConsumerFromClaim(claim ManagedCarrierClaim) CarrierConsumer {
	return CarrierConsumer{
		owner:              claim.Owner(),
		relationSubject:    claim.Identity().RelationSubject(),
		managedInstanceKey: claim.Identity().ExpectedRelation().ManagedInstanceKey(),
	}
}

// Owner returns the manifest state authority that owns this consumer claim.
func (consumer CarrierConsumer) Owner() stateauthority.Authority { return consumer.owner }

// RelationSubject returns the declaration-local host relation subject.
func (consumer CarrierConsumer) RelationSubject() topology.SubjectID {
	return consumer.relationSubject
}

// ManagedInstanceKey returns the exact daem correlation identity.
func (consumer CarrierConsumer) ManagedInstanceKey() hostrelation.ManagedInstanceKey {
	return consumer.managedInstanceKey
}

// ExactEqual reports complete consumer identity equality.
func (consumer CarrierConsumer) ExactEqual(other CarrierConsumer) bool {
	return consumer.owner.ExactEqual(other.owner) &&
		consumer.relationSubject == other.relationSubject &&
		consumer.managedInstanceKey == other.managedInstanceKey
}

// CarrierOccupancy is a pure, daem-known consumer view for one structural
// carrier. It never claims knowledge of ambient or manual consumers.
type CarrierOccupancy struct {
	carrier   extensiontopology.Carrier
	consumers []CarrierConsumer
}

// NewCarrierOccupancy derives the active daem-known consumer set for one key.
func NewCarrierOccupancy(
	carrier extensiontopology.Carrier,
	claims []ManagedCarrierClaim,
) (CarrierOccupancy, error) {
	if err := carrier.Validate(); err != nil {
		return CarrierOccupancy{}, fmt.Errorf("carrier occupancy: %w", err)
	}
	consumers := make([]CarrierConsumer, 0, len(claims))
	seen := make(map[carrierConsumerKey]struct{}, len(claims))
	for index, claim := range claims {
		if err := claim.Validate(); err != nil {
			return CarrierOccupancy{}, fmt.Errorf("carrier occupancy claim[%d]: %w", index, err)
		}
		if claim.Identity().Carrier() != carrier {
			continue
		}
		consumer := carrierConsumerFromClaim(claim)
		key := carrierConsumerKeyFor(consumer)
		if _, duplicate := seen[key]; duplicate {
			return CarrierOccupancy{}, fmt.Errorf(
				"carrier occupancy claim[%d]: duplicate daem-known consumer",
				index,
			)
		}
		seen[key] = struct{}{}
		consumers = append(consumers, consumer)
	}
	sort.Slice(consumers, func(left int, right int) bool {
		return compareCarrierConsumer(consumers[left], consumers[right]) < 0
	})
	occupancy := CarrierOccupancy{carrier: carrier, consumers: consumers}
	if err := occupancy.Validate(); err != nil {
		return CarrierOccupancy{}, err
	}
	return occupancy, nil
}

// Validate rejects zero, duplicate, unsorted, or malformed occupancy facts.
func (occupancy CarrierOccupancy) Validate() error {
	if err := occupancy.carrier.Validate(); err != nil {
		return fmt.Errorf("carrier occupancy: %w", err)
	}
	seen := make(map[carrierConsumerKey]struct{}, len(occupancy.consumers))
	for index, consumer := range occupancy.consumers {
		if err := consumer.Validate(); err != nil {
			return fmt.Errorf("carrier occupancy consumer[%d]: %w", index, err)
		}
		key := carrierConsumerKeyFor(consumer)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("carrier occupancy consumer[%d]: duplicate daem-known consumer", index)
		}
		seen[key] = struct{}{}
		if index > 0 && compareCarrierConsumer(occupancy.consumers[index-1], consumer) >= 0 {
			return fmt.Errorf("carrier occupancy consumers are not in canonical order")
		}
	}
	return nil
}

// Carrier returns the structural carrier whose daem-known consumers are indexed.
func (occupancy CarrierOccupancy) Carrier() extensiontopology.Carrier {
	return occupancy.carrier
}

// DaemKnownConsumers returns a defensive copy of active claim consumers.
func (occupancy CarrierOccupancy) DaemKnownConsumers() []CarrierConsumer {
	return append([]CarrierConsumer(nil), occupancy.consumers...)
}

// DaemKnownConsumerCount returns only the number represented by active daem claims.
func (occupancy CarrierOccupancy) DaemKnownConsumerCount() int {
	return len(occupancy.consumers)
}

// IsDaemKnownEmpty reports only registry emptiness. It is never proof that
// ambient or manually managed consumers are absent.
func (occupancy CarrierOccupancy) IsDaemKnownEmpty() bool {
	return len(occupancy.consumers) == 0
}

// IsOnlyDaemKnownConsumer reports whether candidate is the exact sole active
// daem claim. It makes no assertion about ambient consumers.
func (occupancy CarrierOccupancy) IsOnlyDaemKnownConsumer(candidate CarrierConsumer) bool {
	return len(occupancy.consumers) == 1 && occupancy.consumers[0].ExactEqual(candidate)
}

type carrierConsumerKey struct {
	statefileKey       string
	relationSubject    topology.SubjectID
	managedInstanceKey hostrelation.ManagedInstanceKey
}

func carrierConsumerKeyFor(consumer CarrierConsumer) carrierConsumerKey {
	return carrierConsumerKey{
		statefileKey:       consumer.owner.StatefileKey(),
		relationSubject:    consumer.relationSubject,
		managedInstanceKey: consumer.managedInstanceKey,
	}
}

func compareCarrierConsumer(left CarrierConsumer, right CarrierConsumer) int {
	if order := cmp.Compare(left.owner.StatefileKey(), right.owner.StatefileKey()); order != 0 {
		return order
	}
	if order := cmp.Compare(left.relationSubject.Namespace(), right.relationSubject.Namespace()); order != 0 {
		return order
	}
	if order := cmp.Compare(left.relationSubject.Key(), right.relationSubject.Key()); order != 0 {
		return order
	}
	return cmp.Compare(left.managedInstanceKey, right.managedInstanceKey)
}
