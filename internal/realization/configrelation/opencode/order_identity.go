package opencode

import (
	"fmt"
	"path/filepath"
	"strings"

	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

// PhysicalSequenceID derives the independently mutable plugin sequence owned
// by one OpenCode config document role.
func PhysicalSequenceID(
	scope target.Scope,
	kind ConfigKind,
	sourcePath string,
) (hostrelation.PhysicalSequenceID, error) {
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return "", fmt.Errorf("OpenCode plugin order scope: %w", err)
	}
	if _, err := CandidateNames(kind); err != nil {
		return "", err
	}

	variant := strings.TrimPrefix(filepath.Ext(sourcePath), ".")
	if variant != "json" && variant != "jsonc" {
		return "", fmt.Errorf(
			"OpenCode plugin order config %q has unsupported variant",
			sourcePath,
		)
	}
	return hostrelation.NewPhysicalSequenceID(
		"opencode:" + string(parsedScope) + ":" + string(kind) + "." + variant + ".plugins",
	)
}
