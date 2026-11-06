package execute

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/execute/statefileoperation"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/operationplan"
)

func (authority *mutationAuthority) bindProjectStatefile(
	selectedRoot string,
	statefilePath string,
) error {
	if authority != nil && authority.projectStatefile != nil {
		return fmt.Errorf("project statefile authority is already bound")
	}
	destination, err := authority.bindProjectControlEntry(selectedRoot, statefilePath)
	if err != nil {
		return fmt.Errorf("bind project statefile: %w", err)
	}
	authority.projectStatefile = statefileoperation.NewSelected(authority.filesystem, destination, authority.physicalWorkBudget)
	return nil
}

func (authority *mutationAuthority) bindRecoveryJournal(
	selectedRoot string,
	operationDir string,
) error {
	if authority != nil && (authority.recoveryJournal != nil || authority.recoveryJournalRecord != nil) {
		return fmt.Errorf("recovery journal authority is already bound")
	}
	directory, err := authority.bindProjectControlEntry(selectedRoot, operationDir)
	if err != nil {
		return fmt.Errorf("bind recovery journal: %w", err)
	}
	record, err := authority.bindProjectControlEntry(
		selectedRoot,
		filepath.Join(operationDir, "journal.json"),
	)
	if err != nil {
		return errors.Join(fmt.Errorf("bind recovery journal record: %w", err), directory.Close())
	}
	authority.recoveryJournal = directory
	authority.recoveryJournalRecord = record
	return nil
}

func (authority *mutationAuthority) captureJournalExecutionBasis(
	ctx context.Context,
	recordFingerprint string,
) error {
	if authority == nil || authority.filesystem == nil {
		return fmt.Errorf("recovery journal retirement authority is unavailable")
	}
	captured, err := journalinventory.CaptureActiveJournalAuthority(
		ctx,
		authority.filesystem,
		authority.recoveryJournal,
	)
	if err != nil {
		return fmt.Errorf("capture active recovery journal authority: %w", err)
	}
	basis, err := journalinventory.NewExecutionBasis(recordFingerprint, captured)
	if err != nil {
		return fmt.Errorf("build recovery journal execution basis: %w", err)
	}
	authority.journalBasis = basis
	return nil
}

func (authority *mutationAuthority) setJournalExecutionBasis(
	recordFingerprint string,
	activeAuthority journalinventory.ActiveJournalAuthority,
) error {
	if authority == nil {
		return fmt.Errorf("recovery journal retirement authority is unavailable")
	}
	basis, err := journalinventory.NewExecutionBasis(recordFingerprint, activeAuthority)
	if err != nil {
		return err
	}
	authority.journalBasis = basis
	return nil
}

func (authority *mutationAuthority) validateJournalExecutionBasis(
	ctx context.Context,
	plan recovery.Plan,
	phase string,
) error {
	if authority == nil || authority.filesystem == nil {
		return fmt.Errorf("recovery journal retirement authority is unavailable")
	}
	if err := authority.journalBasis.Validate(); err != nil {
		return fmt.Errorf("recovery journal execution basis is unavailable: %w", err)
	}
	if err := journalinventory.ValidateActiveJournalAuthority(
		ctx,
		authority.filesystem,
		authority.recoveryJournal,
		authority.journalBasis.ActiveAuthority(),
	); err != nil {
		return fmt.Errorf("active recovery journal changed %s: %w", phase, err)
	}
	return requireJournalAuthorityFingerprint(
		authority.journalBasis.RecordFingerprint(),
		plan,
		phase,
	)
}

func (authority *mutationAuthority) bindProjectControlEntry(
	selectedRoot string,
	path string,
) (*rootedpath.EntryAuthority, error) {
	if authority == nil {
		return nil, fmt.Errorf("project mutation authority is unavailable")
	}
	if authority.generalTraversalPhase == nil {
		return nil, fmt.Errorf("project control traversal authority is unavailable")
	}
	return rootedpath.BindSelectedControlEntryAuthorityBounded(
		authority.capturedRoot,
		selectedRoot,
		path,
		physicalwork.MaximumPhysicalPathDepth,
		authority.generalTraversalPhase,
	)
}

