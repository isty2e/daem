package carrierclaim

import (
	"fmt"

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
	var required []pathauthority.Exact
	for _, claim := range canonical.Claims() {
		required = append(required, claim.Owner().StatefileAuthority())
	}
	table, err := pathauthority.NewReferenceTable(required, references)
	if err != nil {
		return RegistryImage{}, fmt.Errorf("carrier claim registry path_references: %w", err)
	}
	return RegistryImage{registry: canonical, references: table}, nil
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
