// Package ownership converts resolved global outputs and the shared claim registry into planner observations.
package ownership

import (
	"fmt"
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// DestinationResolver resolves one output destination to its current physical path.
type DestinationResolver func(output.Destination) (string, error)

// Input contains normalized output/state facts and one already-loaded registry snapshot.
type Input struct {
	Paths           daempaths.Paths
	Resolver        DestinationResolver
	ManagedPaths    []ManagedPathInput
	Aggregates      []aggregate.ProjectionAddress
	StatePaths      []durable.ManagedPathState
	StateAggregates []durable.ManagedAggregateState
	Selection       targetselection.Selection
	Registry        outputownership.Registry
}

// ManagedPathInput is the address-only boundary view required for ownership
// observation. It carries no content, state, or reconciliation policy.
type ManagedPathInput struct {
	Scope           target.Scope
	Destination     output.Destination
	ConsumerTargets []target.Target
}

// Result contains the selected state authority and global managed-address observations.
type Result struct {
	Owner        stateauthority.Authority
	Observations []observe.OwnershipObservation
}

// Build resolves and canonicalizes global output identities without mutating the registry.
func Build(input Input) (Result, error) {
	if input.Resolver == nil {
		return Result{}, fmt.Errorf("ownership observation destination resolver is required")
	}
	statefileAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(input.Paths.StatefilePath)
	if err != nil {
		return Result{}, fmt.Errorf("canonicalize state authority: %w", err)
	}
	owner, err := stateauthority.New(statefileAuthority.CurrentKey(), input.Paths.ManifestPath)
	if err != nil {
		return Result{}, fmt.Errorf("construct state authority: %w", err)
	}
	if err := validateRegistryStateAuthority(input.Registry, owner, statefileAuthority); err != nil {
		return Result{}, err
	}

	byKey := make(map[observationKey]observe.OwnershipObservation)
	for _, desired := range input.ManagedPaths {
		if desired.Scope != target.ScopeGlobal || !managedPathSelected(desired.ConsumerTargets, input.Selection) {
			continue
		}
		if err := addObservation(byKey, input.Resolver, input.Registry, desired.Destination, ""); err != nil {
			return Result{}, err
		}
	}
	for index, address := range input.Aggregates {
		if err := address.Validate(); err != nil {
			return Result{}, fmt.Errorf("aggregate projection[%d]: %w", index, err)
		}
		document := address.Document()
		if document.Scope() != target.ScopeGlobal || !input.Selection.Includes(document.Target()) {
			continue
		}
		if err := addObservation(
			byKey,
			input.Resolver,
			input.Registry,
			document.AggregateRoot(),
			output.ContentPath(address.ContentPath()),
		); err != nil {
			return Result{}, err
		}
	}
	for _, state := range input.StatePaths {
		if state.Scope() != target.ScopeGlobal ||
			!managedPathSelected(state.ConsumerTargets(), input.Selection) {
			continue
		}
		if err := addObservation(
			byKey,
			input.Resolver,
			input.Registry,
			state.Destination(),
			"",
		); err != nil {
			return Result{}, err
		}
	}
	for _, state := range input.StateAggregates {
		contribution := state.Contribution()
		if contribution.Scope() != target.ScopeGlobal ||
			!input.Selection.Includes(contribution.Target()) {
			continue
		}
		if err := addObservation(
			byKey,
			input.Resolver,
			input.Registry,
			contribution.AggregateRoot(),
			output.ContentPath(contribution.ContentPath()),
		); err != nil {
			return Result{}, err
		}
	}

	keys := make([]observationKey, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left int, right int) bool {
		if keys[left].destination != keys[right].destination {
			return keys[left].destination.String() < keys[right].destination.String()
		}
		return keys[left].contentPath < keys[right].contentPath
	})
	observations := make([]observe.OwnershipObservation, 0, len(keys))
	for _, key := range keys {
		observations = append(observations, byKey[key])
	}
	return Result{Owner: owner, Observations: observations}, nil
}

