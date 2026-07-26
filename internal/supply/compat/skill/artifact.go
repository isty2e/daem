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
	_, err := exactSkillFile(ctx, view, sourceID)
	return err
}

func exactSkillFile(
	ctx context.Context,
	view access.View,
	sourceID artifact.SourceID,
) (access.Entry, error) {
	entries, err := view.ReadDirectory(ctx, ".")
	if err != nil {
		return access.Entry{}, fmt.Errorf("read skill source %q directory: %w", sourceID, err)
	}
	for _, entry := range entries {
		if entry.Name() != "SKILL.md" {
			continue
		}
		switch entry.Kind() {
		case access.EntryKindFile:
			return entry, nil
		case access.EntryKindSymlink:
			return access.Entry{}, fmt.Errorf("skill source %q has SKILL.md as a symlink", sourceID)
		default:
			return access.Entry{}, fmt.Errorf("skill source %q has SKILL.md as a non-regular file", sourceID)
		}
	}
	return access.Entry{}, fmt.Errorf("skill source %q is missing SKILL.md", sourceID)
}
