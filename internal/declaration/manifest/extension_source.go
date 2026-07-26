package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/desired"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// ResolveSelectedCarrierSources materializes context-dependent local carrier
// sources against the selected manifest root. It performs no source I/O and
// leaves non-local host source spellings unchanged.
func ResolveSelectedCarrierSources(
	paths daempaths.Paths,
	environment desired.Environment,
) (desired.Environment, error) {
	extensions := environment.Extensions()
	var (
		localContext extensiontopology.LocalSourceContext
		hasContext   bool
	)
	for index, value := range extensions {
		source, err := extensiontopology.InterpretCarrierSource(value.CarrierKey())
		if err != nil {
			return desired.Environment{}, fmt.Errorf(
				"resolve extension[%d] carrier source: %w",
				index,
				err,
			)
		}
		if source.Class() != extensiontopology.CarrierSourceLocal {
			continue
		}
		if !hasContext {
			homeRoot, err := os.UserHomeDir()
			if err != nil {
				return desired.Environment{}, fmt.Errorf(
					"resolve carrier local source home: %w",
					err,
				)
			}
			localContext, err = extensiontopology.NewLocalSourceContext(
				paths.ManifestRoot,
				homeRoot,
			)
			if err != nil {
				return desired.Environment{}, err
			}
			hasContext = true
		}
		identity, err := source.ResolveLocal(localContext)
		if err != nil {
			return desired.Environment{}, fmt.Errorf(
				"resolve extension[%d] local carrier source: %w",
				index,
				err,
			)
		}
		resolvedRef, err := selectedCarrierSourceRef(
			paths.ManifestRoot,
			value.Scope(),
			identity.Path(),
		)
		if err != nil {
			return desired.Environment{}, fmt.Errorf(
				"resolve extension[%d] carrier source identity: %w",
				index,
				err,
			)
		}
		resolvedSource, err := desiredextension.NewSourceRef(value.Source().Kind(), resolvedRef)
		if err != nil {
			return desired.Environment{}, fmt.Errorf(
				"resolve extension[%d] carrier source reference: %w",
				index,
				err,
			)
		}
		extensions[index], err = value.WithSource(resolvedSource)
		if err != nil {
			return desired.Environment{}, fmt.Errorf(
				"resolve extension[%d]: %w",
				index,
				err,
			)
		}
	}
	resolved, err := environment.WithExtensions(extensions)
	if err != nil {
		return desired.Environment{}, fmt.Errorf(
			"resolve selected carrier sources: %w",
			err,
		)
	}
	return resolved, nil
}

func selectedCarrierSourceRef(
	manifestRoot string,
	scope target.Scope,
	absoluteIdentity string,
) (string, error) {
	if scope != target.ScopeProject {
		return absoluteIdentity, nil
	}
	relative, err := filepath.Rel(manifestRoot, absoluteIdentity)
	if err != nil {
		return "", fmt.Errorf("derive project-local carrier source: %w", err)
	}
	relative = filepath.Clean(relative)
	if strings.HasPrefix(relative, "-") {
		relative = "." + string(filepath.Separator) + relative
	}
	return relative, nil
}
