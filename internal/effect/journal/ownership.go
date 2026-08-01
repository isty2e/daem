package journal

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

// One valid recovery entry owns at most one exact transition or provisional
// intent, so this admits 100,000 entries and their complete ownership relation.
const maximumRecoveryOwnershipWorkItems = 200_000

type recoveryClaimTransition struct {
	Kind     string             `json:"kind"`
	Before   recoveryClaimValue `json:"before"`
	Prepared recoveryClaimValue `json:"prepared"`
	After    recoveryClaimValue `json:"after"`
}

type recoveryClaimValue struct {
	Present            bool              `json:"present"`
	PathAuthority      *pathAuthorityDTO `json:"path_authority,omitempty"`
	ContentPath        string            `json:"content_path,omitempty"`
	StatefileAuthority *pathAuthorityDTO `json:"statefile_authority,omitempty"`
	ManifestPath       string            `json:"manifest_path,omitempty"`
	State              string            `json:"state,omitempty"`
	OperationID        string            `json:"operation_id,omitempty"`
}

type pathAuthorityDTO struct {
	Key     string `json:"key"`
	Witness string `json:"semantics_witness"`
}

func recoveryClaimTransitions(transitions []ownershipmutation.ClaimTransition) ([]recoveryClaimTransition, error) {
	persisted := make([]recoveryClaimTransition, 0, len(transitions))
	for index, transition := range transitions {
		if err := transition.Validate(); err != nil {
			return nil, fmt.Errorf("claim transition[%d]: %w", index, err)
		}
		before, err := recoveryClaimValueFrom(transition.Before())
		if err != nil {
			return nil, fmt.Errorf("claim transition[%d] before: %w", index, err)
		}
		prepared, err := recoveryClaimValueFrom(transition.Prepared())
		if err != nil {
			return nil, fmt.Errorf("claim transition[%d] prepared: %w", index, err)
		}
		after, err := recoveryClaimValueFrom(transition.After())
		if err != nil {
			return nil, fmt.Errorf("claim transition[%d] after: %w", index, err)
		}
		persisted = append(persisted, recoveryClaimTransition{
			Kind: string(transition.Kind()), Before: before, Prepared: prepared, After: after,
		})
	}
	sort.Slice(persisted, func(left int, right int) bool {
		leftPath, leftContent := recoveryTransitionAddress(persisted[left])
		rightPath, rightContent := recoveryTransitionAddress(persisted[right])
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return leftContent < rightContent
	})
	return persisted, nil
}

func canonicalClaimTransitions(persisted []recoveryClaimTransition) ([]ownershipmutation.ClaimTransition, error) {
	transitions := make([]ownershipmutation.ClaimTransition, 0, len(persisted))
	addresses := make([]ownership.ManagedAddress, 0, len(persisted))
	for index, record := range persisted {
		before, err := canonicalClaimValue(record.Before)
		if err != nil {
			return nil, fmt.Errorf("recovery claim_transitions[%d].before: %w", index, err)
		}
		prepared, err := canonicalClaimValue(record.Prepared)
		if err != nil {
			return nil, fmt.Errorf("recovery claim_transitions[%d].prepared: %w", index, err)
		}
		after, err := canonicalClaimValue(record.After)
		if err != nil {
			return nil, fmt.Errorf("recovery claim_transitions[%d].after: %w", index, err)
		}
		transition, err := canonicalTransition(ownershipmutation.TransitionKind(record.Kind), before, prepared, after)
		if err != nil {
			return nil, fmt.Errorf("recovery claim_transitions[%d]: %w", index, err)
		}
		addresses = append(addresses, transition.Address())
		transitions = append(transitions, transition)
	}
	if err := validateNonOverlappingRecoveryAddresses(addresses); err != nil {
		return nil, err
	}
	return transitions, nil
}

