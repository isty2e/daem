package readiness

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/realization/aggregate"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

type managedAggregatePlanningInputs struct {
	expected      []lock.LockedSubjectContract
	desired       []aggregate.SubjectContribution
	states        []durable.ManagedAggregateState
	evidence      []observe.AggregateEvidence
	failures      []observe.AggregateObservationFailure
	preconditions []observe.AggregatePreconditionEvidence
}

func (input managedAggregatePlanningInputs) ownershipProjections() []aggregate.ProjectionAddress {
	byAddress := make(map[aggregate.ProjectionAddress]struct{}, len(input.desired)+len(input.states))
	for _, desired := range input.desired {
		byAddress[desired.Contribution().Address()] = struct{}{}
	}
	for _, state := range input.states {
		byAddress[state.Contribution().Address()] = struct{}{}
	}
	result := make([]aggregate.ProjectionAddress, 0, len(byAddress))
	for address := range byAddress {
		result = append(result, address)
	}
	sort.Slice(result, func(left int, right int) bool {
		return aggregateProjectionKey(result[left]) < aggregateProjectionKey(result[right])
	})
	return result
}

type aggregateDocumentObservations struct {
	evidence      []observe.AggregateEvidence
	failures      []observe.AggregateObservationFailure
	preconditions []observe.AggregatePreconditionEvidence
}

func buildManagedAggregatePlanningInputs(
	ctx context.Context,
	resolver liveobserve.DestinationResolver,
	environment desired.Environment,
	locked lock.LockedSection,
	currentState durable.Snapshot,
	selection targetselection.Selection,
	hookEncoder commandhook.ContributionEncoder,
	mcpEncoder lockrefine.MCPContributionEncoder,
	codecs aggregate.CodecCatalog,
) (managedAggregatePlanningInputs, error) {
	lowered, err := topologyhook.Lower(environment.HookAssets(), environment.Hooks())
	if err != nil {
		return managedAggregatePlanningInputs{}, fmt.Errorf("lower Hook topology: %w", err)
	}
	expected, err := lockrefine.HookContributions(
		environment.Hooks(),
		lowered,
		hookEncoder,
	)
	if err != nil {
		return managedAggregatePlanningInputs{}, fmt.Errorf("lower expected Hook contributions: %w", err)
	}
	mcpExpected, err := lockrefine.MCPSubjects(
		environment.MCPServers(),
		environment.Extensions(),
		mcpEncoder,
	)
	if err != nil {
		return managedAggregatePlanningInputs{}, fmt.Errorf("lower expected MCP contributions: %w", err)
	}
	expected = append(expected, mcpExpected...)
	assetPaths, err := resolvedHookAssetPaths(resolver, locked, lowered, selection)
	if err != nil {
		return managedAggregatePlanningInputs{}, err
	}
	desiredContributions, err := commandhook.ContributionsWithAvailablePaths(
		environment.Hooks(),
		lowered,
		assetPaths,
		hookEncoder,
	)
	if err != nil {
		return managedAggregatePlanningInputs{}, fmt.Errorf("refine desired Hook contributions: %w", err)
	}
	mcpDesired, err := aggregateContributionsFromContracts(mcpExpected)
	if err != nil {
		return managedAggregatePlanningInputs{}, fmt.Errorf("refine desired MCP contributions: %w", err)
	}
	desiredContributions = append(desiredContributions, mcpDesired...)
	states := currentState.ManagedAggregates()
	observations, err := observeAggregateDocuments(
		ctx,
		resolver,
		desiredContributions,
		states,
		selection,
		codecs,
	)
	if err != nil {
		return managedAggregatePlanningInputs{}, err
	}
	return managedAggregatePlanningInputs{
		expected: expected, desired: desiredContributions, states: states,
		evidence: observations.evidence, failures: observations.failures,
		preconditions: observations.preconditions,
	}, nil
}

func aggregateContributionsFromContracts(
	contracts []lock.LockedSubjectContract,
) ([]aggregate.SubjectContribution, error) {
	result := make([]aggregate.SubjectContribution, 0, len(contracts))
	for index, contract := range contracts {
		contribution, present, err := contract.ManagedAggregateContribution()
		if err != nil {
			return nil, fmt.Errorf("aggregate contract[%d]: %w", index, err)
		}
		if !present {
			return nil, fmt.Errorf("aggregate contract[%d] has no aggregate realization", index)
		}
		result = append(result, contribution)
	}
	return result, nil
}

func resolvedHookAssetPaths(
	resolver liveobserve.DestinationResolver,
	locked lock.LockedSection,
	lowered topologyhook.Model,
	selection targetselection.Selection,
) (map[topology.SubjectID]string, error) {
	result := make(map[topology.SubjectID]string, len(lowered.AssetProjections()))
	for _, expected := range lowered.AssetProjections() {
		consumers := lowered.ConsumerTargetsOf(expected.SubjectID())
		if !selection.IncludesAny(consumers) {
			continue
		}
		contract, present := locked.Subject(expected.SubjectID())
		if !present {
			continue
		}
		if contract.EntityID() != expected.EntityID() {
			return nil, fmt.Errorf("HookAsset subject %q changes desired identity", expected.SubjectID())
		}
		realization, realized := contract.Realization()
		if !realized {
			return nil, fmt.Errorf("HookAsset subject %q has no path realization", expected.SubjectID())
		}
		projection, managedPath := realization.ManagedPathProjection()
		if !managedPath {
			return nil, fmt.Errorf("HookAsset subject %q is not a managed path", expected.SubjectID())
		}
		if !slices.Equal(projection.ConsumerTargets(), consumers) {
			return nil, fmt.Errorf("HookAsset subject %q consumer targets differ from topology", expected.SubjectID())
		}
		physical, err := resolver(projection.Destination())
		if err != nil {
			return nil, fmt.Errorf("resolve HookAsset %q path: %w", contract.EntityID().Name(), err)
		}
		result[expected.SubjectID()] = physical
	}
	return result, nil
}

