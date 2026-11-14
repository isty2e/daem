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
		rootFact{"claude-account", "claude-account-point-v2"},
		rootFact{"claude-account-owned-source", "claude-owned-source-point-v1"},
		rootFact{"opencode-configuration", "opencode-location-v1"},
		rootFact{"antigravity-preferences", "antigravity-preferences-location-v1"},
		rootFact{"antigravity-shared", "antigravity-shared-location-v1"},
		rootFact{"antigravity-shared", "antigravity-shared-point-v2"}:
		return nil
	default:
		return fmt.Errorf("native root reference has no supported locator contract")
	}
}

// NativeRootIsPoint retains selection shape with the locator revision, not the
// read-only protection envelope of a similarly named anchor.
func NativeRootIsPoint(reference output.RootReference) (bool, error) {
	if reference.Identity() == ProjectRootIdentity && reference.LocatorPolicy() == "selected-project-v1" {
		return false, nil
	}
	if err := AdmitNativeAliasRootReference(reference); err != nil {
		return false, err
	}
	return NativeRootIsCurrentMCPPoint(reference) || reference.LocatorPolicy() == "antigravity-shared-point-v2" || NativeRootIsOwnedPoint(reference), nil
}

func NativeRootIsCurrentMCPPoint(reference output.RootReference) bool {
	return reference.Identity() == "claude-account" && reference.LocatorPolicy() == "claude-account-point-v2"
}

// NativeRootIsOwnedPoint distinguishes a fixed owned document from a current
// preferred/fallback chooser. Neither kind grants its containing directory.
func NativeRootIsOwnedPoint(reference output.RootReference) bool {
	return reference.Identity() == "claude-account-owned-source" && reference.LocatorPolicy() == "claude-owned-source-point-v1"
}