func validateNonOverlappingRecoveryAddresses(addresses []ownership.ManagedAddress) error {
	type addressGroup struct {
		witness      string
		contentPaths []string
	}
	groups := make(map[string]addressGroup, len(addresses))
	for _, address := range addresses {
		path := address.Path()
		group, present := groups[path]
		if present && group.witness != address.PathAuthority().Witness() {
			return fmt.Errorf("recovery claim transitions contain overlapping addresses")
		}
		group.witness = address.PathAuthority().Witness()
		group.contentPaths = append(group.contentPaths, address.ContentPath())
		groups[path] = group
	}
	pathRoots := make(map[string]*recoveryPathPrefixSet)
	for path, group := range groups {
		volume, components := recoveryFilesystemPathComponents(path)
		root := pathRoots[volume]
		if root == nil {
			root = &recoveryPathPrefixSet{}
			pathRoots[volume] = root
		}
		if !root.insertDisjoint(components) {
			return fmt.Errorf("recovery claim transitions contain overlapping addresses")
		}
		if err := validateNonOverlappingRecoveryContentPaths(group.contentPaths); err != nil {
			return fmt.Errorf("recovery claim transitions contain overlapping addresses")
		}
	}
	return nil
}

func validateNonOverlappingRecoveryContentPaths(contentPaths []string) error {
	paths := recoveryPathPrefixSet{}
	for _, contentPath := range contentPaths {
		components := []string(nil)
		if contentPath != "" {
			components = strings.Split(strings.TrimPrefix(contentPath, "/"), "/")
		}
		if !paths.insertDisjoint(components) {
			return fmt.Errorf("overlapping recovery content path")
		}
	}
	return nil
}

type recoveryPathPrefixSet struct {
	terminal bool
	children map[string]*recoveryPathPrefixSet
}

func (set *recoveryPathPrefixSet) insertDisjoint(components []string) bool {
	current := set
	if current.terminal {
		return false
	}
	for _, component := range components {
		if current.terminal {
			return false
		}
		if current.children == nil {
			current.children = make(map[string]*recoveryPathPrefixSet)
		}
		next := current.children[component]
		if next == nil {
			next = &recoveryPathPrefixSet{}
			current.children[component] = next
		}
		current = next
	}
	if current.terminal || len(current.children) != 0 {
		return false
	}
	current.terminal = true
	return true
}

func recoveryFilesystemPathComponents(path string) (string, []string) {
	volume := filepath.VolumeName(path)
	relative := strings.TrimPrefix(path, volume)
	relative = strings.TrimPrefix(relative, string(filepath.Separator))
	if relative == "" {
		return volume, nil
	}
	return volume, strings.Split(relative, string(filepath.Separator))
}

func validateRecoveryClaimAuthorities(
	transitions []ownershipmutation.ClaimTransition,
	authority mutation.PersistedDirectoryEntryAuthority,
) error {
	for index, transition := range transitions {
		if !authority.Exact().Equal(transition.Owner().StatefileAuthority()) {
			return fmt.Errorf(
				"recovery claim_transitions[%d] has incompatible state authority %q with semantics %q",
				index,
				transition.Owner().StatefileKey(),
				transition.Owner().StatefileAuthority().Witness(),
			)
		}
	}
	return nil
}

