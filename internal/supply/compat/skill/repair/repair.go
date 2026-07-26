package repair

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

// Result is either the original artifact or one owned repaired materialization.
type Result struct {
	identity        artifact.ExactIdentity
	originalView    access.View
	recipe          *Recipe
	materialization *access.Materialization
}

// Identity returns the exact bytes represented by this result.
func (result Result) Identity() artifact.ExactIdentity { return result.identity }

// View returns access to the live result and rejects released repaired output.
func (result Result) View() (access.View, error) {
	if result.materialization != nil {
		return result.materialization.View()
	}
	if err := result.identity.Validate(); err != nil {
		return access.View{}, fmt.Errorf("skill repair result: %w", err)
	}
	return result.originalView, nil
}

// Recipe returns the canonical recipe when bytes were repaired.
func (result Result) Recipe() (Recipe, bool) {
	if result.recipe == nil {
		return Recipe{}, false
	}
	return result.recipe.clone(), true
}

// Release cleans repaired staging. It is idempotent and reports cleanup errors.
func (result Result) Release() error {
	if result.materialization == nil {
		return nil
	}
	return result.materialization.Release()
}

// Repair copies and verifies original bytes, derives admitted operations, and
// returns either the unchanged source view or owned repaired staging.
func Repair(
	ctx context.Context,
	input artifact.ExactIdentity,
	view access.View,
	installName string,
	targets []target.Target,
) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("skill repair context is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := input.Validate(); err != nil {
		return Result{}, fmt.Errorf("skill repair input: %w", err)
	}
	if input.Kind() != artifact.ArtifactKindDirectory || view.Kind() != artifact.ArtifactKindDirectory {
		return Result{}, ManualError{reasons: []string{"skill source must resolve to a directory"}}
	}
	if err := validateInstallName(installName); err != nil {
		return Result{}, err
	}

	staging, err := newVerifiedRepairStaging(ctx, input, view)
	if err != nil {
		return Result{}, err
	}
	draft := repairDraft{}
	if err := planAndApply(ctx, staging.artifactRoot, input.SourceID(), installName, targets, &draft); err != nil {
		return Result{}, releaseWithError(&staging, err)
	}

	repairedView, err := staging.openView()
	if err != nil {
		return Result{}, releaseWithError(&staging, err)
	}
	repairedHash, err := repairedView.Hash(ctx)
	if err != nil {
		return Result{}, releaseWithError(&staging, fmt.Errorf("hash repaired skill source: %w", err))
	}
	output, err := artifact.NewExactIdentity(
		input.SourceID(),
		input.ResolvedRef(),
		artifact.ArtifactKindDirectory,
		repairedHash,
	)
	if err != nil {
		return Result{}, releaseWithError(&staging, err)
	}

	if len(draft.operations) == 0 {
		if !output.Equal(input) {
			return Result{}, releaseWithError(
				&staging,
				fmt.Errorf("skill repair changed artifact without recording an operation"),
			)
		}
		if err := staging.release(); err != nil {
			return Result{}, err
		}
		return unchangedResult(input, view)
	}
	if err := staging.finalizeDirectoryModes(); err != nil {
		return Result{}, releaseWithError(&staging, err)
	}

	recipe, err := NewRecipe(input, output, draft.operations)
	if err != nil {
		return Result{}, releaseWithError(&staging, err)
	}
	if _, err := recipe.Inverse(); err != nil {
		return Result{}, releaseWithError(&staging, fmt.Errorf("construct inverse skill repair recipe: %w", err))
	}
	materialization, err := staging.materialization()
	if err != nil {
		return Result{}, err
	}
	return repairedResult(output, recipe, materialization)
}

