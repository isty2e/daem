package ownership

import (
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
)

// ErrOwnReleaseChanged identifies own metadata outside the recorded release.
var ErrOwnReleaseChanged = errors.New("own document management changed during release")

// OwnDocumentRelease removes only recorded own claims and an optional own binding.
// An already released address may have a foreign owner; it is never reacquired.
// Pending protection remains a barrier even when every target is already absent.
type OwnDocumentRelease struct {
	owner         stateauthority.Authority
	scope         DocumentScope
	before        []Claim
	removed       []Claim
	destination   output.Destination
	beforeBinding DocumentBindingValue
	afterBinding  DocumentBindingValue
	initialized   bool
}

type OwnDocumentReleaseInput struct {
	Owner              stateauthority.Authority
	Scope              DocumentScope
	BeforeClaims       []Claim
	RemovalAddresses   []ManagedAddress
	BindingDestination output.Destination
	BindingBefore      DocumentBindingValue
	RetainBinding      bool
}

func NewOwnDocumentRelease(input OwnDocumentReleaseInput) (OwnDocumentRelease, error) {
	owner, scope := input.Owner, input.Scope
	if err := owner.Validate(); err != nil {
		return OwnDocumentRelease{}, err
	}
	if err := scope.RequireWritable(EmptyRegistry()); err != nil {
		return OwnDocumentRelease{}, err
	}
	if len(scope.exact)+len(scope.missing) != 1 {
		return OwnDocumentRelease{}, fmt.Errorf("document release requires one qualified document namespace")
	}
	if err := input.BindingDestination.Validate(); err != nil {
		return OwnDocumentRelease{}, err
	}
	prior, err := NewRegistry(input.BeforeClaims)
	if err != nil {
		return OwnDocumentRelease{}, err
	}
	byAddress := make(map[ManagedAddress]Claim, len(prior.claims))
	for _, claim := range prior.claims {
		if !claim.Owner().Equal(owner) || claim.State() != ClaimActive || !scope.matchesRetainedDocument(claim.Address().PathAuthority()) {
			return OwnDocumentRelease{}, fmt.Errorf("document release before must contain only scoped own active claims")
		}
		byAddress[claim.Address()] = claim
	}
	seen := make(map[ManagedAddress]struct{}, len(input.RemovalAddresses))
	var removals []Claim
	for _, address := range input.RemovalAddresses {
		if _, repeated := seen[address]; repeated {
			return OwnDocumentRelease{}, fmt.Errorf("document release repeats a removed claim")
		}
		seen[address] = struct{}{}
		claim, present := byAddress[address]
		if !present {
			return OwnDocumentRelease{}, fmt.Errorf("document release removes a claim outside its own before set")
		}
		removals = append(removals, claim)
	}
	if binding, present := input.BindingBefore.Get(); present {
		if err := binding.Validate(); err != nil {
			return OwnDocumentRelease{}, err
		}
		if !binding.Owner().Equal(owner) || binding.Destination() != input.BindingDestination {
			return OwnDocumentRelease{}, fmt.Errorf("document release binding has a different identity key")
		}
		if !scope.matchesRetainedDocument(binding.PathAuthority()) {
			return OwnDocumentRelease{}, fmt.Errorf("document release binding is outside its recorded scope")
		}
	} else if input.RetainBinding {
		return OwnDocumentRelease{}, fmt.Errorf("document release cannot retain a binding absent from before")
	}
	afterBinding := NoDocumentBinding()
	if input.RetainBinding {
		afterBinding = input.BindingBefore
	}
	sort.Slice(removals, func(left, right int) bool { return removals[left].Address().Less(removals[right].Address()) })
	return OwnDocumentRelease{
		owner: owner, scope: scope, before: prior.Claims(), removed: removals,
		destination: input.BindingDestination, beforeBinding: input.BindingBefore, afterBinding: afterBinding, initialized: true,
	}, nil
}

func (release OwnDocumentRelease) Validate() error {
	if !release.initialized {
		return fmt.Errorf("own document release is required")
	}
	return nil
}