func bindRecoveryClaimCoverage(
	ctx context.Context,
	entries []recoveryEntry,
	transitions []ownershipmutation.ClaimTransition,
	intents []ownership.ProvisionalAcquireIntent,
	resolver func(output.Destination) (string, error),
) ([]recoveryEntry, error) {
	if err := validateRecoveryOwnershipWorkBudget(len(entries), len(transitions), len(intents)); err != nil {
		return nil, err
	}
	bound := append([]recoveryEntry(nil), entries...)
	transitionsByAddress := make(map[string]struct{}, len(transitions))
	for index, transition := range transitions {
		if err := transition.Validate(); err != nil {
			return nil, fmt.Errorf("recovery claim transitions[%d]: %w", index, err)
		}
		key := ownershipAddressKey(transition.Address())
		if _, duplicate := transitionsByAddress[key]; duplicate {
			return nil, fmt.Errorf("recovery journal has duplicate exact ownership transition")
		}
		transitionsByAddress[key] = struct{}{}
	}
	intentKeys := make(map[string]struct{}, len(intents))
	for index, intent := range intents {
		if err := intent.Validate(); err != nil {
			return nil, fmt.Errorf("recovery provisional acquisition intents[%d]: %w", index, err)
		}
		key := provisionalAcquireIntentKey(intent.Destination(), intent.ContentPath())
		if _, duplicate := intentKeys[key]; duplicate {
			return nil, fmt.Errorf("recovery journal has duplicate provisional acquisition intent")
		}
		intentKeys[key] = struct{}{}
	}
	for index := range bound {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := bound[index]
		if entry.OwnershipPathAuthority != nil {
			return nil, fmt.Errorf("recovery entries[%d] capture already carries ownership path authority", index)
		}
		if !recoveryEntryRequiresOwnership(entry) {
			continue
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			return nil, fmt.Errorf("recovery entries[%d] destination: %w", index, err)
		}
		intentKey := provisionalAcquireIntentKey(destination, output.ContentPath(entry.ContentPath))
		if _, present := intentKeys[intentKey]; present {
			continue
		}
		if resolver == nil {
			return nil, fmt.Errorf("ownership coverage destination resolver is required")
		}
		physical, err := resolver(destination)
		if err != nil {
			return nil, fmt.Errorf("recovery entries[%d] resolve ownership path: %w", index, err)
		}
		authority, err := mutation.ObserveDirectoryEntryAuthority(physical)
		if err != nil {
			return nil, fmt.Errorf("recovery entries[%d] canonicalize ownership path: %w", index, err)
		}
		exact, present := authority.Exact()
		if !present {
			return nil, fmt.Errorf("recovery entries[%d] global output lacks exact ownership authority", index)
		}
		address, err := ownership.NewManagedAddress(exact, entry.ContentPath)
		if err != nil {
			return nil, fmt.Errorf("recovery entries[%d] ownership address: %w", index, err)
		}
		key := ownershipAddressKey(address)
		if _, present := transitionsByAddress[key]; !present {
			return nil, fmt.Errorf("recovery entries[%d] global output has no exact ownership transition", index)
		}
		bound[index].OwnershipPathAuthority = persistedPathAuthority(exact)
	}
	if err := validateRecoveryClaimCoverage(bound, transitions, intents); err != nil {
		return nil, err
	}
	return bound, nil
}

func validateRecoveryClaimCoverage(
	entries []recoveryEntry,
	transitions []ownershipmutation.ClaimTransition,
	intents []ownership.ProvisionalAcquireIntent,
) error {
	if err := validateRecoveryOwnershipWorkBudget(len(entries), len(transitions), len(intents)); err != nil {
		return err
	}
	remaining := make(map[string]ownershipmutation.ClaimTransition, len(transitions))
	for _, transition := range transitions {
		key := ownershipAddressKey(transition.Address())
		if _, duplicate := remaining[key]; duplicate {
			return fmt.Errorf("recovery journal has duplicate exact ownership transition")
		}
		remaining[key] = transition
	}
	remainingIntents := make(map[string]ownership.ProvisionalAcquireIntent, len(intents))
	for _, intent := range intents {
		key := provisionalAcquireIntentKey(intent.Destination(), intent.ContentPath())
		if _, duplicate := remainingIntents[key]; duplicate {
			return fmt.Errorf("recovery journal has duplicate provisional acquisition intent")
		}
		remainingIntents[key] = intent
	}
	for index, entry := range entries {
		if !recoveryEntryRequiresOwnership(entry) {
			if entry.OwnershipPathAuthority != nil {
				return fmt.Errorf("recovery entries[%d] must not carry ownership path authority", index)
			}
			continue
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			return fmt.Errorf("recovery entries[%d] destination: %w", index, err)
		}
		intentKey := provisionalAcquireIntentKey(destination, output.ContentPath(entry.ContentPath))
		if intent, present := remainingIntents[intentKey]; present {
			if entry.OwnershipPathAuthority != nil {
				return fmt.Errorf("recovery entries[%d] provisional acquisition must not carry exact ownership path authority", index)
			}
			matches, err := recoveryEntryAllowsTransition(entry, ownershipmutation.TransitionAcquire)
			if err != nil {
				return fmt.Errorf("recovery entries[%d]: %w", index, err)
			}
			if !matches {
				wantKind, _ := recoveryEntryTransitionKind(entry)
				return fmt.Errorf(
					"recovery entries[%d] requires %s ownership transition, got provisional acquire intent",
					index,
					wantKind,
				)
			}
			if intent.Destination() != destination || string(intent.ContentPath()) != entry.ContentPath {
				return fmt.Errorf("recovery entries[%d] provisional acquisition identity changed", index)
			}
			delete(remainingIntents, intentKey)
			continue
		}
		if entry.OwnershipPathAuthority == nil {
			return fmt.Errorf("recovery entries[%d] global output has no exact ownership transition authority", index)
		}
		exact, err := canonicalPathAuthority(*entry.OwnershipPathAuthority)
		if err != nil {
			return fmt.Errorf("recovery entries[%d] ownership_path_authority: %w", index, err)
		}
		address, err := ownership.NewManagedAddress(exact, entry.ContentPath)
		if err != nil {
			return fmt.Errorf("recovery entries[%d] ownership address: %w", index, err)
		}
		key := ownershipAddressKey(address)
		transition, present := remaining[key]
		if !present {
			return fmt.Errorf("recovery entries[%d] global output has no exact ownership transition", index)
		}
		matches, err := recoveryEntryAllowsTransition(entry, transition.Kind())
		if err != nil {
			return fmt.Errorf("recovery entries[%d]: %w", index, err)
		}
		if !matches {
			wantKind, _ := recoveryEntryTransitionKind(entry)
			return fmt.Errorf(
				"recovery entries[%d] requires %s ownership transition, got %s",
				index,
				wantKind,
				transition.Kind(),
			)
		}
		delete(remaining, key)
	}
	if len(remaining) != 0 {
		return fmt.Errorf("recovery journal has ownership transition without a global output entry")
	}
	if len(remainingIntents) != 0 {
		return fmt.Errorf("recovery journal has provisional acquisition intent without a global output entry")
	}
	return nil
}

