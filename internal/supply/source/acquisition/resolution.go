package acquisition

import (
	"fmt"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/supply/source"
)

// Resolution pairs exact source/artifact facts with non-owning materialized
// access. It does not claim the view remains equal to the identity.
type Resolution struct {
	identity artifact.ExactIdentity
	view     access.View
}

// NewResolution validates source correlation and structural pairing.
func NewResolution(
	sourceSpec source.Source,
	identity artifact.ExactIdentity,
	view access.View,
) (Resolution, error) {
	if err := identity.Validate(); err != nil {
		return Resolution{}, fmt.Errorf("source acquisition identity: %w", err)
	}
	if err := source.ValidateResolutionCorrelation(
		sourceSpec,
		identity.SourceID(),
		identity.ResolvedRef(),
	); err != nil {
		return Resolution{}, err
	}
	if view.Kind() != identity.Kind() {
		return Resolution{}, fmt.Errorf(
			"source acquisition view kind %q does not match identity kind %q",
			view.Kind(),
			identity.Kind(),
		)
	}
	return Resolution{identity: identity, view: view}, nil
}

// Identity returns the canonical exact artifact identity.
func (resolution Resolution) Identity() artifact.ExactIdentity { return resolution.identity }

// View returns copyable non-owning access to the current materialized root.
func (resolution Resolution) View() access.View { return resolution.view }

// Validate rejects a zero or internally inconsistent resolution.
func (resolution Resolution) Validate(sourceSpec source.Source) error {
	_, err := NewResolution(sourceSpec, resolution.identity, resolution.view)
	return err
}
