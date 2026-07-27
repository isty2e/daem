package projection

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
)

// AggregateInput contains reproducible lock facts, ephemeral desired values,
// fresh codec evidence, and durable prior ownership. It performs no I/O.
type AggregateInput struct {
	Locked                 lock.LockedSection
	Expected               []lock.LockedSubjectContract
	Desired                []aggregate.SubjectContribution
	States                 []durable.ManagedAggregateState
	Evidence               []observe.AggregateEvidence
	ObservationFailures    []observe.AggregateObservationFailure
	PreconditionEvidence   []observe.AggregatePreconditionEvidence
	SelectedTargets        reconcile.SelectedTargets
	ManageUnmanagedMatches bool
	Owner                  stateauthority.Authority
	Ownership              []observe.OwnershipObservation
	Codecs                 aggregate.CodecCatalog
}

type aggregateGroupInput struct {
	contract aggregate.ProjectionContract
	desired  []aggregate.SubjectContribution
	previous []durable.ManagedAggregateState
	blocked  map[topology.SubjectID]aggregateBlockedSubject
}

type aggregateBlockedSubject struct {
	item   aggregate.SubjectContribution
	reason reconcile.ActionReason
	detail string
}

// BuildAggregateDecisions reconciles semantic projections by address, then
// batches every projection sharing one physical document and codec.
func BuildAggregateDecisions(input AggregateInput) ([]reconcile.AggregateDecision, error) {
	selection := selectedTargetSet(input.SelectedTargets)
	expected, blocked, err := expectedAggregateSubjects(
		input.Locked,
		input.Expected,
		selection,
		input.Codecs,
	)
	if err != nil {
		return nil, err
	}
	desired, err := desiredAggregateSubjects(input.Desired, expected, selection)
	if err != nil {
		return nil, err
	}
	states, err := selectedAggregateStates(input.States, selection)
	if err != nil {
		return nil, err
	}
	evidence, err := aggregateEvidenceIndex(input.Evidence)
	if err != nil {
		return nil, err
	}
	failures, err := aggregateObservationFailureIndex(input.ObservationFailures)
	if err != nil {
		return nil, err
	}
	preconditions, err := aggregatePreconditionEvidenceIndex(input.PreconditionEvidence)
	if err != nil {
		return nil, err
	}
	ownershipEvidence, ownershipConflicts, err := ownershipObservations(input.Ownership)
	if err != nil {
		return nil, err
	}

	groups := make(map[aggregate.ProjectionAddress]*aggregateGroupInput)
	for subject, contribution := range desired {
		group, err := aggregateGroup(groups, contribution.Contribution().Contract())
		if err != nil {
			return nil, fmt.Errorf("desired aggregate subject %q: %w", subject, err)
		}
		group.desired = append(group.desired, contribution)
	}
	for subject, state := range states {
		group, err := aggregateGroup(groups, state.Contribution().Contract())
		if err != nil {
			return nil, fmt.Errorf("managed aggregate subject %q: %w", subject, err)
		}
		group.previous = append(group.previous, state)
	}
	blockedBySubject := make(map[topology.SubjectID]aggregateBlockedSubject, len(blocked))
	for _, fact := range blocked {
		blockedBySubject[fact.item.SubjectID()] = fact
		group, groupErr := aggregateGroup(groups, fact.item.Contribution().Contract())
		if groupErr != nil {
			return nil, fmt.Errorf("blocked aggregate subject %q: %w", fact.item.SubjectID(), groupErr)
		}
		if group.blocked == nil {
			group.blocked = make(map[topology.SubjectID]aggregateBlockedSubject)
		}
		group.blocked[fact.item.SubjectID()] = fact
	}
	for _, group := range groups {
		for _, subject := range aggregateGroupSubjects(*group) {
			fact, subjectBlocked := blockedBySubject[subject]
			if !subjectBlocked {
				continue
			}
			if group.blocked == nil {
				group.blocked = make(map[topology.SubjectID]aggregateBlockedSubject)
			}
			if _, exact := group.blocked[subject]; exact {
				continue
			}
			item, itemErr := aggregateSubjectItem(*group, subject)
			if itemErr != nil {
				return nil, itemErr
			}
			group.blocked[subject] = aggregateBlockedSubject{
				item: item, reason: reconcile.ReasonAggregateLockBlocked,
				detail: "aggregate subject is blocked by lock readiness in another projection",
			}
			_ = fact
		}
	}

	documents, err := aggregateDocumentGroups(groups)
	if err != nil {
		return nil, err
	}
	if err := validateAggregateObservationCoverage(documents, evidence, failures, preconditions); err != nil {
		return nil, err
	}
	addresses := make([]aggregate.DocumentAddress, 0, len(documents))
	for address := range documents {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(left int, right int) bool {
		return aggregateDocumentAddressKey(addresses[left]) < aggregateDocumentAddressKey(addresses[right])
	})
	drafts := make([]aggregateDecision, 0, len(addresses))
	for _, address := range addresses {
		decision, err := reconcileAggregateDocument(
			documents[address],
			evidence,
			failures,
			preconditions,
			input.ManageUnmanagedMatches,
			input.Owner,
			ownershipEvidence,
			ownershipConflicts,
			input.Codecs,
		)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, decision)
	}
	sort.SliceStable(drafts, func(left int, right int) bool {
		return aggregateDecisionKey(drafts[left]) < aggregateDecisionKey(drafts[right])
	})
	return canonicalAggregateDecisions(drafts)
}

func aggregateGroup(
	groups map[aggregate.ProjectionAddress]*aggregateGroupInput,
	contract aggregate.ProjectionContract,
) (*aggregateGroupInput, error) {
	address := contract.Address()
	if group := groups[address]; group != nil {
		if !group.contract.Equal(contract) {
			return nil, fmt.Errorf("contributors at one projection address have different contracts")
		}
		return group, nil
	}
	group := &aggregateGroupInput{contract: contract}
	groups[address] = group
	return group, nil
}

func aggregateDocumentGroups(
	groups map[aggregate.ProjectionAddress]*aggregateGroupInput,
) (map[aggregate.DocumentAddress][]aggregateGroupInput, error) {
	documents := make(map[aggregate.DocumentAddress][]aggregateGroupInput)
	for _, group := range groups {
		address := group.contract.Address().Document()
		documents[address] = append(documents[address], *group)
	}
	for address, values := range documents {
		sort.Slice(values, func(left int, right int) bool {
			return aggregateAddressKey(values[left].contract.Address()) <
				aggregateAddressKey(values[right].contract.Address())
		})
		contracts := make([]aggregate.ProjectionContract, len(values))
		for index, group := range values {
			contracts[index] = group.contract
		}
		if _, err := aggregate.NewSelection(contracts); err != nil {
			return nil, fmt.Errorf("aggregate document %q: %w", address.AggregateRoot(), err)
		}
		documents[address] = values
	}
	return documents, nil
}