func recoveryEntryRequiresOwnership(entry recoveryEntry) bool {
	return entry.Scope == string(target.ScopeGlobal) &&
		(!entry.StateIndependent || entry.Aggregate != nil)
}

func validateRecoveryOwnershipWorkBudget(entryCount int, transitionCount int, intentCount int) error {
	if entryCount < 0 || transitionCount < 0 || intentCount < 0 {
		return fmt.Errorf("recovery ownership work counts must be non-negative")
	}
	if entryCount > maximumRecoveryOwnershipWorkItems ||
		transitionCount > maximumRecoveryOwnershipWorkItems-entryCount ||
		intentCount > maximumRecoveryOwnershipWorkItems-entryCount-transitionCount {
		return fmt.Errorf(
			"recovery ownership relationships exceed %d work items",
			maximumRecoveryOwnershipWorkItems,
		)
	}
	return nil
}

func recoveryEntryAllowsTransition(
	entry recoveryEntry,
	kind ownershipmutation.TransitionKind,
) (bool, error) {
	want, err := recoveryEntryTransitionKind(entry)
	if err != nil {
		return false, err
	}
	if kind == want {
		return true, nil
	}
	return entry.Aggregate != nil &&
		entry.Before.Existed &&
		entry.ExpectedAfter.Existed &&
		kind == ownershipmutation.TransitionAcquire, nil
}

func recoveryEntryTransitionKind(entry recoveryEntry) (ownershipmutation.TransitionKind, error) {
	if entry.Aggregate != nil {
		switch {
		case !entry.Before.Existed && entry.ExpectedAfter.Existed:
			return ownershipmutation.TransitionAcquire, nil
		case entry.Before.Existed && !entry.ExpectedAfter.Existed:
			return ownershipmutation.TransitionRelease, nil
		case entry.Before.Existed && entry.ExpectedAfter.Existed:
			return ownershipmutation.TransitionRetain, nil
		default:
			return "", fmt.Errorf("global aggregate projection must change or retain selected state")
		}
	}
	switch {
	case !entry.StateBefore.Managed && entry.StateExpectedAfter.Managed:
		return ownershipmutation.TransitionAcquire, nil
	case entry.StateBefore.Managed && !entry.StateExpectedAfter.Managed:
		return ownershipmutation.TransitionRelease, nil
	case entry.StateBefore.Managed && entry.StateExpectedAfter.Managed:
		return ownershipmutation.TransitionRetain, nil
	default:
		return "", fmt.Errorf("global output must change or retain managed state")
	}
}