func (release OwnDocumentRelease) Owner() stateauthority.Authority { return release.owner }
func (release OwnDocumentRelease) BeforeClaims() []Claim           { return slices.Clone(release.before) }

func (release OwnDocumentRelease) RemovedClaims() []Claim { return slices.Clone(release.removed) }

func (release OwnDocumentRelease) BindingDestination() output.Destination { return release.destination }

func (release OwnDocumentRelease) BindingBefore() DocumentBindingValue { return release.beforeBinding }

func (release OwnDocumentRelease) BindingAfter() DocumentBindingValue { return release.afterBinding }

func (release OwnDocumentRelease) currentBinding(registry Registry) DocumentBindingValue {
	if binding, present := registry.DocumentBinding(release.owner.Key(), release.destination); present {
		value, _ := PresentDocumentBinding(binding)
		return value
	}
	return NoDocumentBinding()
}

// RequireBefore verifies the precommit phase without repairing missing claims.
func (release OwnDocumentRelease) RequireBefore(registry Registry) error {
	if err := release.Validate(); err != nil {
		return err
	}
	if err := release.scope.RequireWritable(registry); err != nil {
		return err
	}
	expected := make(map[ManagedAddress]Claim, len(release.before))
	for _, claim := range release.before {
		expected[claim.Address()] = claim
	}
	for _, claim := range registry.claims {
		if !claim.Owner().Equal(release.owner) || !release.scope.matchesRetainedDocument(claim.Address().PathAuthority()) {
			continue
		}
		before, present := expected[claim.Address()]
		if !present || !before.Equal(claim) {
			return ErrOwnReleaseChanged
		}
		delete(expected, claim.Address())
	}
	if len(expected) != 0 {
		return ErrOwnReleaseChanged
	}
	if !release.currentBinding(registry).Equal(release.beforeBinding) {
		return ErrOwnReleaseChanged
	}
	return nil
}

// ApplyAfterCommit converges scoped own metadata after the containing local
// file-set commit. It does not establish that commit or grant storage access.
func (release OwnDocumentRelease) ApplyAfterCommit(registry Registry) (Registry, bool, error) {
	if err := release.Validate(); err != nil {
		return Registry{}, false, err
	}
	if err := release.scope.RequireWritable(registry); err != nil {
		return Registry{}, false, err
	}
	expected := make(map[ManagedAddress]Claim, len(release.before))
	for _, claim := range release.before {
		expected[claim.Address()] = claim
	}
	removed := make(map[ManagedAddress]struct{}, len(release.removed))
	for _, claim := range release.removed {
		removed[claim.Address()] = struct{}{}
	}
	claims := make([]Claim, 0, len(registry.claims))
	changed := false
	for _, claim := range registry.claims {
		if !claim.Owner().Equal(release.owner) || !release.scope.matchesRetainedDocument(claim.Address().PathAuthority()) {
			claims = append(claims, claim)
			continue
		}
		prior, present := expected[claim.Address()]
		if !present || !prior.Equal(claim) {
			return Registry{}, false, ErrOwnReleaseChanged
		}
		delete(expected, claim.Address())
		if _, remove := removed[claim.Address()]; remove {
			changed = true
		} else {
			claims = append(claims, claim)
		}
	}
	for address := range expected {
		if _, removable := removed[address]; !removable {
			return Registry{}, false, ErrOwnReleaseChanged
		}
	}
	next, err := registry.withFacts(claims, registry.bindings)
	if err != nil {
		return Registry{}, false, err
	}
	actual := release.currentBinding(next)
	if actual.Equal(release.afterBinding) {
		return next, changed, nil
	}
	if !actual.Equal(release.beforeBinding) {
		return Registry{}, false, ErrOwnReleaseChanged
	}
	next, err = next.ApplyDocumentBinding(release.owner.Key(), release.destination, release.beforeBinding, release.afterBinding)
	if err != nil {
		return Registry{}, false, err
	}
	return next, true, nil
}
