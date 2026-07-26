package repair

import (
	"context"
	"errors"
	"fmt"
	"os"

	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
)

func planAndApplySkillFileCasing(
	ctx context.Context,
	root string,
	draft *repairDraft,
) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		draft.addManual(fmt.Sprintf("read skill source directory: %v", err))
		return newManualError(*draft)
	}

	hasUppercase := false
	hasLowercase := false
	for _, entry := range entries {
		switch entry.Name() {
		case "SKILL.md":
			hasUppercase = true
		case "skill.md":
			hasLowercase = true
		}
	}
	if hasUppercase {
		return nil
	}
	if !hasLowercase {
		draft.addManual("skill source is missing SKILL.md; add a skill file manually")
		return newManualError(*draft)
	}

	state, err := skillDocumentState(ctx, root, "skill.md")
	if err != nil {
		if errors.Is(err, skillcompat.ErrSkillDocumentTooLarge) {
			return err
		}
		draft.addManual(fmt.Sprintf("rename skill.md to SKILL.md manually: %v", err))
		return newManualError(*draft)
	}
	operation, err := NewRenameOperation("skill.md", "SKILL.md", state.Hash, state.Mode)
	if err != nil {
		return fmt.Errorf("construct skill filename repair: %w", err)
	}
	if err := applyOperation(ctx, root, operation); err != nil {
		return fmt.Errorf("apply skill filename repair: %w", err)
	}
	draft.operations = append(draft.operations, operation)
	return nil
}
