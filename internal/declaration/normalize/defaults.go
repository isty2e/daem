package normalize

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/target"
)

func normalizeDefaults(raw declaration.Defaults) (desired.Defaults, error) {
	scopeValue := raw.Scope
	if scopeValue == "" {
		scopeValue = string(target.ScopeProject)
	}

	scope, err := target.ParseScope(scopeValue)
	if err != nil {
		return desired.Defaults{}, fmt.Errorf("defaults.scope: %w", err)
	}

	installModeValue := raw.InstallMode
	if installModeValue == "" {
		installModeValue = string(desiredskill.InstallModeCopy)
	}

	installMode, err := desiredskill.ParseInstallMode(installModeValue)
	if err != nil {
		return desired.Defaults{}, fmt.Errorf("defaults.install_mode: %w", err)
	}

	defaults, err := desired.NewDefaults(scope, installMode)
	if err != nil {
		return desired.Defaults{}, fmt.Errorf("defaults: %w", err)
	}
	return defaults, nil
}
