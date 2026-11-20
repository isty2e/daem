package ownership

import (
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
)

var (
	ErrOwnerOutputsReserved = errors.New("the selected state owner has reserved output claims from an interrupted operation")
	ErrOwnerOutputsChanged  = errors.New("the selected output ownership records changed")
)

// RecordedOwner separates claim evidence from retained bindings or protection
// participation, which can exist without an applied State baseline.
type RecordedOwner struct {
	Authority stateauthority.Authority
	HasClaim  bool
}

// RecordedOwners includes protection participants even when they have no
// current binding row. Manifest provenance is retained for each recorded owner.
func (registry Registry) RecordedOwners() ([]RecordedOwner, error) {
	if !registry.initialized {
		return nil, fmt.Errorf("ownership registry is required")
	}
	indices := make(map[stateauthority.Authority]int)
	var owners []RecordedOwner
	add := func(owner stateauthority.Authority, hasClaim bool) {
		if index, exists := indices[owner]; exists {
			owners[index].HasClaim = owners[index].HasClaim || hasClaim
		} else {
			indices[owner] = len(owners)
			owners = append(owners, RecordedOwner{Authority: owner, HasClaim: hasClaim})
		}
	}
	for _, claim := range registry.claims {
		add(claim.Owner(), true)
	}
	for _, binding := range registry.bindings {
		add(binding.Owner(), false)
	}
	for _, protection := range registry.protections {
		add(protection.Owner(), false)
		for _, document := range protection.documents {
			for _, binding := range document.bindings {
				add(binding.Owner(), false)
			}
		}
	}
	return owners, nil
}

// OwnerOutputs is the complete recorded claim and binding set of one owner.
// It confers no authority over its output files.
type OwnerOutputs struct {
	owner    stateauthority.Key
	claims   []Claim
	bindings []DocumentBinding
}

func (outputs OwnerOutputs) Empty() bool { return len(outputs.claims)+len(outputs.bindings) == 0 }

func (outputs OwnerOutputs) Claims() []Claim { return slices.Clone(outputs.claims) }

func (outputs OwnerOutputs) Bindings() []DocumentBinding { return slices.Clone(outputs.bindings) }

func (outputs OwnerOutputs) Equal(other OwnerOutputs) bool {
	return outputs.owner == other.owner && slices.EqualFunc(outputs.claims, other.claims, Claim.Equal) &&
		slices.EqualFunc(outputs.bindings, other.bindings, DocumentBinding.Equal)
}

func (registry Registry) SettledOwnerOutputs(owner stateauthority.Key) (OwnerOutputs, error) {
	if !registry.initialized {
		return OwnerOutputs{}, fmt.Errorf("ownership registry is required")
	}
	if err := owner.Validate(); err != nil {
		return OwnerOutputs{}, err
	}
	outputs := OwnerOutputs{owner: owner}
	var reserved error
	for _, claim := range registry.claims {
		if claim.Owner().Key() == owner {
			if claim.State() == ClaimReserved {
				reserved = ErrOwnerOutputsReserved
			}
			outputs.claims = append(outputs.claims, claim)
		}
	}
	for _, binding := range registry.bindings {
		if binding.Owner().Key() == owner {
			outputs.bindings = append(outputs.bindings, binding)
		}
	}
	if err := registry.RequireOwnerSettled(owner); err != nil {
		return OwnerOutputs{}, errors.Join(err, reserved)
	}
	if len(registry.protections) > 0 && !outputs.Empty() {
		var paths []pathauthority.Exact
		for _, claim := range outputs.claims {
			paths = append(paths, claim.Address().PathAuthority())
		}
		for _, binding := range outputs.bindings {
			paths = append(paths, binding.PathAuthority())
		}
		scope, err := NewDocumentScope(paths, nil)
		if err != nil {
			return OwnerOutputs{}, err
		}
		if err := scope.RequireWritable(registry); err != nil {
			return OwnerOutputs{}, errors.Join(err, reserved)
		}
	}
	if reserved != nil {
		return OwnerOutputs{}, reserved
	}
	return outputs, nil
}

func (registry Registry) WithoutSettledOwnerOutputs(expected OwnerOutputs) (Registry, error) {
	current, err := registry.SettledOwnerOutputs(expected.owner)
	if err != nil {
		return Registry{}, err
	}
	if !current.Equal(expected) {
		return Registry{}, ErrOwnerOutputsChanged
	}
	claims := make([]Claim, 0, len(registry.claims)-len(current.claims))
	for _, claim := range registry.claims {
		if claim.Owner().Key() != current.owner {
			claims = append(claims, claim)
		}
	}
	bindings := make([]DocumentBinding, 0, len(registry.bindings)-len(current.bindings))
	for _, binding := range registry.bindings {
		if binding.Owner().Key() != current.owner {
			bindings = append(bindings, binding)
		}
	}
	return registry.withFacts(claims, bindings)
}
