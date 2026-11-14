package qualification

import (
	"context"
	"errors"
	"fmt"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type ordinaryWriterAddress struct {
	scope       target.Scope
	destination output.Destination
}

func QualifyWriterPartition(ctx context.Context, entries []journalrecord.PathEntry, namespaces []journalnamespace.DocumentNamespace, manifest journalnamespace.RootGrant, ordinaryCapabilities journalnamespace.CapabilityResolver, budget *physicalwork.Budget, protected []pathauthority.Exact) error {
	var partition journalrecord.WriterPartition
	appendObservation := func(observation mutation.DirectoryEntryAuthorityObservation, group int) error {
		if err := observation.Validate(); err != nil {
			return err
		}
		return partition.Add(observation.NamespaceKey(), group)
	}
	for index, namespace := range namespaces {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := appendObservation(namespace.Observation(), index); err != nil {
			return err
		}
		for _, alias := range namespace.Aliases() {
			observation, err := alias.ObserveRoute(ctx, budget)
			if err != nil {
				return err
			}
			if err := appendObservation(observation, index); err != nil {
				return err
			}
		}
	}
	seen := make(map[ordinaryWriterAddress]journalrecord.GlobalBindingKey)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		scope, destination, err := entry.WriterAddress()
		if err != nil {
			return err
		}
		address := ordinaryWriterAddress{scope, destination}
		binding := entry.BindingIdentity()
		if previous, duplicate := seen[address]; duplicate {
			if previous != binding {
				return fmt.Errorf("ordinary writer repeats a logical address with different root evidence")
			}
			continue
		}
		seen[address] = binding
		var observation mutation.DirectoryEntryAuthorityObservation
		switch scope {
		case target.ScopeProject:
			if destination.RootRole() != output.RootProject {
				return fmt.Errorf("ordinary project writer has no project destination")
			}
			observation, err = manifest.ObserveRoute(ctx, destination, budget)
		case target.ScopeGlobal:
			capability, acquireErr := acquireNativeOrdinaryGlobalWriter(entry, destination, ordinaryCapabilities, budget)
			if acquireErr != nil {
				return acquireErr
			}

			observation, err = journalnamespace.ObserveWriterCapability(ctx, capability, budget)
		default:
			err = fmt.Errorf("ordinary writer has an unsupported scope")
		}
		if err != nil {
			return err
		}
		if err := appendObservation(observation, -1); err != nil {
			return err
		}
	}
	for index, path := range protected {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := path.Validate(); err != nil {
			return err
		}
		if err := partition.Add(path.Key(), -2-index); err != nil {
			return err
		}
	}
	return partition.Validate(ctx)
}

func acquireNativeOrdinaryGlobalWriter(entry journalrecord.PathEntry, destination output.Destination, acquire journalnamespace.CapabilityResolver, budget *physicalwork.Budget) (rootedpath.CommitCapability, error) {
	if !entry.HasGlobalBinding() || acquire == nil {
		return nil, fmt.Errorf("ordinary global writer requires independently selected capability and stored binding")
	}
	capability, present, err := journalnamespace.AcquireMatchingCapability(destination, entry.GlobalPath(), acquire, budget)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, fmt.Errorf("ordinary global writer has no selected capability")
	}
	err = entry.MatchGlobalRoot(capability.Destination().Root())
	if err != nil {
		return nil, errors.Join(err, capability.Close())
	}
	return capability, nil
}
