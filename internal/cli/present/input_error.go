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

const manualSkillRepairInstruction = "edit the source SKILL.md to fix the reported compatibility issues, then retry"

const manualSkillRepairHint = "\nnext: " + manualSkillRepairInstruction

// RecognizedInputError renders owned input guidance without admitting an unknown
// error to the caller's raw-text fallback.
func RecognizedInputError(err error) (string, bool) {
	diagnostic, recognized := RecognizedInputDiagnostic(err)
	if !recognized {
		return "", false
	}
	return diagnostic.HumanText(), true
}

// RecognizedInputDiagnostic projects typed input facts before human annotations
// are rendered. Unknown errors retain the caller's fallback policy.
func RecognizedInputDiagnostic(err error) (FailureDiagnostic, bool) {
	if err == nil {
		return FailureDiagnostic{}, false
	}
	var guidance *skillrepair.GuidanceError
	if errors.As(err, &guidance) {
		raw := err.Error()
		diagnostic := NewFailureDiagnostic(raw)
		classification := guidance.Classification()
		if classification.Repairability() == skillrepair.RepairabilityMechanical {
			return diagnostic.WithGuidance(
				InstructionGuidance("fix the source SKILL.md, or declare the resource with compat_repair = true in the manifest and run daem lock"),
				textGuidance(GuidanceInstruction, "repair actions: ", strings.Join(classification.Actions(), "; ")),
			), true
		}
		for _, reason := range classification.ManualReasons() {
			if !strings.Contains(raw, reason) {
				diagnostic = diagnostic.WithGuidance(textGuidance(GuidanceNote, "detail: ", reason))
			}
		}
		return diagnostic.WithGuidance(InstructionGuidance(manualSkillRepairInstruction)), true
	}

	var manual skillrepair.ManualError
	if errors.As(err, &manual) {
		return NewFailureDiagnostic(err.Error()).WithGuidance(InstructionGuidance("fix the reported source issues manually, then retry")), true
	}

	diagnostic := NewFailureDiagnostic(err.Error())
	if line, column, ok := tomlstrict.ErrorPosition(err); ok {
		diagnostic.message += fmt.Sprintf(" (line %d, column %d)", line, column)
		return diagnostic.WithGuidance(InstructionGuidance("check TOML brackets, quotes, and key/value assignments")), true
	}

	var unknown declaration.UnknownManifestKeyError
	if errors.As(err, &unknown) {
		if line, column, ok := unknown.Position(); ok {
			diagnostic.message += fmt.Sprintf(" (line %d, column %d)", line, column)
		}
		return diagnostic.WithGuidance(InstructionGuidance("correct or remove this key; see https://github.com/isty2e/daem/blob/main/docs/manifest.md")), true
	}

	if errors.Is(err, authoring.ErrMissingGitRef) {
		return diagnostic.WithGuidance(InstructionGuidance("supply --ref with a branch, tag, or commit; local paths do not need --ref")), true
	}
	return FailureDiagnostic{}, false
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
