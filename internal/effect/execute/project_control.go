package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
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
	authority.projectStatefile = destination
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

type journalExecutionBasis struct {
	recordFingerprint string
	activeAuthority   journal.ActiveJournalAuthority
}

func newJournalExecutionBasis(
	recordFingerprint string,
	activeAuthority journal.ActiveJournalAuthority,
) (journalExecutionBasis, error) {
	if recordFingerprint == "" {
		return journalExecutionBasis{}, fmt.Errorf(
			"recovery journal record fingerprint is required",
		)
	}
	if err := activeAuthority.Validate(); err != nil {
		return journalExecutionBasis{}, err
	}
	return journalExecutionBasis{
		recordFingerprint: recordFingerprint,
		activeAuthority:   activeAuthority,
	}, nil
}

func (basis journalExecutionBasis) validate() error {
	_, err := newJournalExecutionBasis(
		basis.recordFingerprint,
		basis.activeAuthority,
	)
	return err
}

func (authority *mutationAuthority) captureJournalExecutionBasis(
	ctx context.Context,
	recordFingerprint string,
) error {
	if authority == nil || authority.filesystem == nil {
		return fmt.Errorf("recovery journal retirement authority is unavailable")
	}
	captured, err := journal.CaptureActiveJournalAuthority(
		ctx,
		authority.filesystem,
		authority.recoveryJournal,
	)
	if err != nil {
		return fmt.Errorf("capture active recovery journal authority: %w", err)
	}
	basis, err := newJournalExecutionBasis(recordFingerprint, captured)
	if err != nil {
		return fmt.Errorf("build recovery journal execution basis: %w", err)
	}
	authority.journalBasis = basis
	return nil
}

func (authority *mutationAuthority) setJournalExecutionBasis(
	recordFingerprint string,
	activeAuthority journal.ActiveJournalAuthority,
) error {
	if authority == nil {
		return fmt.Errorf("recovery journal retirement authority is unavailable")
	}
	basis, err := newJournalExecutionBasis(recordFingerprint, activeAuthority)
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
	if err := authority.journalBasis.validate(); err != nil {
		return fmt.Errorf("recovery journal execution basis is unavailable: %w", err)
	}
	if err := journal.ValidateActiveJournalAuthority(
		ctx,
		authority.filesystem,
		authority.recoveryJournal,
		authority.journalBasis.activeAuthority,
	); err != nil {
		return fmt.Errorf("active recovery journal changed %s: %w", phase, err)
	}
	return requireJournalAuthorityFingerprint(
		authority.journalBasis.recordFingerprint,
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
	if strings.IndexFunc(path, func(character rune) bool {
		return character != '\x00' && unicode.IsControl(character)
	}) >= 0 {
		return rootedpath.BindCanonicalSelectedEntryAuthorityBounded(
			authority.capturedRoot,
			selectedRoot,
			path,
			recovery.MaximumPhysicalPathDepth,
			authority.generalTraversalPhase,
		)
	}
	return rootedpath.BindSelectedEntryAuthorityBounded(
		authority.capturedRoot,
		selectedRoot,
		path,
		recovery.MaximumPhysicalPathDepth,
		authority.generalTraversalPhase,
	)
}

func (authority *mutationAuthority) validateProjectSelection(selectedRoot string) error {
	if authority == nil || authority.capturedRoot == nil {
		return fmt.Errorf("project mutation authority is unavailable")
	}
	return authority.capturedRoot.ValidateSelectionBounded(
		selectedRoot,
		recovery.MaximumPhysicalPathDepth,
		authority.generalTraversalPhase,
	)
}

func (authority *mutationAuthority) commitProjectStatefile(
	ctx context.Context,
	content []byte,
	mode os.FileMode,
) statefileCommitOutcome {
	if authority == nil || authority.filesystem == nil ||
		authority.projectStatefile == nil {
		return statefileCommitOutcome{
			status: statefileUncommitted,
			err:    fmt.Errorf("project statefile authority is unavailable"),
		}
	}
	err := commitRootedControlFile(
		ctx,
		authority.filesystem,
		authority.projectStatefile,
		content,
		mode,
	)
	if err == nil {
		return statefileCommitOutcome{status: statefileCommitted}
	}
	status := statefileUncommitted
	if mutationfs.MayHaveVisibleEffect(err) {
		status = statefileCommitIndeterminate
	}
	return statefileCommitOutcome{status: status, err: err}
}

func commitRootedControlFile(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	content []byte,
	mode os.FileMode,
) error {
	if filesystem == nil || authority == nil {
		return fmt.Errorf("rooted control-file authority is required")
	}
	capability, err := authority.Acquire()
	if err != nil {
		return err
	}
	expected, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if errors.Is(err, os.ErrNotExist) {
		return filesystem.CreateRootedFile(ctx, capability, content, mode)
	}
	if err != nil {
		_ = capability.Close()
		return err
	}
	return filesystem.ReplaceRootedFile(ctx, capability, content, mode, expected)
}

func (authority *mutationAuthority) retireActiveJournal(
	ctx context.Context,
	plan recovery.Plan,
) error {
	if authority == nil || authority.filesystem == nil {
		return fmt.Errorf("recovery journal retirement authority is unavailable")
	}
	if authority.preparedRetirement == nil {
		return fmt.Errorf("prepared recovery journal retirement is unavailable")
	}
	if err := authority.validateJournalExecutionBasis(
		ctx,
		plan,
		"before removal cleanup",
	); err != nil {
		return err
	}
	if err := authority.bindRemovalIntents(plan); err != nil {
		return fmt.Errorf("validate complete removal authority before retirement: %w", err)
	}
	if plan.Blocked() || plan.HasErrors() {
		return fmt.Errorf("recovery journal retirement requires an effect-admissible clean plan")
	}
	if classification := plan.Classification(); classification != recovery.ClassificationCleanBefore &&
		classification != recovery.ClassificationCleanAfter {
		return fmt.Errorf("recovery journal retirement requires a clean classified plan")
	}
	if err := authority.preparedRetirement.AdvanceActiveBasis(
		ctx,
		plan,
		authority.journalBasis.activeAuthority,
	); err != nil {
		return fmt.Errorf("advance prepared journal retirement basis: %w", err)
	}
	if _, err := authority.cleanupRemovalResidues(ctx, plan); err != nil {
		return fmt.Errorf("reconcile journaled removal residues before retirement: %w", err)
	}
	if err := authority.validateJournalExecutionBasis(
		ctx,
		plan,
		"before journal retirement",
	); err != nil {
		return err
	}
	if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
		return err
	}
	return authority.preparedRetirement.ExecuteActive(ctx, plan)
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
		authority.journalBasis.activeAuthority,
		paths.RecoveryDir,
		recovery.MaximumPhysicalPathDepth,
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
