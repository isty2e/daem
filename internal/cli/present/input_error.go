package clipresent

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source/backend/localfs"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

const manualSkillRepairHint = "\nnext: edit the source SKILL.md to fix the reported compatibility issues, then retry"

func inputError(err error) string {
	var guidance *skillrepair.GuidanceError
	if errors.As(err, &guidance) {
		cause := guidance.Unwrap()
		raw := err.Error()
		if raw == guidance.Error() {
			raw = cause.Error()
		}
		message := Escape(raw)
		classification := guidance.Classification()
		if classification.Repairability() == skillrepair.RepairabilityMechanical {
			return message + "\nnext: fix the source SKILL.md, or declare the resource with compat_repair = true in the manifest and run daem lock" +
				"\nrepair actions: " + Escape(strings.Join(classification.Actions(), "; "))
		}
		for _, reason := range classification.ManualReasons() {
			if !strings.Contains(raw, reason) {
				message += "\ndetail: " + Escape(reason)
			}
		}
		return message + manualSkillRepairHint
	}

	var manual skillrepair.ManualError
	if errors.As(err, &manual) {
		return Escape(err.Error()) + "\nnext: fix the reported source issues manually, then retry"
	}

	message := Escape(err.Error())
	if line, column, ok := tomlstrict.ErrorPosition(err); ok {
		message += fmt.Sprintf(" (line %d, column %d)", line, column)
		return message + "\nnext: check TOML brackets, quotes, and key/value assignments"
	}

	var unknown declaration.UnknownManifestKeyError
	if errors.As(err, &unknown) {
		return message + "\nnext: correct or remove this key; see https://github.com/isty2e/daem/blob/main/docs/manifest.md"
	}

	if errors.Is(err, authoring.ErrMissingGitRef) {
		return message + "\nnext: supply --ref with a branch, tag, or commit; local paths do not need --ref"
	}
	return message
}

// PrintMissingSourceHint emits advice only for a missing local source root,
// not for an unrelated filesystem failure or an error with similar wording.
func PrintMissingSourceHint(output io.Writer, err error) bool {
	if !localfs.IsSourceUnavailable(err) {
		return false
	}
	fmt.Fprintln(output, "next: check that the source exists at this path; correct the source argument or the manifest source path")
	return true
}
