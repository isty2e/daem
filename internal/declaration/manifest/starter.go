package manifest

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
)

// StarterContent returns the canonical starter declaration used by init.
func StarterContent() []byte {
	return fmt.Appendf(
		nil,
		"version = %d\ntargets = [\"codex\"]\n",
		declaration.CurrentManifestVersion,
	)
}
