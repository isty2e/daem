package skillcompat

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

// Validate verifies that a skill artifact is compatible with the selected target.
func Validate(
	ctx context.Context,
	view access.View,
	sourceID artifact.SourceID,
	installName string,
	target target.Target,
) error {
	for _, diagnostic := range Diagnostics(ctx, view, sourceID, installName, target) {
		if diagnostic.Blocking() {
			return fmt.Errorf("%s", diagnostic.Message)
		}
	}

	return nil
}

// Diagnostics reports target-specific compatibility findings for a skill artifact.
func Diagnostics(
	ctx context.Context,
	view access.View,
	sourceID artifact.SourceID,
	installName string,
	target target.Target,
) []Diagnostic {
	profile, ok := profileForTarget(target)
	if !ok {
		return []Diagnostic{errorDiagnostic(
			AxisDiscovery,
			"missing-profile",
			"skill source %q target %q: skill compatibility profile is not defined",
			sourceID,
			target,
		)}
	}

	frontmatter, err := LoadSkillFrontmatter(ctx, view, sourceID)
	if err != nil {
		var limitErr *SkillDocumentLimitError
		if errors.As(err, &limitErr) {
			return []Diagnostic{errorDiagnostic(
				AxisArtifact,
				"skill-document-too-large",
				"skill source %q target %q: SKILL.md is at least %d bytes; maximum supported size is %d bytes",
				sourceID,
				target,
				limitErr.Observed(),
				limitErr.Limit(),
			)}
		}
		return []Diagnostic{errorDiagnostic(
			AxisFrontmatter,
			"invalid-frontmatter",
			"skill source %q target %q: %v",
			sourceID,
			target,
			err,
		)}
	}

	return frontmatterDiagnostics(sourceID, installName, profile, frontmatter)
}
