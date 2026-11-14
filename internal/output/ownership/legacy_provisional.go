package ownership

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// NewProvisionalAcquireIntentFromLegacy treats old segments literally, including
// tilde spellings. An empty legacy scope remains whole-path authority.
func NewProvisionalAcquireIntentFromLegacy(destination output.Destination, contentPath output.ContentPath, path pathauthority.Provisional, owner stateauthority.Authority, operationID string) (ProvisionalAcquireIntent, error) {
	if err := destination.ValidateScope(target.ScopeGlobal); err != nil {
		return ProvisionalAcquireIntent{}, fmt.Errorf("provisional acquisition destination: %w", err)
	}
	footprint, err := output.FootprintFromLegacy(contentPath)
	if err != nil {
		return ProvisionalAcquireIntent{}, err
	}
	return NewProvisionalAcquireIntent(destination, footprint, path, owner, operationID)
}

func (intent ProvisionalAcquireIntent) LegacyContentPath() (output.ContentPath, error) {
	if err := intent.Validate(); err != nil {
		return "", err
	}
	if err := intent.destination.ValidateScope(target.ScopeGlobal); err != nil {
		return "", fmt.Errorf("legacy provisional acquisition destination: %w", err)
	}
	return intent.footprint.LegacyContentPath()
}
