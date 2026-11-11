package carrierclaim

import (
	"fmt"
	"slices"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

// RegistryImage preserves every carrier owner's access reference without
// changing owner identity or granting live filesystem authority.
type RegistryImage struct {
	registry   durablecarrier.GlobalCarrierClaims
	references map[pathauthority.Exact]pathauthority.Reference
}

func NewRegistryImage(registry durablecarrier.GlobalCarrierClaims, references []pathauthority.Reference) (RegistryImage, error) {
	canonical, err := durablecarrier.NewGlobalCarrierClaims(registry.Claims())
	if err != nil {
		return RegistryImage{}, err
	}
	required := make(map[pathauthority.Exact]struct{})
	for _, claim := range canonical.Claims() {
		required[claim.Owner().StatefileAuthority()] = struct{}{}
	}
	values := make(map[pathauthority.Exact]pathauthority.Reference, len(references))
	for _, reference := range references {
		if err := reference.Validate(); err != nil {
			return RegistryImage{}, err
		}
		identity := reference.Identity()
		if _, present := required[identity]; !present {
			return RegistryImage{}, fmt.Errorf("carrier reference %q has no owner", identity.Key())
		}
		if previous, present := values[identity]; present && !previous.Equal(reference) {
			return RegistryImage{}, fmt.Errorf("carrier owner %q has conflicting locators", identity.Key())
		}
		values[identity] = reference
	}
	for identity := range required {
		if _, present := values[identity]; !present {
			return RegistryImage{}, fmt.Errorf("carrier owner %q requires an access reference", identity.Key())
		}
	}
	return RegistryImage{registry: canonical, references: values}, nil
}

func (image RegistryImage) Validate() error {
	if image.references == nil {
		return fmt.Errorf("carrier registry image is uninitialized")
	}
	return nil
}

func (image RegistryImage) Registry() durablecarrier.GlobalCarrierClaims { return image.registry }

func (image RegistryImage) Reference(identity pathauthority.Exact) (pathauthority.Reference, bool) {
	reference, present := image.references[identity]
	return reference, present
}

func (image RegistryImage) References() []pathauthority.Reference {
	values := make([]pathauthority.Reference, 0, len(image.references))
	for _, reference := range image.references {
		values = append(values, reference)
	}
	slices.SortFunc(values, func(left, right pathauthority.Reference) int { return left.Identity().Compare(right.Identity()) })
	return values
}
