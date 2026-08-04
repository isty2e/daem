package projection

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
)

func reconcileAggregateDocument(
	groups []aggregateGroupInput,
	evidenceByDocument map[aggregate.DocumentAddress]observe.AggregateEvidence,
	failuresByDocument map[aggregate.DocumentAddress]observe.AggregateObservationFailure,
	preconditionsByDocument map[aggregate.DocumentAddress][]observe.AggregatePreconditionEvidence,
	manageUnmanagedMatches bool,
	owner stateauthority.Authority,
	ownershipEvidence map[ownershipObservationKey]observe.OwnershipObservation,
	ownershipConflicts map[ownershipObservationKey]struct{},
	codecs aggregate.CodecCatalog,
) (aggregateDecision, error) {
	if len(groups) == 0 {
		return aggregateDecision{}, fmt.Errorf("aggregate document group is empty")
	}
	contracts := make([]aggregate.ProjectionContract, len(groups))
	for index, group := range groups {
		contracts[index] = group.contract
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		return aggregateDecision{}, err
	}
	documentAddress := selection.DocumentAddress()
	if failure, failed := failuresByDocument[documentAddress]; failed {
		if err := validateAggregateSelection(failure.Selection(), selection); err != nil {
			return aggregateDecision{}, fmt.Errorf("aggregate failed observation: %w", err)
		}
		reason := reconcile.ReasonInvalidDesiredState
		if manageUnmanagedMatches &&
			failure.Document().Exists() &&
			!aggregateGroupsHavePreviousState(groups) {
			reason = reconcile.ReasonUnmanagedOutputExists
		}
		decision := blockedAggregateDocument(
			groups,
			documentAddress,
			selection.CodecContractID(),
			reason,
			failure.Error(),
		)
		for index := range decision.projections {
			decision.projections[index] = enforceAggregateProjectionOwnership(
				decision.projections[index],
				owner,
				ownershipEvidence,
				ownershipConflicts,
			)
		}
		decision.reason, decision.detail = firstAggregateProjectionFailure(
			decision.projections,
		)
		return decision, nil
	}
	evidence, observed := evidenceByDocument[documentAddress]
	if !observed {
		if aggregateGroupsHaveBlockedSubjects(groups) {
			return blockedAggregateDocumentWithoutEvidence(
				groups,
				documentAddress,
				selection.CodecContractID(),
			), nil
		}
		return blockedAggregateDocument(
			groups,
			documentAddress,
			selection.CodecContractID(),
			reconcile.ReasonMissingLiveObservation,
			"fresh aggregate evidence is required",
		), nil
	}
	if err := validateAggregateEvidenceSelection(evidence, selection); err != nil {
		return aggregateDecision{}, err
	}
	operationPreconditions, blockReason, blockDetail, err := reconcileAggregatePreconditions(
		selection,
		preconditionsByDocument[documentAddress],
	)
	if err != nil {
		return aggregateDecision{}, err
	}
	if blockReason != "" {
		return blockedAggregateDocument(
			groups,
			documentAddress,
			selection.CodecContractID(),
			blockReason,
			blockDetail,
		), nil
	}
	codec, ok := codecs.Lookup(selection.CodecContractID())
	if !ok {
		return aggregateDecision{}, fmt.Errorf("aggregate codec %q is not admitted", selection.CodecContractID())
	}
	currentByAddress := make(map[aggregate.ProjectionAddress]aggregate.ProjectionState, len(groups))
	for _, state := range evidence.Snapshot().States() {
		currentByAddress[state.Contract().Address()] = state
	}

	projections := make([]aggregateProjectionDecision, 0, len(groups))
	intents := make([]aggregate.ProjectionIntent, 0, len(groups))
	preBlocked := false
	for _, group := range groups {
		current := currentByAddress[group.contract.Address()]
		projection, intent, err := prepareAggregateProjection(codec, group, current)
		if err != nil {
			return aggregateDecision{}, err
		}
		projections = append(projections, projection)
		if projection.kind == reconcile.AggregateBlocked {
			preBlocked = true
			continue
		}
		intents = append(intents, intent)
	}
	if preBlocked {
		return finalizeBlockedAggregateDocument(
			documentAddress,
			selection.CodecContractID(),
			projections,
			evidence,
		), nil
	}
	codecPlan, err := aggregate.NewPlan(evidence.Snapshot(), intents)
	if err != nil {
		return aggregateDecision{}, err
	}
	rendered, failure := codec.Render(evidence.Document(), codecPlan)
	if failure != nil {
		for index := range projections {
			projections[index] = blockAggregateProjection(
				projections[index],
				reconcile.ReasonInvalidDesiredState,
				failure.Error(),
			)
		}
		return finalizeBlockedAggregateDocument(
			documentAddress,
			selection.CodecContractID(),
			projections,
			evidence,
		), nil
	}
	expectedByAddress := make(map[aggregate.ProjectionAddress]aggregate.ProjectionState, len(projections))
	for _, state := range rendered.Expected().States() {
		expectedByAddress[state.Contract().Address()] = state
	}
	blocked := false
	for index := range projections {
		projection := projections[index]
		projection.expected = expectedByAddress[projection.contract.Address()]
		projection, err = classifyAggregateProjection(
			codec,
			projection,
			evidence.FileMode(),
			manageUnmanagedMatches,
		)
		if err != nil {
			return aggregateDecision{}, err
		}
		projection = enforceAggregateProjectionOwnership(
			projection,
			owner,
			ownershipEvidence,
			ownershipConflicts,
		)
		if projection.kind == reconcile.AggregateBlocked {
			blocked = true
		}
		projections[index] = projection
	}
	decision := aggregateDecision{
		documentAddress: documentAddress,
		codecContractID: selection.CodecContractID(),
		projections:     projections,
		document:        evidence.Document(),
		snapshot:        evidence.Snapshot(),
		codecPlan:       codecPlan,
		rendered:        rendered,
		evidence:        evidence,
		preconditions:   operationPreconditions,
	}
	if blocked {
		decision.kind = reconcile.AggregateBlocked
		decision.reason, decision.detail = firstAggregateProjectionFailure(projections)
		decision.disableHostMutation()
		return decision, nil
	}
	decision.kind, decision.reason = classifyAggregateDocument(
		evidence.Document(),
		evidence.FileMode(),
		rendered.Document(),
		projections,
	)
	if decision.kind == reconcile.AggregateRecord || decision.kind == reconcile.AggregateNoOp {
		rendered, err = aggregate.NewRenderedDocument(
			evidence.Document(),
			codecPlan,
			rendered.Expected(),
		)
		if err != nil {
			return aggregateDecision{}, fmt.Errorf(
				"retain aggregate document for state-only decision: %w",
				err,
			)
		}
		decision.rendered = rendered
	}
	decision.enableHostMutation()
	return decision, nil
}

