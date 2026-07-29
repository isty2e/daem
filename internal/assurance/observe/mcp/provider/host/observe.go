// Package host dispatches current MCP provider observation to admitted
// host-specific package observers.
package host

import (
	"context"
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	mcpprovider "github.com/isty2e/daem/internal/assurance/observe/mcp/provider"
	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// Input contains selected locked MCP contracts and current operation paths.
type Input struct {
	Paths     daempaths.Paths
	Locked    lock.File
	Contracts []lock.LockedSubjectContract
}

// Observe returns one current provider observation per selected contribution.
func Observe(ctx context.Context, input Input) ([]mcpprovider.Observation, error) {
	groups, err := providerConsumerGroups(input.Contracts)
	if err != nil {
		return nil, err
	}
	result := make([]mcpprovider.Observation, 0, len(groups))
	for _, group := range groups {
		carrier, err := providerCarrierIdentity(input.Locked.Locked, group.reference)
		if err != nil {
			return nil, err
		}
		observation, err := observeProviderGroup(ctx, input.Paths, carrier, group)
		if err != nil {
			return nil, err
		}
		result = append(result, observation)
	}
	return result, nil
}

type providerConsumerGroup struct {
	reference extensiontopology.ContributionReference
	consumers []topology.SubjectID
}

func providerConsumerGroups(
	contracts []lock.LockedSubjectContract,
) ([]providerConsumerGroup, error) {
	byReference := make(map[string]providerConsumerGroup)
	for _, contract := range contracts {
		reference, mediated := contract.MCPProviderContribution()
		if !mediated {
			continue
		}
		key := reference.SubjectID().String()
		group, exists := byReference[key]
		if exists && !group.reference.Equal(reference) {
			return nil, fmt.Errorf("MCP provider contribution key collision for %q", key)
		}
		if !exists {
			group.reference = reference
		}
		group.consumers = append(group.consumers, contract.SubjectID())
		byReference[key] = group
	}
	keys := make([]string, 0, len(byReference))
	for key := range byReference {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]providerConsumerGroup, 0, len(keys))
	for _, key := range keys {
		result = append(result, byReference[key])
	}
	return result, nil
}

func providerCarrierIdentity(
	locked lock.LockedSection,
	reference extensiontopology.ContributionReference,
) (durablecarrier.ManagedCarrierIdentity, error) {
	var matched durablecarrier.ManagedCarrierIdentity
	found := false
	for _, contract := range locked.Subjects() {
		identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
		if err != nil {
			return durablecarrier.ManagedCarrierIdentity{}, err
		}
		if !admitted || identity.CarrierSubject() != reference.ProviderSubjectID() {
			continue
		}
		if found {
			return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf(
				"MCP provider carrier %q has multiple locked relations",
				reference.ProviderSubjectID(),
			)
		}
		matched = identity
		found = true
	}
	if !found {
		return durablecarrier.ManagedCarrierIdentity{}, fmt.Errorf(
			"MCP provider carrier %q has no locked relation",
			reference.ProviderSubjectID(),
		)
	}
	return matched, nil
}

func observeProviderGroup(
	ctx context.Context,
	paths daempaths.Paths,
	identity durablecarrier.ManagedCarrierIdentity,
	group providerConsumerGroup,
) (mcpprovider.Observation, error) {
	if identity.Target() != target.TargetPi {
		return mcpprovider.Observation{}, fmt.Errorf(
			"provider-mediated MCP target %q has no current-version observer",
			identity.Target(),
		)
	}
	inventory, err := observepipackage.ReadSettings(observepipackage.SettingsInput{
		WorkDir:     paths.ManifestRoot,
		ProjectRoot: paths.ManifestRoot,
		Scope:       identity.Scope(),
	})
	if err != nil {
		return mcpprovider.NewObservation(mcpprovider.ObservationInput{
			Contribution: group.reference,
			Carrier:      identity,
			Consumers:    group.consumers,
			State:        mcpprovider.StateUnobservable,
			Detail:       "Pi package settings cannot be observed exactly",
		})
	}
	version := observepipackage.ObserveNPMVersion(ctx, inventory, identity.Carrier())
	switch version.State() {
	case observepipackage.VersionAbsent:
		return mcpprovider.NewObservation(mcpprovider.ObservationInput{
			Contribution: group.reference,
			Carrier:      identity,
			Consumers:    group.consumers,
			State:        mcpprovider.StateAbsent,
		})
	case observepipackage.VersionUnobservable:
		return mcpprovider.NewObservation(mcpprovider.ObservationInput{
			Contribution: group.reference,
			Carrier:      identity,
			Consumers:    group.consumers,
			State:        mcpprovider.StateUnobservable,
			Detail:       version.Detail(),
		})
	case observepipackage.VersionExact:
		mapped, err := profile.MCPProviderCodecForCurrentVersion(
			identity.Target(),
			identity.Carrier(),
			group.reference,
			version.Version(),
		)
		if err != nil {
			return mcpprovider.NewObservation(mcpprovider.ObservationInput{
				Contribution: group.reference,
				Carrier:      identity,
				Consumers:    group.consumers,
				State:        mcpprovider.StateIncompatible,
				Version:      version.Version(),
				Detail:       err.Error(),
			})
		}
		return mcpprovider.NewObservation(mcpprovider.ObservationInput{
			Contribution: group.reference,
			Carrier:      identity,
			Consumers:    group.consumers,
			State:        mcpprovider.StateCurrent,
			Version:      version.Version(),
			MappedCodec:  mapped,
		})
	default:
		return mcpprovider.Observation{}, fmt.Errorf(
			"Pi MCP provider version observation has unsupported state %q",
			version.State(),
		)
	}
}
