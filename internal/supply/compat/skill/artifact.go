package skillcompat

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// ValidateSkillArtifact verifies that an accessed artifact has skill directory shape.
func ValidateSkillArtifact(ctx context.Context, view access.View, sourceID artifact.SourceID) error {
	if view.Kind() != artifact.ArtifactKindDirectory {
		return fmt.Errorf("skill source %q must resolve to a directory", sourceID)
	}
	return validateSkillFile(ctx, view, sourceID)
}

func validateSkillFile(
	ctx context.Context,
	view access.View,
	sourceID artifact.SourceID,
) error {
	var kind access.EntryKind
	// Continue past a match so later entry errors and the final root verification are not skipped.
	err := view.VisitDirectory(ctx, ".", func(entry access.Entry) error {
		if entry.Name() == "SKILL.md" {
			kind = entry.Kind()
		}
		return nil
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("read skill source %q directory: %w", sourceID, err)
	}

	switch kind {
	case "":
		return fmt.Errorf("skill source %q is missing SKILL.md", sourceID)
	case access.EntryKindFile:
		return nil
	case access.EntryKindSymlink:
		return fmt.Errorf("skill source %q has SKILL.md as a symlink", sourceID)
	default:
		return fmt.Errorf("skill source %q has SKILL.md as a non-regular file", sourceID)
	}
}
