package configuration

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
)

// AdmitNativeAliasRootReference recognizes locator versions independently of
// today's placement selection. Retained records still name earlier versions.
func AdmitNativeAliasRootReference(reference output.RootReference) error {
	if err := reference.Validate(); err != nil {
		return err
	}
	key := rootFact{reference.Identity(), reference.LocatorPolicy()}
	switch key {
	case rootFact{"pi-agent", "pi-agent-location-v1"},
		rootFact{"codex-home", "codex-home-location-v1"},
		rootFact{"claude-preferences", "claude-preferences-location-v1"},
		rootFact{"claude-account", "claude-account-location-v1"},
		rootFact{"opencode-configuration", "opencode-location-v1"},
		rootFact{"antigravity-preferences", "antigravity-preferences-location-v1"},
		rootFact{"antigravity-shared", "antigravity-shared-location-v1"}:
		return nil
	default:
		return fmt.Errorf("native root reference has no supported locator contract")
	}
}
