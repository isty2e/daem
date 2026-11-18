package nativeconfig

import (
	"errors"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func ordinaryDocumentAliasCandidates(input readiness.Input, before durable.Snapshot, selected []aggregate.DocumentAddress) ([]aggregate.DocumentAddress, error) {
	locations := make(map[configuration.AliasLocation]struct{}, len(selected))
	roots := make(map[output.RootReference]struct{})
	selectedAddresses := make(map[aggregate.DocumentAddress]struct{}, len(selected))
	for _, address := range selected {
		binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot()})
		if err != nil {
			return nil, err
		}
		locations[binding.Location()] = struct{}{}
		roots[binding.RootReference()] = struct{}{}
		selectedAddresses[address] = struct{}{}
	}
	candidates := make(map[aggregate.DocumentAddress]struct{})
	add := func(contribution aggregate.ManagedContribution) error {
		if _, native := aggregate.AgentConfigSyntaxForCodec(contribution.CodecContractID()); native {
			return nil
		}
		address := contribution.Contract().Address().Document()
		if _, observed := selectedAddresses[address]; observed || !input.Selection.Includes(address.Target()) {
			return nil
		}
		binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot()})
		if errors.Is(err, configuration.ErrDocumentPath) || errors.Is(err, configuration.ErrNoNativeSurface) {
			return nil
		}
		if err != nil {
			return err
		}
		_, sameLocation := locations[binding.Location()]
		_, selectedRoot := roots[binding.RootReference()]
		_, mcp := aggregate.MCPPlacementForCodec(aggregate.MCPPlacementID(contribution.PlacementID()), contribution.CodecContractID())
		if sameLocation || selectedRoot && mcp {
			candidates[address] = struct{}{}
		}
		return nil
	}
	if input.NativeReplacement == nil {
		declared := make(map[entity.ID]struct{})
		for _, server := range input.Environment.MCPServers() {
			declared[server.ID()] = struct{}{}
		}
		for _, hook := range input.Environment.Hooks() {
			declared[hook.ID()] = struct{}{}
		}
		for _, extension := range input.Environment.Extensions() {
			declared[extension.ID()] = struct{}{}
		}
		for _, subject := range input.Lockfile.Locked.Subjects() {
			if _, selected := declared[subject.EntityID()]; !selected {
				continue
			}
			item, present, err := subject.ManagedAggregateContribution()
			if err != nil {
				return nil, err
			}
			if present {
				if err := add(item.Contribution()); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, state := range before.ManagedAggregates() {
		if err := add(state.Contribution()); err != nil {
			return nil, err
		}
	}
	result := make([]aggregate.DocumentAddress, 0, len(candidates))
	for candidate := range candidates {
		result = append(result, candidate)
	}
	slices.SortFunc(result, journaldocument.CompareAliases)
	return result, nil
}
