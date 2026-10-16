package manifest

import (
	"context"

	"github.com/isty2e/daem/internal/declaration"
	declarationnormalize "github.com/isty2e/daem/internal/declaration/normalize"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/desired"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// LoadSelected validates the selected manifest and resolves contextual carrier
// sources. InputErrorCause distinguishes input failures from resolution failures.
func LoadSelected(ctx context.Context, paths daempaths.Paths) (desired.Environment, error) {
	environment, err := Load(ctx, paths.ManifestPath)
	if err != nil {
		return desired.Environment{}, selectedInputError{cause: err}
	}
	if err := ValidatePlacement(paths, environment); err != nil {
		return desired.Environment{}, selectedInputError{cause: err}
	}
	resolved, err := ResolveSelectedCarrierSources(paths, environment)
	if err != nil {
		return desired.Environment{}, err
	}
	return resolved, nil
}

// Load decodes and normalizes a manifest file.
func Load(ctx context.Context, path string) (desired.Environment, error) {
	content, err := declarationartifact.Read(ctx, path)
	if err != nil {
		return desired.Environment{}, err
	}

	return Decode(content)
}

// Decode decodes and normalizes manifest bytes without expanding resources.
func Decode(content []byte) (desired.Environment, error) {
	if err := declarationartifact.Admit(content); err != nil {
		return desired.Environment{}, err
	}
	raw, err := declaration.DecodeManifest(content)
	if err != nil {
		return desired.Environment{}, err
	}

	normalized, err := declarationnormalize.Manifest(raw)
	if err != nil {
		return desired.Environment{}, err
	}

	return normalized, nil
}