func aggregateGroupsHaveBlockedSubjects(groups []aggregateGroupInput) bool {
	for _, group := range groups {
		if len(group.blocked) != 0 {
			return true
		}
	}
	return false
}

func aggregateGroupsHavePreviousState(groups []aggregateGroupInput) bool {
	for _, group := range groups {
		if len(group.previous) != 0 {
			return true
		}
	}
	return false
}

func prepareAggregateProjection(
	codec aggregate.Codec,
	group aggregateGroupInput,
	current aggregate.ProjectionState,
) (aggregateProjectionDecision, aggregate.ProjectionIntent, error) {
	desired, err := aggregateContributionSet(group.desired)
	if err != nil {
		return aggregateProjectionDecision{}, aggregate.ProjectionIntent{}, err
	}
	previous, err := aggregateStateContributionSet(group.previous)
	if err != nil {
		return aggregateProjectionDecision{}, aggregate.ProjectionIntent{}, err
	}
	projection := aggregateProjectionDecision{
		contract: group.contract,
		desired:  cloneContributionSetPointer(desired),
		previous: append([]durable.ManagedAggregateState(nil), group.previous...),
		before:   current,
	}
	if len(group.blocked) != 0 {
		return blockAggregateProjectionFromFacts(projection, group), aggregate.ProjectionIntent{}, nil
	}
	if previous != nil {
		previousProjection, err := canonicalAggregateProjection(codec, group.contract, *previous)
		if err != nil {
			return aggregateProjectionDecision{}, aggregate.ProjectionIntent{}, err
		}
		projection.managedBaseline = previousProjection
		projection.hasManagedBaseline = true
		if !current.Present() {
			return blockAggregateProjection(
				projection,
				reconcile.ReasonMissingOutput,
				"managed aggregate projection is missing",
			), aggregate.ProjectionIntent{}, nil
		}
	}
	intent, err := aggregate.NewProjectionIntent(current, desired)
	if err != nil {
		return aggregateProjectionDecision{}, aggregate.ProjectionIntent{}, err
	}
	return projection, intent, nil
}

