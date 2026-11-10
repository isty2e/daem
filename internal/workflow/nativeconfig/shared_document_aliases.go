package nativeconfig

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	physicalhostpath "github.com/isty2e/daem/internal/assurance/observe/hostpath"
	"github.com/isty2e/daem/internal/desired/entity"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func ordinaryDocumentAliasCandidates(input readiness.Input, before durable.Snapshot, selected []aggregate.DocumentAddress) ([]aggregate.DocumentAddress, error) {
	locations := make(map[configuration.AliasLocation]struct{}, len(selected))
	for _, address := range selected {
		binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot()})
		if err != nil {
			return nil, err
		}
		locations[binding.Location()] = struct{}{}
	}
	candidates := make(map[aggregate.DocumentAddress]struct{})
	add := func(contribution aggregate.ManagedContribution) error {
		if _, native := aggregate.HostConfigSyntaxForCodec(contribution.CodecContractID()); native {
			return nil
		}
		address := contribution.Contract().Address().Document()
		if !input.Selection.Includes(address.Target()) || slices.Contains(selected, address) {
			return nil
		}
		binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot()})
		if errors.Is(err, configuration.ErrDocumentPath) || errors.Is(err, configuration.ErrNoNativeSurface) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, sameLocation := locations[binding.Location()]; sameLocation {
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

func joinOrdinaryDocumentAliases(ctx context.Context, paths daempaths.Paths, roots journalnamespace.Roots, budget *physicalwork.Budget, addresses, candidates []aggregate.DocumentAddress) ([]aggregate.DocumentAddress, error) {
	resolver := physicalhostpath.NewResolver(paths)
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		native, err := journalnamespace.ObserveAlias(ctx, candidate, roots, budget)
		if err != nil {
			return nil, err
		}
		path, err := resolver.Resolve(candidate.AggregateRoot(), budget)
		if err != nil {
			return nil, fmt.Errorf("resolve ordinary document %q: %w", candidate.AggregateRoot(), err)
		}
		referent, err := rootedpath.ResolveFutureReferentPathBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, fmt.Errorf("resolve ordinary document referent %q: %w", candidate.AggregateRoot(), err)
		}
		ordinary, err := mutation.ObserveDirectoryEntryAuthorityBounded(referent, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, fmt.Errorf("observe ordinary document source %q: %w", candidate.AggregateRoot(), err)
		}
		// A shared static binding cannot redirect a divergent ordinary source.
		if ordinary.Equal(native) {
			addresses = append(addresses, candidate)
		}
	}
	return addresses, ctx.Err()
}
