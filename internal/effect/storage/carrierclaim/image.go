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
	references pathauthority.ReferenceTable
}

func NewRegistryImage(registry durablecarrier.GlobalCarrierClaims, references []pathauthority.Reference) (RegistryImage, error) {
	canonical, err := durablecarrier.NewGlobalCarrierClaims(registry.Claims())
	if err != nil {
		return RegistryImage{}, err
	}
	table, err := pathauthority.NewReferenceTable(registryPathIdentities(canonical), references)
	if err != nil {
		return RegistryImage{}, fmt.Errorf("carrier claim registry path_references: %w", err)
	}
	return RegistryImage{registry: canonical, references: table}, nil
}

func registryPathIdentities(registry durablecarrier.GlobalCarrierClaims) []pathauthority.Exact {
	var identities []pathauthority.Exact
	for _, claim := range registry.Claims() {
		identities = append(identities, claim.Owner().StatefileAuthority())
	}
	return identities
}

// RequiredReferenceIdentities exposes every required owner in stable identity
// order, without selecting locators or granting access to those keys.
func RequiredReferenceIdentities(registry durablecarrier.GlobalCarrierClaims) []pathauthority.Exact {
	identities := registryPathIdentities(registry)
	slices.SortFunc(identities, pathauthority.Exact.Compare)
	return slices.Compact(identities)
}

func (image RegistryImage) Validate() error {
	return image.references.Validate()
}

func (image RegistryImage) Registry() durablecarrier.GlobalCarrierClaims { return image.registry }

func (image RegistryImage) Reference(identity pathauthority.Exact) (pathauthority.Reference, bool) {
	return image.references.Reference(identity)
}

func (image RegistryImage) References() []pathauthority.Reference {
	return image.references.References()
}