func validateRegistryStateAuthority(
	registry outputownership.Registry,
	owner stateauthority.Authority,
	authority mutation.PersistedDirectoryEntryAuthority,
) error {
	return validateRegistryStateAuthorityWith(
		registry,
		owner,
		authority.ValidatePersistedKey,
		authority.RejectLegacyPersistedKey,
	)
}

func validateRegistryStateAuthorityWith(
	registry outputownership.Registry,
	owner stateauthority.Authority,
	validatePersisted func(string) error,
	rejectLegacy func(string) error,
) error {
	if validatePersisted == nil || rejectLegacy == nil {
		return fmt.Errorf("ownership-authority validators are required")
	}
	for index, claim := range registry.Claims() {
		if claim.Owner().ManifestPath() != owner.ManifestPath() {
			if err := rejectLegacy(claim.Owner().StatefileKey()); err != nil {
				return fmt.Errorf(
					"ownership registry claim[%d] has ambiguous legacy state authority: %w",
					index,
					err,
				)
			}
			continue
		}
		if err := validatePersisted(claim.Owner().StatefileKey()); err != nil {
			return fmt.Errorf(
				"ownership registry claim[%d] for selected manifest has incompatible state authority: %w",
				index,
				err,
			)
		}
	}
	return nil
}

func managedPathSelected(consumers []target.Target, selection targetselection.Selection) bool {
	return slices.ContainsFunc(consumers, selection.Includes)
}

type observationKey struct {
	destination output.Destination
	contentPath output.ContentPath
}

func addObservation(
	byKey map[observationKey]observe.OwnershipObservation,
	resolver DestinationResolver,
	registry outputownership.Registry,
	destination output.Destination,
	contentPath output.ContentPath,
) error {
	key := observationKey{destination: destination, contentPath: contentPath}
	if _, exists := byKey[key]; exists {
		return nil
	}
	resolved, err := resolver(destination)
	if err != nil {
		return fmt.Errorf("resolve ownership destination %q: %w", destination, err)
	}
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(resolved)
	if err != nil {
		return fmt.Errorf("canonicalize ownership destination %q: %w", destination, err)
	}
	address, err := outputownership.NewManagedAddress(authority.CurrentKey(), string(contentPath))
	if err != nil {
		return fmt.Errorf("construct ownership address for %q: %w", destination, err)
	}
	if err := rejectLegacyOwnershipAddress(registry, address, authority); err != nil {
		return fmt.Errorf("validate ownership address for %q: %w", destination, err)
	}
	claimValue := outputownership.NoClaim()
	if claim, present := registry.Conflict(address); present {
		claimValue, _ = outputownership.PresentClaim(claim)
	}
	byKey[key] = observe.OwnershipObservation{
		Destination: destination,
		ContentPath: contentPath,
		Address:     address,
		Claim:       claimValue,
	}
	return nil
}

func rejectLegacyOwnershipAddress(
	registry outputownership.Registry,
	current outputownership.ManagedAddress,
	authority mutation.PersistedDirectoryEntryAuthority,
) error {
	return rejectLegacyOwnershipAddressWith(
		registry,
		current,
		authority.RejectLegacyPersistedKey,
	)
}

func rejectLegacyOwnershipAddressWith(
	registry outputownership.Registry,
	current outputownership.ManagedAddress,
	rejectLegacy func(string) error,
) error {
	if rejectLegacy == nil {
		return fmt.Errorf("legacy ownership-address validator is required")
	}
	for _, claim := range registry.Claims() {
		rebased, err := outputownership.NewManagedAddress(
			current.Path(),
			claim.Address().ContentPath(),
		)
		if err != nil {
			return err
		}
		if !rebased.Overlaps(current) {
			continue
		}
		if err := rejectLegacy(claim.Address().Path()); err != nil {
			return err
		}
	}
	return nil
}