func (authority *mutationAuthority) validateProjectSelection(selectedRoot string) error {
	if authority == nil || authority.capturedRoot == nil {
		return fmt.Errorf("project mutation authority is unavailable")
	}
	return authority.capturedRoot.ValidateSelectionBounded(
		selectedRoot,
		physicalwork.MaximumPhysicalPathDepth,
		authority.generalTraversalPhase,
	)
}

func (authority *mutationAuthority) retireActiveJournal(
	ctx context.Context,
	plan recovery.Plan,
	outerExecution *activeRecoveryExecution,
) error {
	run := func(step activeRecoveryStep, action func() error) error {
		if outerExecution == nil {
			return action()
		}
		return outerExecution.runTerminalStep(step, action)
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/retirement/validate-before-cleanup",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			if authority == nil || authority.filesystem == nil {
				return fmt.Errorf("recovery journal retirement authority is unavailable")
			}
			if authority.preparedRetirement == nil {
				return fmt.Errorf("prepared recovery journal retirement is unavailable")
			}
			return authority.validateJournalExecutionBasis(
				ctx,
				plan,
				"before removal cleanup",
			)
		},
	); err != nil {
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/retirement/bind-removal-authority",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			if err := authority.removals.BindPlan(plan); err != nil {
				return fmt.Errorf("validate complete removal authority before retirement: %w", err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/retirement/validate-clean-plan",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			if plan.Blocked() || plan.HasErrors() {
				return fmt.Errorf("recovery journal retirement requires an effect-admissible clean plan")
			}
			if classification := plan.Classification(); classification != recovery.ClassificationCleanBefore &&
				classification != recovery.ClassificationCleanAfter {
				return fmt.Errorf("recovery journal retirement requires a clean classified plan")
			}
			return nil
		},
	); err != nil {
		return err
	}
	structure := operationplan.EffectStructure{}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/retirement/prepare-tail",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			var err error
			structure, err = journal.CompileRetirementSchedule()
			if err != nil {
				return fmt.Errorf("compile active journal retirement schedule: %w", err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/retirement/advance-basis",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			if err := authority.preparedRetirement.AdvanceActiveBasis(
				ctx,
				plan,
				authority.journalBasis.ActiveAuthority(),
			); err != nil {
				return fmt.Errorf("advance prepared journal retirement basis: %w", err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/retirement/removal-cleanup",
			kind: operationplan.EffectStepCleanup,
		},
		func() error {
			if _, err := authority.removals.CleanupResidues(ctx, plan); err != nil {
				return fmt.Errorf("reconcile journaled removal residues before retirement: %w", err)
			}
			return nil
		},
	); err != nil {
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/retirement/validate-journal",
			kind: operationplan.EffectStepObservation,
		},
		func() error {
			return authority.validateJournalExecutionBasis(
				ctx,
				plan,
				"before journal retirement",
			)
		},
	); err != nil {
		return err
	}
	if err := run(
		activeRecoveryStep{
			id:   "active-recovery/retirement/validate-semantics",
			kind: operationplan.EffectStepObservation,
		},
		func() error { return authority.control.Validate(ctx) },
	); err != nil {
		return err
	}
	return run(
		activeRecoveryStep{
			id:   "active-recovery/retirement/execute-tail",
			kind: operationplan.EffectStepRetirement,
		},
		func() error {
			retirementExecution := journal.NewRetirementExecution(structure)
			retirementErr := authority.preparedRetirement.ExecuteActiveWithGate(
				ctx,
				plan,
				retirementExecution,
			)
			return retirementExecution.Finish(retirementErr)
		},
	)
}

func (authority *mutationAuthority) prepareActiveJournalRetirement(
	ctx context.Context,
	paths Paths,
	plan recovery.Plan,
	stateCodec durable.SnapshotCodec,
) error {
	if authority == nil || authority.filesystem == nil || authority.physicalWorkBudget == nil {
		return fmt.Errorf("recovery journal retirement authority is unavailable")
	}
	if authority.preparedRetirement != nil {
		return authority.preparedRetirement.RequireActivePlan(plan)
	}
	prepared, err := journal.PrepareActiveJournalRetirement(
		ctx,
		plan,
		authority.journalBasis.ActiveAuthority(),
		paths.RecoveryDir,
		physicalwork.MaximumPhysicalPathDepth,
		authority.physicalWorkBudget,
		authority.filesystem,
		stateCodec,
	)
	if err != nil {
		return err
	}
	authority.preparedRetirement = prepared
	return nil
}
