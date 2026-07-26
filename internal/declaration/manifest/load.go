package manifest

import (
	"os"

	"github.com/isty2e/daem/internal/declaration"
	declarationnormalize "github.com/isty2e/daem/internal/declaration/normalize"
	"github.com/isty2e/daem/internal/desired"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// LoadSelected decodes, normalizes, and validates the manifest selected by the
// command boundary.
func LoadSelected(paths daempaths.Paths) (desired.Environment, error) {
	environment, err := Load(paths.ManifestPath)
	if err != nil {
		return desired.Environment{}, err
	}
	if err := ValidatePlacement(paths, environment); err != nil {
		return desired.Environment{}, err
	}
	resolved, err := ResolveSelectedCarrierSources(paths, environment)
	if err != nil {
		return desired.Environment{}, err
	}
	return resolved, nil
}

// Load decodes and normalizes a manifest file.
func Load(path string) (desired.Environment, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return desired.Environment{}, err
	}

	return Decode(content)
}

// Decode decodes and normalizes manifest bytes without expanding resources.
func Decode(content []byte) (desired.Environment, error) {
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