func observeAggregateDocuments(
	ctx context.Context,
	resolver liveobserve.DestinationResolver,
	desiredContributions []aggregate.SubjectContribution,
	states []durable.ManagedAggregateState,
	selection targetselection.Selection,
	codecs aggregate.CodecCatalog,
) (aggregateDocumentObservations, error) {
	contractsByDocument := make(map[aggregate.DocumentAddress]map[aggregate.ProjectionAddress]aggregate.ProjectionContract)
	addContract := func(contract aggregate.ProjectionContract) error {
		address := contract.Address()
		if !selection.Includes(address.Document().Target()) {
			return nil
		}
		document := address.Document()
		contracts := contractsByDocument[document]
		if contracts == nil {
			contracts = make(map[aggregate.ProjectionAddress]aggregate.ProjectionContract)
			contractsByDocument[document] = contracts
		}
		if previous, duplicate := contracts[address]; duplicate && !previous.Equal(contract) {
			return fmt.Errorf("aggregate document contains contract drift at %q", address.ContentPath())
		}
		contracts[address] = contract.Clone()
		return nil
	}
	for _, item := range desiredContributions {
		if err := addContract(item.Contribution().Contract()); err != nil {
			return aggregateDocumentObservations{}, err
		}
	}
	for _, state := range states {
		if err := addContract(state.Contribution().Contract()); err != nil {
			return aggregateDocumentObservations{}, err
		}
	}
	managedSubjects := make(map[topology.SubjectID]struct{}, len(states))
	for _, state := range states {
		if !selection.Includes(state.Contribution().Target()) {
			continue
		}
		if _, duplicate := managedSubjects[state.Subject()]; duplicate {
			return aggregateDocumentObservations{}, fmt.Errorf(
				"duplicate managed aggregate state for subject %q",
				state.Subject(),
			)
		}
		managedSubjects[state.Subject()] = struct{}{}
	}
	documents := make([]aggregate.DocumentAddress, 0, len(contractsByDocument))
	for document := range contractsByDocument {
		documents = append(documents, document)
	}
	sort.Slice(documents, func(left int, right int) bool {
		return aggregateDocumentKey(documents[left]) < aggregateDocumentKey(documents[right])
	})
	result := aggregateDocumentObservations{
		evidence:      make([]observe.AggregateEvidence, 0, len(documents)),
		failures:      make([]observe.AggregateObservationFailure, 0),
		preconditions: make([]observe.AggregatePreconditionEvidence, 0),
	}
	for _, address := range documents {
		contracts := make([]aggregate.ProjectionContract, 0, len(contractsByDocument[address]))
		for _, contract := range contractsByDocument[address] {
			contracts = append(contracts, contract)
		}
		selected, err := aggregate.NewSelection(contracts)
		if err != nil {
			return aggregateDocumentObservations{}, err
		}
		codec, ok := codecs.Lookup(selected.CodecContractID())
		if !ok {
			return aggregateDocumentObservations{}, fmt.Errorf("aggregate codec %q is not admitted", selected.CodecContractID())
		}
		preconditions, admitted, err := aggregate.OperationPreconditionsForSelection(selected)
		if err != nil {
			return aggregateDocumentObservations{}, err
		}
		if !admitted {
			return aggregateDocumentObservations{}, fmt.Errorf(
				"aggregate codec %q has no operation-precondition profile",
				selected.CodecContractID(),
			)
		}
		for _, precondition := range preconditions {
			satisfied, err := observeAggregatePrecondition(ctx, resolver, precondition)
			if err != nil {
				return aggregateDocumentObservations{}, err
			}
			observed, err := observe.NewAggregatePreconditionEvidence(address, precondition, satisfied)
			if err != nil {
				return aggregateDocumentObservations{}, err
			}
			result.preconditions = append(result.preconditions, observed)
		}
		logical := address.AggregateRoot()
		if err := liveobserve.ValidateAggregateReadPreconditions(ctx, logical, resolver); err != nil {
			return aggregateDocumentObservations{}, err
		}
		physical, err := resolver(logical)
		if err != nil {
			return aggregateDocumentObservations{}, fmt.Errorf("resolve aggregate document %q: %w", logical, err)
		}
		document, mode, err := readAggregateDocument(ctx, physical, codec.MaximumDocumentBytes())
		if err != nil {
			return aggregateDocumentObservations{}, fmt.Errorf("read aggregate document %q: %w", logical, err)
		}
		snapshot, failure := codec.Read(document, selected)
		if failure != nil {
			failed, err := observe.NewAggregateObservationFailure(document, selected, mode, failure)
			if err != nil {
				return aggregateDocumentObservations{}, err
			}
			result.failures = append(result.failures, failed)
			continue
		}
		observed, err := observe.NewAggregateEvidence(document, snapshot, mode)
		if err != nil {
			return aggregateDocumentObservations{}, err
		}
		result.evidence = append(result.evidence, observed)
	}
	return result, nil
}

func aggregateDocumentKey(address aggregate.DocumentAddress) string {
	return string(address.Target()) + "\x00" + string(address.Scope()) + "\x00" + address.AggregateRoot().String()
}

func aggregateProjectionKey(address aggregate.ProjectionAddress) string {
	return aggregateDocumentKey(address.Document()) + "\x00" +
		address.PlacementID() + "\x00" +
		string(address.MergeUnit()) + "\x00" +
		string(address.ContentPath())
}