func aggregateContributionSet(
	items []aggregate.SubjectContribution,
) (*aggregate.ContributionSet, error) {
	if len(items) == 0 {
		return nil, nil
	}
	set, err := aggregate.NewContributionSet(items)
	if err != nil {
		return nil, err
	}
	return &set, nil
}

func aggregateStateContributionSet(
	states []durable.ManagedAggregateState,
) (*aggregate.ContributionSet, error) {
	if len(states) == 0 {
		return nil, nil
	}
	items := make([]aggregate.SubjectContribution, 0, len(states))
	for _, state := range states {
		item, err := aggregate.NewSubjectContribution(state.Subject(), state.Contribution())
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return aggregateContributionSet(items)
}

func validateAggregateEvidenceSelection(
	evidence observe.AggregateEvidence,
	expected aggregate.Selection,
) error {
	actual, err := evidence.Snapshot().Selection()
	if err != nil {
		return err
	}
	return validateAggregateSelection(actual, expected)
}

func validateAggregateSelection(
	actual aggregate.Selection,
	expected aggregate.Selection,
) error {
	if actual.DocumentAddress() != expected.DocumentAddress() ||
		actual.CodecContractID() != expected.CodecContractID() {
		return fmt.Errorf("aggregate evidence does not cover the selected document and codec")
	}
	actualContracts := actual.Contracts()
	expectedContracts := expected.Contracts()
	if len(actualContracts) != len(expectedContracts) {
		return fmt.Errorf(
			"aggregate evidence projection count = %d, want %d",
			len(actualContracts),
			len(expectedContracts),
		)
	}
	for index := range actualContracts {
		if !actualContracts[index].Equal(expectedContracts[index]) {
			return fmt.Errorf(
				"aggregate evidence does not exactly cover projection %q",
				expectedContracts[index].Address().ContentPath(),
			)
		}
	}
	return nil
}

func reconcileAggregatePreconditions(
	selection aggregate.Selection,
	evidence []observe.AggregatePreconditionEvidence,
) ([]aggregate.OperationPrecondition, reconcile.ActionReason, string, error) {
	expected, admitted, err := aggregate.OperationPreconditionsForSelection(selection)
	if err != nil {
		return nil, "", "", err
	}
	if !admitted {
		return nil, "", "", fmt.Errorf(
			"aggregate codec %q has no operation-precondition profile",
			selection.CodecContractID(),
		)
	}
	if len(evidence) != len(expected) {
		return nil, reconcile.ReasonMissingLiveObservation,
			fmt.Sprintf(
				"aggregate operation precondition evidence count = %d, want %d",
				len(evidence),
				len(expected),
			), nil
	}
	byPrecondition := make(map[aggregate.OperationPrecondition]observe.AggregatePreconditionEvidence, len(evidence))
	for _, item := range evidence {
		precondition := item.Precondition()
		if _, duplicate := byPrecondition[precondition]; duplicate {
			return nil, "", "", fmt.Errorf("duplicate aggregate operation precondition evidence")
		}
		byPrecondition[precondition] = item
	}
	for _, precondition := range expected {
		item, present := byPrecondition[precondition]
		if !present {
			return nil, reconcile.ReasonMissingLiveObservation,
				"fresh aggregate operation precondition evidence is required", nil
		}
		if !item.Satisfied() {
			return nil, reconcile.ReasonInvalidDesiredState,
				precondition.UnsatisfiedDetail(), nil
		}
	}
	return expected, "", "", nil
}
