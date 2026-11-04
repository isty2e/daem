package ownership

import (
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
)

// NewProvisionalAcquireIntentFromLegacy treats old segments literally, including
// tilde spellings. An empty legacy scope remains whole-path authority.
func NewProvisionalAcquireIntentFromLegacy(destination output.Destination, contentPath output.ContentPath, path pathauthority.Provisional, owner stateauthority.Authority, operationID string) (ProvisionalAcquireIntent, error) {
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
	return intent.footprint.LegacyContentPath()
}