func ownershipAddressKey(address ownership.ManagedAddress) string {
	return address.Path() + "\x00" + address.PathAuthority().Witness() + "\x00" + address.ContentPath()
}

func canonicalTransition(
	kind ownershipmutation.TransitionKind,
	before ownership.ClaimValue,
	prepared ownership.ClaimValue,
	after ownership.ClaimValue,
) (ownershipmutation.ClaimTransition, error) {
	return ownershipmutation.NewTransition(kind, before, prepared, after)
}

func recoveryClaimValueFrom(value ownership.ClaimValue) (recoveryClaimValue, error) {
	claim, present := value.Get()
	if !present {
		return recoveryClaimValue{}, nil
	}
	if err := claim.Validate(); err != nil {
		return recoveryClaimValue{}, err
	}
	path := claim.Address().PathAuthority()
	statefile := claim.Owner().StatefileAuthority()
	return recoveryClaimValue{
		Present:            true,
		PathAuthority:      persistedPathAuthority(path),
		ContentPath:        claim.Address().ContentPath(),
		StatefileAuthority: persistedPathAuthority(statefile),
		ManifestPath:       claim.Owner().ManifestPath(),
		State:              string(claim.State()),
		OperationID:        claim.OperationID(),
	}, nil
}

func canonicalClaimValue(record recoveryClaimValue) (ownership.ClaimValue, error) {
	if !record.Present {
		if record.PathAuthority != nil || record.ContentPath != "" ||
			record.StatefileAuthority != nil ||
			record.ManifestPath != "" || record.State != "" || record.OperationID != "" {
			return ownership.ClaimValue{}, fmt.Errorf("absent claim must not retain claim fields")
		}
		return ownership.NoClaim(), nil
	}
	if record.PathAuthority == nil || record.StatefileAuthority == nil {
		return ownership.ClaimValue{}, fmt.Errorf(
			"present claim requires path_authority and statefile_authority",
		)
	}
	path, err := canonicalPathAuthority(*record.PathAuthority)
	if err != nil {
		return ownership.ClaimValue{}, fmt.Errorf("path authority: %w", err)
	}
	address, err := ownership.NewManagedAddress(path, record.ContentPath)
	if err != nil {
		return ownership.ClaimValue{}, err
	}
	statefile, err := canonicalPathAuthority(*record.StatefileAuthority)
	if err != nil {
		return ownership.ClaimValue{}, fmt.Errorf("statefile authority: %w", err)
	}
	owner, err := stateauthority.New(statefile, record.ManifestPath)
	if err != nil {
		return ownership.ClaimValue{}, err
	}
	var claim ownership.Claim
	switch ownership.ClaimState(record.State) {
	case ownership.ClaimReserved:
		claim, err = ownership.NewReservedClaim(address, owner, record.OperationID)
	case ownership.ClaimActive:
		if record.OperationID != "" {
			return ownership.ClaimValue{}, fmt.Errorf("active claim must not retain operation_id")
		}
		claim, err = ownership.NewActiveClaim(address, owner)
	default:
		return ownership.ClaimValue{}, fmt.Errorf("unsupported claim state %q", record.State)
	}
	if err != nil {
		return ownership.ClaimValue{}, err
	}
	return ownership.PresentClaim(claim)
}

func persistedPathAuthority(authority pathauthority.Exact) *pathAuthorityDTO {
	return &pathAuthorityDTO{Key: authority.Key(), Witness: authority.Witness()}
}

func canonicalPathAuthority(record pathAuthorityDTO) (pathauthority.Exact, error) {
	return pathauthority.NewExact(record.Key, record.Witness)
}

func recoveryTransitionAddress(transition recoveryClaimTransition) (string, string) {
	for _, value := range []recoveryClaimValue{transition.Prepared, transition.Before, transition.After} {
		if value.Present {
			if value.PathAuthority == nil {
				return "", value.ContentPath
			}
			return value.PathAuthority.Key + "\x00" + value.PathAuthority.Witness, value.ContentPath
		}
	}
	return "", ""
}
