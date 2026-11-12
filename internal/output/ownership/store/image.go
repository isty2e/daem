package store

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output/ownership"
)

// RegistryImage binds registry facts to complete recorded access evidence.
// It does not establish filesystem correspondence or authorize publication.
type RegistryImage struct {
	registry   ownership.Registry
	references pathauthority.ReferenceTable
}

func NewRegistryImage(registry ownership.Registry, references []pathauthority.Reference) (RegistryImage, error) {
	canonical, err := ownership.NewRegistryWithProtections(registry.Claims(), registry.DocumentBindings(), registry.PendingProtections())
	if err != nil {
		return RegistryImage{}, err
	}
	table, err := pathauthority.NewReferenceTable(registryPathIdentities(canonical), references)
	if err != nil {
		return RegistryImage{}, fmt.Errorf("ownership registry path_references: %w", err)
	}
	return RegistryImage{registry: canonical, references: table}, nil
}

func registryPathIdentities(registry ownership.Registry) []pathauthority.Exact {
	var identities []pathauthority.Exact
	for _, claim := range registry.Claims() {
		identities = append(identities, claim.Address().PathAuthority(), claim.Owner().StatefileAuthority())
	}
	addBinding := func(binding ownership.DocumentBinding) {
		identities = append(identities, binding.PathAuthority(), binding.Owner().StatefileAuthority())
	}
	for _, binding := range registry.DocumentBindings() {
		addBinding(binding)
	}
	for _, protection := range registry.PendingProtections() {
		identities = append(identities, protection.Owner().StatefileAuthority())
		for _, document := range protection.Documents() {
			for _, binding := range document.Bindings() {
				addBinding(binding)
			}
		}
	}
	return identities
}

// RequiredReferenceIdentities exposes the image's complete membership in stable
// identity order, without selecting locators or granting access to those keys.
func RequiredReferenceIdentities(registry ownership.Registry) []pathauthority.Exact {
	identities := registryPathIdentities(registry)
	slices.SortFunc(identities, pathauthority.Exact.Compare)
	return slices.Compact(identities)
}

func (image RegistryImage) Registry() ownership.Registry { return image.registry }

func (image RegistryImage) Validate() error {
	return image.references.Validate()
}

func (image RegistryImage) Reference(identity pathauthority.Exact) (pathauthority.Reference, bool) {
	return image.references.Reference(identity)
}

func (image RegistryImage) References() []pathauthority.Reference {
	return image.references.References()
}
