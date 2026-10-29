package ownership

import (
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/executionidentity"
)

// NewRegistryWithProtections reconstructs all registry families. Construction
// grants neither publication authority nor permission to settle safety work.
func NewRegistryWithProtections(claims []Claim, bindings []DocumentBinding, protections []PendingProtection) (Registry, error) {
	registry, err := NewRegistry(claims, bindings...)
	if err != nil {
		return Registry{}, err
	}
	canonical := slices.Clone(protections)
	addresses := make([]ManagedAddress, 0)
	identities := make(map[executionidentity.Invocation]struct{}, len(canonical))
	for index, protection := range canonical {
		if err := protection.Validate(); err != nil {
			return Registry{}, fmt.Errorf("pending protection[%d]: %w", index, err)
		}
		if _, repeated := identities[protection.invocation]; repeated {
			return Registry{}, fmt.Errorf("pending protections repeat an invocation identity")
		}
		identities[protection.invocation] = struct{}{}
		for _, document := range protection.documents {
			address, _ := NewManagedAddressFromLegacy(document.PathAuthority(), "")
			addresses = append(addresses, address)
		}
	}
	if _, _, overlap := firstOverlappingAddress(addresses); overlap {
		return Registry{}, fmt.Errorf("pending protections overlap physical document scopes")
	}
	slices.SortFunc(canonical, func(left, right PendingProtection) int {
		if order := left.owner.Key().Compare(right.owner.Key()); order != 0 {
			return order
		}
		return strings.Compare(left.invocation.String(), right.invocation.String())
	})
	registry.protections = canonical
	return registry, nil
}

func (registry Registry) withFacts(claims []Claim, bindings []DocumentBinding) (Registry, error) {
	return NewRegistryWithProtections(claims, bindings, registry.protections)
}

func (registry Registry) PendingProtections() []PendingProtection {
	return slices.Clone(registry.protections)
}

func (registry Registry) Protection(invocation executionidentity.Invocation) (PendingProtection, bool) {
	for _, protection := range registry.protections {
		if protection.invocation == invocation {
			return protection, true
		}
	}
	return PendingProtection{}, false
}

func (registry Registry) ProtectionsForOwner(owner stateauthority.Key) []PendingProtection {
	var result []PendingProtection
	for _, protection := range registry.protections {
		if protection.owner.Key() == owner {
			result = append(result, protection)
		}
	}
	return result
}

// RequireWritable admits only document scopes without unresolved protection.
// The protection's own owner has no exemption for ordinary content/claim writes.
func (registry Registry) RequireWritable(physical pathauthority.Exact) error {
	if !registry.initialized {
		return fmt.Errorf("ownership registry is required")
	}
	if err := physical.Validate(); err != nil {
		return err
	}
	for _, protection := range registry.protections {
		for _, document := range protection.documents {
			if protectionPathsOverlap(document.PathAuthority(), physical) {
				return &ProtectionPendingError{Protection: protection, Path: physical}
			}
		}
	}
	return nil
}

// RequireOwnerSettled refuses a destructive owner transition even when its
// ordinary claims and document bindings have already become empty.
func (registry Registry) RequireOwnerSettled(owner stateauthority.Key) error {
	if !registry.initialized {
		return fmt.Errorf("ownership registry is required")
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	for _, protection := range registry.protections {
		if protection.owner.Key() == owner {
			return &ProtectionPendingError{Protection: protection, Path: protection.documents[0].PathAuthority()}
		}
		for _, document := range protection.documents {
			for _, binding := range document.bindings {
				if binding.Owner().Key() == owner {
					return &ProtectionPendingError{Protection: protection, Path: document.PathAuthority()}
				}
			}
		}
	}
	return nil
}

type ProtectionPendingError struct {
	Protection PendingProtection
	Path       pathauthority.Exact
}

func (err *ProtectionPendingError) Error() string {
	return fmt.Sprintf("document %q has pending permission protection for invocation %q; recover state authority %q before writing", err.Path.Key(), err.Protection.Invocation().String(), err.Protection.Owner().StatefileKey())
}

func (registry Registry) requireBindingWritable(values ...DocumentBindingValue) error {
	for _, value := range values {
		if binding, present := value.Get(); present {
			if err := registry.RequireWritable(binding.PathAuthority()); err != nil {
				return err
			}
		}
	}
	return nil
}
