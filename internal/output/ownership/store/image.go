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
	references map[pathauthority.Exact]pathauthority.Reference
}

func NewRegistryImage(registry ownership.Registry, references []pathauthority.Reference) (RegistryImage, error) {
	canonical, err := ownership.NewRegistryWithProtections(registry.Claims(), registry.DocumentBindings(), registry.PendingProtections())
	if err != nil {
		return RegistryImage{}, err
	}
	required := registryPathIdentities(canonical)
	byIdentity := make(map[pathauthority.Exact]pathauthority.Reference, len(references))
	for _, reference := range references {
		if err := reference.Validate(); err != nil {
			return RegistryImage{}, err
		}
		identity := reference.Identity()
		if _, present := required[identity]; !present {
			return RegistryImage{}, fmt.Errorf("path reference %q is not used by this registry", identity.Key())
		}
		if previous, present := byIdentity[identity]; present && !previous.Equal(reference) {
			return RegistryImage{}, fmt.Errorf("registry path %q has conflicting recorded locators", identity.Key())
		}
		byIdentity[identity] = reference
	}
	for identity := range required {
		if _, present := byIdentity[identity]; !present {
			return RegistryImage{}, fmt.Errorf("registry path %q requires a recorded locator", identity.Key())
		}
	}
	return RegistryImage{registry: canonical, references: byIdentity}, nil
}

func registryPathIdentities(registry ownership.Registry) map[pathauthority.Exact]struct{} {
	identities := make(map[pathauthority.Exact]struct{})
	for _, claim := range registry.Claims() {
		identities[claim.Address().PathAuthority()] = struct{}{}
		identities[claim.Owner().StatefileAuthority()] = struct{}{}
	}
	addBinding := func(binding ownership.DocumentBinding) {
		identities[binding.PathAuthority()] = struct{}{}
		identities[binding.Owner().StatefileAuthority()] = struct{}{}
	}
	for _, binding := range registry.DocumentBindings() {
		addBinding(binding)
	}
	for _, protection := range registry.PendingProtections() {
		identities[protection.Owner().StatefileAuthority()] = struct{}{}
		for _, document := range protection.Documents() {
			for _, binding := range document.Bindings() {
				addBinding(binding)
			}
		}
	}
	return identities
}

func (image RegistryImage) Registry() ownership.Registry { return image.registry }

func (image RegistryImage) Validate() error {
	if image.references == nil {
		return fmt.Errorf("registry image is required")
	}
	return nil
}

func (image RegistryImage) Reference(identity pathauthority.Exact) (pathauthority.Reference, bool) {
	reference, present := image.references[identity]
	return reference, present
}

func (image RegistryImage) References() []pathauthority.Reference {
	references := make([]pathauthority.Reference, 0, len(image.references))
	for _, reference := range image.references {
		references = append(references, reference)
	}
	slices.SortFunc(references, func(left, right pathauthority.Reference) int {
		return left.Identity().Compare(right.Identity())
	})
	return references
}
