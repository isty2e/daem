package durable

import (
	"fmt"
	"sort"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

// SnapshotInput is the complete set of durable assurance fact families.
type SnapshotInput struct {
	ManagedPaths           []ManagedPathState
	ManagedAggregates      []ManagedAggregateState
	PendingCarrierInstalls []durablecarrier.PendingCarrierInstall
	PendingCarrierRemovals []durablecarrier.PendingCarrierRemoval
	ManagedCarrierClaims   []durablecarrier.ManagedCarrierClaim
	DelegateAttempts       []durableattempt.DelegateAttempt
	HostRouteAttempts      []durableattempt.HostRouteAttempt
}

// Snapshot is canonical non-current state used for correlation and history.
type Snapshot struct {
	managedPaths           []ManagedPathState
	managedAggregates      []ManagedAggregateState
	pendingCarrierInstalls []durablecarrier.PendingCarrierInstall
	pendingCarrierRemovals []durablecarrier.PendingCarrierRemoval
	managedCarrierClaims   []durablecarrier.ManagedCarrierClaim
	delegateAttempts       []durableattempt.DelegateAttempt
	hostRouteAttempts      []durableattempt.HostRouteAttempt
}

// EmptySnapshot returns the canonical empty durable state.
func EmptySnapshot() Snapshot { return Snapshot{} }

// NewSnapshot validates, orders, and defensively copies every fact family.
func NewSnapshot(input SnapshotInput) (Snapshot, error) {
	snapshot := Snapshot{
		managedPaths:           append([]ManagedPathState(nil), input.ManagedPaths...),
		managedAggregates:      append([]ManagedAggregateState(nil), input.ManagedAggregates...),
		pendingCarrierInstalls: append([]durablecarrier.PendingCarrierInstall(nil), input.PendingCarrierInstalls...),
		pendingCarrierRemovals: append([]durablecarrier.PendingCarrierRemoval(nil), input.PendingCarrierRemovals...),
		managedCarrierClaims:   append([]durablecarrier.ManagedCarrierClaim(nil), input.ManagedCarrierClaims...),
		delegateAttempts:       append([]durableattempt.DelegateAttempt(nil), input.DelegateAttempts...),
		hostRouteAttempts:      append([]durableattempt.HostRouteAttempt(nil), input.HostRouteAttempts...),
	}
	if err := snapshot.validate(); err != nil {
		return Snapshot{}, err
	}
	snapshot.sort()
	return snapshot, nil
}

func (snapshot Snapshot) validate() error {
	projectionSubjects := make(map[topology.SubjectID]string)
	pathAddresses := make(map[output.Destination]topology.SubjectID)
	aggregateContracts := make(map[string]aggregate.ProjectionContract)
	for index, state := range snapshot.managedPaths {
		if err := state.validate(); err != nil {
			return fmt.Errorf("managed path[%d]: %w", index, err)
		}
		if family, duplicate := projectionSubjects[state.subject]; duplicate {
			return fmt.Errorf(
				"managed path[%d]: projection subject %q already belongs to %s",
				index,
				state.subject,
				family,
			)
		}
		projectionSubjects[state.subject] = "managed path"
		address := state.destination
		if existing, duplicate := pathAddresses[address]; duplicate {
			return fmt.Errorf(
				"managed path[%d]: destination %q already belongs to subject %q",
				index,
				address,
				existing,
			)
		}
		pathAddresses[address] = state.subject
	}
	for index, state := range snapshot.managedAggregates {
		contribution := state.contribution
		if _, err := NewManagedAggregateState(state.subject, contribution); err != nil {
			return fmt.Errorf("managed aggregate[%d]: %w", index, err)
		}
		if family, duplicate := projectionSubjects[state.subject]; duplicate {
			return fmt.Errorf(
				"managed aggregate[%d]: projection subject %q already belongs to %s",
				index,
				state.subject,
				family,
			)
		}
		projectionSubjects[state.subject] = "managed aggregate"
		root := contribution.AggregateRoot()
		if _, conflict := pathAddresses[root]; conflict {
			return fmt.Errorf(
				"managed aggregate[%d]: aggregate root %q overlaps managed path ownership",
				index,
				root,
			)
		}
		physicalKey := aggregatePhysicalKey(contribution)
		if existing, shared := aggregateContracts[physicalKey]; shared {
			if !existing.Equal(contribution.Contract()) {
				return fmt.Errorf(
					"managed aggregate[%d]: contributors at one physical projection must share the full contract",
					index,
				)
			}
			if existing.Cardinality() != aggregate.ContributionSharedSet {
				return fmt.Errorf(
					"managed aggregate[%d]: exclusive aggregate projection cannot be shared",
					index,
				)
			}
		} else {
			aggregateContracts[physicalKey] = contribution.Contract()
		}
	}
	if err := validateSnapshotCarrierFacts(
		snapshot.pendingCarrierInstalls,
		snapshot.pendingCarrierRemovals,
		snapshot.managedCarrierClaims,
	); err != nil {
		return err
	}
	if err := validateUniqueDelegateAttempts(snapshot.delegateAttempts); err != nil {
		return err
	}
	if err := validateUniqueHostRouteAttempts(snapshot.hostRouteAttempts); err != nil {
		return err
	}
	return nil
}

func (snapshot *Snapshot) sort() {
	sort.Slice(snapshot.managedPaths, func(left int, right int) bool {
		return compareManagedPath(snapshot.managedPaths[left], snapshot.managedPaths[right]) < 0
	})
	sort.Slice(snapshot.managedAggregates, func(left int, right int) bool {
		return compareManagedAggregate(snapshot.managedAggregates[left], snapshot.managedAggregates[right]) < 0
	})
	sort.Slice(snapshot.pendingCarrierInstalls, func(left int, right int) bool {
		return snapshot.pendingCarrierInstalls[left].Compare(
			snapshot.pendingCarrierInstalls[right],
		) < 0
	})
	sort.Slice(snapshot.pendingCarrierRemovals, func(left int, right int) bool {
		return snapshot.pendingCarrierRemovals[left].Compare(
			snapshot.pendingCarrierRemovals[right],
		) < 0
	})
	sort.Slice(snapshot.managedCarrierClaims, func(left int, right int) bool {
		return snapshot.managedCarrierClaims[left].Compare(
			snapshot.managedCarrierClaims[right],
		) < 0
	})
	sort.Slice(snapshot.delegateAttempts, func(left int, right int) bool {
		return snapshot.delegateAttempts[left].Compare(snapshot.delegateAttempts[right]) < 0
	})
	sort.Slice(snapshot.hostRouteAttempts, func(left int, right int) bool {
		return snapshot.hostRouteAttempts[left].Compare(snapshot.hostRouteAttempts[right]) < 0
	})
}

// ManagedPaths returns a defensive copy of managed-path baselines.
func (snapshot Snapshot) ManagedPaths() []ManagedPathState {
	return append([]ManagedPathState(nil), snapshot.managedPaths...)
}

// ManagedAggregates returns defensive canonical copies of aggregate baselines.
func (snapshot Snapshot) ManagedAggregates() []ManagedAggregateState {
	result := make([]ManagedAggregateState, 0, len(snapshot.managedAggregates))
	for _, state := range snapshot.managedAggregates {
		cloned, _ := NewManagedAggregateState(state.subject, state.contribution)
		result = append(result, cloned)
	}
	return result
}

// PendingCarrierInstalls returns write-ahead install correlation facts.
func (snapshot Snapshot) PendingCarrierInstalls() []durablecarrier.PendingCarrierInstall {
	return append([]durablecarrier.PendingCarrierInstall(nil), snapshot.pendingCarrierInstalls...)
}

// PendingCarrierRemovals returns write-ahead removal facts.
func (snapshot Snapshot) PendingCarrierRemovals() []durablecarrier.PendingCarrierRemoval {
	return append([]durablecarrier.PendingCarrierRemoval(nil), snapshot.pendingCarrierRemovals...)
}

// ManagedCarrierClaims returns project-scoped durable carrier claims.
func (snapshot Snapshot) ManagedCarrierClaims() []durablecarrier.ManagedCarrierClaim {
	return append([]durablecarrier.ManagedCarrierClaim(nil), snapshot.managedCarrierClaims...)
}

// DelegateAttempts returns a defensive copy of delegate history.
func (snapshot Snapshot) DelegateAttempts() []durableattempt.DelegateAttempt {
	return append([]durableattempt.DelegateAttempt(nil), snapshot.delegateAttempts...)
}

// HostRouteAttempts returns a defensive copy of host-route history.
func (snapshot Snapshot) HostRouteAttempts() []durableattempt.HostRouteAttempt {
	return append([]durableattempt.HostRouteAttempt(nil), snapshot.hostRouteAttempts...)
}

// WithManagedPaths replaces the complete managed-path baseline family.
func (snapshot Snapshot) WithManagedPaths(values []ManagedPathState) (Snapshot, error) {
	input := snapshot.input()
	input.ManagedPaths = values
	return NewSnapshot(input)
}

// WithManagedAggregates replaces the complete aggregate baseline family.
func (snapshot Snapshot) WithManagedAggregates(values []ManagedAggregateState) (Snapshot, error) {
	input := snapshot.input()
	input.ManagedAggregates = values
	return NewSnapshot(input)
}

// WithPendingCarrierInstalls replaces the complete write-ahead carrier family.
func (snapshot Snapshot) WithPendingCarrierInstalls(values []durablecarrier.PendingCarrierInstall) (Snapshot, error) {
	input := snapshot.input()
	input.PendingCarrierInstalls = values
	return NewSnapshot(input)
}

// WithPendingCarrierRemovals replaces the complete write-ahead removal family.
func (snapshot Snapshot) WithPendingCarrierRemovals(values []durablecarrier.PendingCarrierRemoval) (Snapshot, error) {
	input := snapshot.input()
	input.PendingCarrierRemovals = values
	return NewSnapshot(input)
}

// WithManagedCarrierClaims replaces the complete project carrier-claim family.
func (snapshot Snapshot) WithManagedCarrierClaims(values []durablecarrier.ManagedCarrierClaim) (Snapshot, error) {
	input := snapshot.input()
	input.ManagedCarrierClaims = values
	return NewSnapshot(input)
}

// WithDelegateAttempts replaces the complete delegate-attempt family.
func (snapshot Snapshot) WithDelegateAttempts(values []durableattempt.DelegateAttempt) (Snapshot, error) {
	input := snapshot.input()
	input.DelegateAttempts = values
	return NewSnapshot(input)
}

// WithHostRouteAttempts replaces the complete host-route-attempt family.
func (snapshot Snapshot) WithHostRouteAttempts(values []durableattempt.HostRouteAttempt) (Snapshot, error) {
	input := snapshot.input()
	input.HostRouteAttempts = values
	return NewSnapshot(input)
}

// Equal reports semantic equality across every ordered fact family.
func (snapshot Snapshot) Equal(other Snapshot) bool {
	if len(snapshot.managedPaths) != len(other.managedPaths) ||
		len(snapshot.managedAggregates) != len(other.managedAggregates) ||
		len(snapshot.pendingCarrierInstalls) != len(other.pendingCarrierInstalls) ||
		len(snapshot.pendingCarrierRemovals) != len(other.pendingCarrierRemovals) ||
		len(snapshot.managedCarrierClaims) != len(other.managedCarrierClaims) ||
		len(snapshot.delegateAttempts) != len(other.delegateAttempts) ||
		len(snapshot.hostRouteAttempts) != len(other.hostRouteAttempts) {
		return false
	}
	for index := range snapshot.managedPaths {
		if !snapshot.managedPaths[index].Equal(other.managedPaths[index]) {
			return false
		}
	}
	for index := range snapshot.managedAggregates {
		if !snapshot.managedAggregates[index].Equal(other.managedAggregates[index]) {
			return false
		}
	}
	for index := range snapshot.pendingCarrierInstalls {
		if !snapshot.pendingCarrierInstalls[index].ExactEqual(other.pendingCarrierInstalls[index]) {
			return false
		}
	}
	for index := range snapshot.pendingCarrierRemovals {
		if !snapshot.pendingCarrierRemovals[index].ExactEqual(other.pendingCarrierRemovals[index]) {
			return false
		}
	}
	for index := range snapshot.managedCarrierClaims {
		if !snapshot.managedCarrierClaims[index].ExactEqual(other.managedCarrierClaims[index]) {
			return false
		}
	}
	for index := range snapshot.delegateAttempts {
		if !snapshot.delegateAttempts[index].Equal(other.delegateAttempts[index]) {
			return false
		}
	}
	for index := range snapshot.hostRouteAttempts {
		if !snapshot.hostRouteAttempts[index].Equal(other.hostRouteAttempts[index]) {
			return false
		}
	}
	return true
}

func (snapshot Snapshot) input() SnapshotInput {
	return SnapshotInput{
		ManagedPaths:           snapshot.ManagedPaths(),
		ManagedAggregates:      snapshot.ManagedAggregates(),
		PendingCarrierInstalls: snapshot.PendingCarrierInstalls(),
		PendingCarrierRemovals: snapshot.PendingCarrierRemovals(),
		ManagedCarrierClaims:   snapshot.ManagedCarrierClaims(),
		DelegateAttempts:       snapshot.DelegateAttempts(),
		HostRouteAttempts:      snapshot.HostRouteAttempts(),
	}
}

func aggregatePhysicalKey(contribution aggregate.ManagedContribution) string {
	return string(contribution.Scope()) + "\x00" +
		contribution.AggregateRoot().String() + "\x00" +
		contribution.ContentPath()
}
