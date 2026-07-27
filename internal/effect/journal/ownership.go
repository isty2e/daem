package journal

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

type recoveryClaimTransition struct {
	Kind     string             `json:"kind"`
	Before   recoveryClaimValue `json:"before"`
	Prepared recoveryClaimValue `json:"prepared"`
	After    recoveryClaimValue `json:"after"`
}

type recoveryClaimValue struct {
	Present      bool   `json:"present"`
	Path         string `json:"path,omitempty"`
	ContentPath  string `json:"content_path,omitempty"`
	StatefileKey string `json:"statefile_key,omitempty"`
	ManifestPath string `json:"manifest_path,omitempty"`
	State        string `json:"state,omitempty"`
	OperationID  string `json:"operation_id,omitempty"`
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
		for _, address := range addresses {
			if address.Overlaps(transition.Address()) {
				return nil, fmt.Errorf("recovery claim_transitions[%d]: overlapping address", index)
			}
		}
		addresses = append(addresses, transition.Address())
		transitions = append(transitions, transition)
	}
	return transitions, nil
}

func validateRecoveryClaimCoverage(
	entries []recoveryEntry,
	transitions []ownershipmutation.ClaimTransition,
	resolver func(output.Destination) (string, error),
) error {
	requiresResolver := len(transitions) != 0
	for _, entry := range entries {
		if !entry.StateIndependent &&
			entry.Scope == string(target.ScopeGlobal) {
			requiresResolver = true
			break
		}
	}
	if requiresResolver && resolver == nil {
		return fmt.Errorf("ownership coverage destination resolver is required")
	}
	remaining := make(map[string]ownershipmutation.ClaimTransition, len(transitions))
	for _, transition := range transitions {
		remaining[ownershipAddressKey(transition.Address())] = transition
	}
	for index, entry := range entries {
		if entry.StateIndependent && entry.Aggregate == nil {
			continue
		}
		if entry.Scope != string(target.ScopeGlobal) {
			continue
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			return fmt.Errorf("recovery entries[%d] destination: %w", index, err)
		}
		physical, err := resolver(destination)
		if err != nil {
			return fmt.Errorf("recovery entries[%d] resolve ownership path: %w", index, err)
		}
		canonical, err := mutation.CanonicalDirectoryEntryKey(physical)
		if err != nil {
			return fmt.Errorf("recovery entries[%d] canonicalize ownership path: %w", index, err)
		}
		address, err := ownership.NewManagedAddress(canonical, entry.ContentPath)
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
	return address.Path() + "\x00" + address.ContentPath()
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
	return recoveryClaimValue{
		Present: true, Path: claim.Address().Path(), ContentPath: claim.Address().ContentPath(),
		StatefileKey: claim.Owner().StatefileKey(), ManifestPath: claim.Owner().ManifestPath(),
		State: string(claim.State()), OperationID: claim.OperationID(),
	}, nil
}

func canonicalClaimValue(record recoveryClaimValue) (ownership.ClaimValue, error) {
	if !record.Present {
		if record.Path != "" || record.ContentPath != "" || record.StatefileKey != "" || record.ManifestPath != "" || record.State != "" || record.OperationID != "" {
			return ownership.ClaimValue{}, fmt.Errorf("absent claim must not retain claim fields")
		}
		return ownership.NoClaim(), nil
	}
	address, err := ownership.NewManagedAddress(record.Path, record.ContentPath)
	if err != nil {
		return ownership.ClaimValue{}, err
	}
	owner, err := ownership.NewOwnerAuthority(record.StatefileKey, record.ManifestPath)
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

func recoveryTransitionAddress(transition recoveryClaimTransition) (string, string) {
	for _, value := range []recoveryClaimValue{transition.Prepared, transition.Before, transition.After} {
		if value.Present {
			return value.Path, value.ContentPath
		}
	}
	return "", ""
}
