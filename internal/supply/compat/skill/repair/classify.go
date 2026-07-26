package repair

import (
	"context"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillcompat "github.com/isty2e/daem/internal/supply/compat/skill"
	"github.com/isty2e/daem/internal/target"
)

// Repairability classifies whether a failed source can be fixed mechanically.
type Repairability string

const (
	RepairabilityNone       Repairability = "none"
	RepairabilityMechanical Repairability = "mechanical"
	RepairabilityManual     Repairability = "manual"
)

// Classification is diagnose-only repairability evidence, not a recipe.
type Classification struct {
	repairability Repairability
	actions       []string
	manualReasons []string
}

// Repairability returns the classification category.
func (classification Classification) Repairability() Repairability {
	return classification.repairability
}

// Actions returns deterministic mechanical action summaries.
func (classification Classification) Actions() []string {
	return append([]string(nil), classification.actions...)
}

// ManualReasons returns non-executable repair guidance.
func (classification Classification) ManualReasons() []string {
	return append([]string(nil), classification.manualReasons...)
}

// ManualError reports compatibility issues outside the mechanical registry.
type ManualError struct {
	actions []string
	reasons []string
}

func (err ManualError) Error() string {
	if len(err.reasons) == 0 {
		return "manual skill compatibility repair required"
	}
	return "manual skill compatibility repair required: " + strings.Join(err.reasons, "; ")
}

func (err ManualError) Actions() []string { return append([]string(nil), err.actions...) }
func (err ManualError) Reasons() []string { return append([]string(nil), err.reasons...) }

type repairDraft struct {
	operations []Operation
	manual     []string
}

func (draft repairDraft) actions() []string {
	actions := make([]string, 0, len(draft.operations))
	for _, operation := range draft.operations {
		actions = append(actions, operation.Summary())
	}
	return actions
}

func (draft *repairDraft) addManual(reason string) {
	if reason == "" {
		return
	}
	if slices.Contains(draft.manual, reason) {
		return
	}
	draft.manual = append(draft.manual, reason)
}

func newManualError(draft repairDraft) ManualError {
	return ManualError{actions: draft.actions(), reasons: append([]string(nil), draft.manual...)}
}

// Classify checks repairability without retaining temporary output.
func Classify(
	ctx context.Context,
	input artifact.ExactIdentity,
	view access.View,
	installName string,
	targets []target.Target,
) (Classification, error) {
	result, err := Repair(ctx, input, view, installName, targets)
	if err != nil {
		manual, ok := err.(ManualError)
		if !ok {
			return Classification{}, err
		}
		return Classification{
			repairability: RepairabilityManual,
			actions:       manual.Actions(),
			manualReasons: manual.Reasons(),
		}, nil
	}
	if recipe, ok := result.Recipe(); ok {
		if err := result.Release(); err != nil {
			return Classification{}, err
		}
		return Classification{repairability: RepairabilityMechanical, actions: recipe.Actions()}, nil
	}
	return Classification{repairability: RepairabilityNone}, nil
}

func collectBlockingDiagnostics(
	ctx context.Context,
	root string,
	sourceID artifact.SourceID,
	installName string,
	targets []target.Target,
	draft *repairDraft,
) error {
	diagnostics, err := diagnosticsForTargets(ctx, root, sourceID, installName, targets)
	if err != nil {
		return err
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Blocking() {
			draft.addManual(manualAction(diagnostic))
		}
	}
	if len(draft.manual) != 0 {
		return newManualError(*draft)
	}
	return nil
}

func diagnosticsForTargets(
	ctx context.Context,
	root string,
	sourceID artifact.SourceID,
	installName string,
	targets []target.Target,
) ([]skillcompat.Diagnostic, error) {
	view, err := access.OpenView(root)
	if err != nil {
		return nil, err
	}
	diagnostics := make([]skillcompat.Diagnostic, 0)
	for _, selectedTarget := range targets {
		diagnostics = append(
			diagnostics,
			skillcompat.Diagnostics(ctx, view, sourceID, installName, selectedTarget)...,
		)
	}
	return diagnostics, nil
}

func nameRepairsFromDiagnostics(diagnostics []skillcompat.Diagnostic) (addName bool, alignName bool) {
	for _, diagnostic := range diagnostics {
		if !diagnostic.Blocking() {
			continue
		}
		switch diagnostic.Code {
		case "missing-name":
			addName = true
		case "name-install-name-mismatch":
			alignName = true
		}
	}
	return addName, alignName
}

func manualAction(diagnostic skillcompat.Diagnostic) string {
	switch diagnostic.Code {
	case "missing-description":
		return "SKILL.md frontmatter description is required; provide an author-written description"
	case "description-too-long":
		return "SKILL.md frontmatter description is too long for a selected target; rewrite it manually"
	case "invalid-name", "name-too-long":
		return "SKILL.md frontmatter name cannot be mechanically aligned with the selected target; choose a compatible skill name manually"
	case "name-install-name-mismatch":
		return "SKILL.md frontmatter name must match the daem skill name; edit the name manually"
	default:
		return diagnostic.Message
	}
}