// Replay applies a validated recipe to exact original bytes without inference.
func Replay(ctx context.Context, recipe Recipe, view access.View) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("skill repair replay context is required")
	}
	if err := recipe.Validate(); err != nil {
		return Result{}, err
	}
	staging, err := newVerifiedRepairStaging(ctx, recipe.input, view)
	if err != nil {
		return Result{}, err
	}
	if err := applyOperations(ctx, staging.artifactRoot, recipe.operations); err != nil {
		return Result{}, releaseWithError(&staging, err)
	}
	if err := staging.finalizeDirectoryModes(); err != nil {
		return Result{}, releaseWithError(&staging, err)
	}
	repairedView, err := staging.openView()
	if err != nil {
		return Result{}, releaseWithError(&staging, err)
	}
	repairedHash, err := repairedView.Hash(ctx)
	if err != nil {
		return Result{}, releaseWithError(&staging, fmt.Errorf("hash replayed skill source: %w", err))
	}
	actual, err := artifact.NewExactIdentity(
		recipe.input.SourceID(),
		recipe.input.ResolvedRef(),
		recipe.input.Kind(),
		repairedHash,
	)
	if err != nil {
		return Result{}, releaseWithError(&staging, err)
	}
	if !actual.Equal(recipe.output) {
		return Result{}, releaseWithError(
			&staging,
			fmt.Errorf("replayed artifact identity does not match recipe output"),
		)
	}
	materialization, err := staging.materialization()
	if err != nil {
		return Result{}, err
	}
	return repairedResult(actual, recipe, materialization)
}

func planAndApply(
	ctx context.Context,
	root string,
	sourceID artifact.SourceID,
	installName string,
	targets []target.Target,
	draft *repairDraft,
) error {
	if err := planAndApplySkillFileCasing(ctx, root, draft); err != nil {
		return err
	}
	if err := planAndApplyFrontmatterDelimiter(ctx, root, draft); err != nil {
		return err
	}

	frontmatter, err := loadFrontmatter(ctx, root, sourceID)
	if err != nil {
		draft.addManual(err.Error())
		return newManualError(*draft)
	}
	diagnostics, err := diagnosticsForTargets(ctx, root, sourceID, installName, targets)
	if err != nil {
		return err
	}
	addName, alignName := nameRepairsFromDiagnostics(diagnostics)
	if addName && strings.TrimSpace(frontmatter.Name) == "" {
		var oldValue *string
		if _, present := frontmatter.Fields["name"]; present {
			value, isString := frontmatter.StringField("name")
			if !isString {
				draft.addManual("SKILL.md frontmatter field \"name\" is not a string")
				return newManualError(*draft)
			}
			oldValue = &value
		}
		if err := planAndApplyFrontmatterString(ctx, root, sourceID, "name", oldValue, installName, draft); err != nil {
			return err
		}
	}
	if alignName && strings.TrimSpace(frontmatter.Name) != "" && frontmatter.Name != installName {
		oldValue := frontmatter.Name
		if err := planAndApplyFrontmatterString(ctx, root, sourceID, "name", &oldValue, installName, draft); err != nil {
			return err
		}
	}
	if _, err := loadFrontmatter(ctx, root, sourceID); err != nil {
		draft.addManual(err.Error())
		return newManualError(*draft)
	}
	if err := collectBlockingDiagnostics(ctx, root, sourceID, installName, targets, draft); err != nil {
		return err
	}
	return ctx.Err()
}

func unchangedResult(identity artifact.ExactIdentity, view access.View) (Result, error) {
	if err := identity.Validate(); err != nil {
		return Result{}, err
	}
	if view.Kind() != identity.Kind() {
		return Result{}, fmt.Errorf("skill repair result view kind does not match identity")
	}
	return Result{identity: identity, originalView: view}, nil
}

func repairedResult(
	identity artifact.ExactIdentity,
	recipe Recipe,
	materialization *access.Materialization,
) (Result, error) {
	if materialization == nil {
		return Result{}, fmt.Errorf("repaired skill result requires materialization")
	}
	if !identity.Equal(recipe.Output()) {
		return Result{}, errors.Join(
			fmt.Errorf("skill repair result identity does not match recipe output"),
			materialization.Release(),
		)
	}
	view, err := materialization.View()
	if err != nil {
		return Result{}, errors.Join(err, materialization.Release())
	}
	if view.Kind() != identity.Kind() {
		return Result{}, errors.Join(
			fmt.Errorf("repaired skill materialization kind does not match identity"),
			materialization.Release(),
		)
	}
	cloned := recipe.clone()
	return Result{identity: identity, recipe: &cloned, materialization: materialization}, nil
}

func applyOperations(
	ctx context.Context,
	root string,
	operations []Operation,
) error {
	for index, operation := range operations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := applyOperation(ctx, root, operation); err != nil {
			return fmt.Errorf("repair operation[%d]: %w", index, err)
		}
	}
	return nil
}

func releaseWithError(staging *repairStaging, operationErr error) error {
	releaseErr := staging.release()
	if releaseErr == nil {
		return operationErr
	}
	return errors.Join(operationErr, fmt.Errorf("release skill repair staging: %w", releaseErr))
}
