package piorder

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/isty2e/daem/internal/realization/aggregate"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

const (
	// MaximumSettingsBytes is the observation and mutation limit for Pi settings.
	MaximumSettingsBytes = 4 << 20
)

// settingsImage is one immutable, scope-specific Pi package settings observation.
type settingsImage struct {
	scope         target.Scope
	settingsPath  string
	settingsBase  string
	revision      string
	exists        bool
	content       []byte
	document      piconfig.Document
	sourceContext extensiontopology.LocalSourceContext
}

func (inventory settingsImage) Document() aggregate.Document {
	if !inventory.exists {
		return aggregate.AbsentDocument()
	}
	return aggregate.ExistingDocument(inventory.content)
}

func SettingsRevision(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}
