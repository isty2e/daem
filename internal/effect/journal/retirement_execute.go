package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type retirementStart uint8

const (
	retirementStartInvalid retirementStart = iota
	retirementStartActive
	retirementStartLegacy
	retirementStartPrepared
	retirementStartFinalizingWithResidue
	retirementStartFinalizingWithoutResidue
)

type retirementExecution struct {
	record             retirement.Record
	activePath         string
	activeAuthority    ActiveJournalAuthority
	legacyPath         string
	legacyAuthority    LegacyJournalAuthority
	journalFingerprint string
	start              retirementStart
}

func (execution retirementExecution) valid() bool {
	if _, err := retirement.Encode(execution.record); err != nil {
		return false
	}
	switch execution.start {
	case retirementStartActive:
		return execution.activePath != "" &&
			execution.activeAuthority.valid() &&
			execution.legacyPath == "" &&
			!execution.legacyAuthority.valid() &&
			execution.journalFingerprint != "" &&
			execution.record.Phase() == retirement.PhasePrepared
	case retirementStartLegacy:
		return execution.activePath == "" &&
			!execution.activeAuthority.valid() &&
			execution.legacyPath != "" &&
			execution.legacyAuthority.valid() &&
			execution.journalFingerprint != "" &&
			execution.record.Phase() == retirement.PhasePrepared
	case retirementStartPrepared:
		return execution.activePath == "" &&
			!execution.activeAuthority.valid() &&
			execution.legacyPath == "" &&
			!execution.legacyAuthority.valid() &&
			execution.journalFingerprint == "" &&
			execution.record.Phase() == retirement.PhasePrepared
	case retirementStartFinalizingWithResidue,
		retirementStartFinalizingWithoutResidue:
		return execution.activePath == "" &&
			!execution.activeAuthority.valid() &&
			execution.legacyPath == "" &&
			!execution.legacyAuthority.valid() &&
			execution.journalFingerprint == "" &&
			execution.record.Phase() == retirement.PhaseFinalizing
	default:
		return false
	}
}

func (execution retirementExecution) advancesPhase() bool {
	return execution.start == retirementStartActive ||
		execution.start == retirementStartLegacy ||
		execution.start == retirementStartPrepared
}

func (execution retirementExecution) hasResidue() bool {
	return execution.start != retirementStartFinalizingWithoutResidue
}

func (execution retirementExecution) migratesLegacy() bool {
	return execution.start == retirementStartLegacy
}

type retirementBindings struct {
	active       *rootedpath.EntryAuthority
	activeRecord *rootedpath.EntryAuthority
	legacy       *rootedpath.EntryAuthority
	legacyRecord *rootedpath.EntryAuthority
	control      *rootedpath.EntryAuthority
	record       *rootedpath.EntryAuthority
	residue      *rootedpath.EntryAuthority
	garbage      *rootedpath.EntryAuthority
}

// RetireActiveJournal advances one clean active journal through the canonical
// retirement protocol. Root must be an open, caller-owned witness captured
// from the recovery directory used to build Plan. The function borrows Root
// and Filesystem and does not perform host or state recovery.
func RetireActiveJournal(
	ctx context.Context,
	plan recovery.Plan,
	activeAuthority ActiveJournalAuthority,
	root *rootedpath.CapturedRoot,
	filesystem mutationfs.RootedStore,
	stateCodec durable.SnapshotCodec,
) error {
	if filesystem == nil {
		return fmt.Errorf("journal retirement filesystem is required")
	}
	if root == nil {
		return fmt.Errorf("journal retirement root authority is required")
	}
	if err := activeAuthority.Validate(); err != nil {
		return err
	}
	if stateCodec == nil {
		return fmt.Errorf("journal retirement state codec is required")
	}
	if plan.Blocked() || plan.HasErrors() {
		return fmt.Errorf("active recovery plan is not clean enough to retire")
	}
	switch plan.Classification() {
	case recovery.ClassificationCleanBefore, recovery.ClassificationCleanAfter:
	default:
		return fmt.Errorf(
			"active recovery plan classification %q cannot be retired",
			plan.Classification(),
		)
	}

	fingerprint, err := plan.JournalAuthorityFingerprint()
	if err != nil {
		return fmt.Errorf("read active journal retirement identity: %w", err)
	}
	record, err := retirement.NewRecord(
		plan.OperationID(),
		fingerprint,
		retirement.PhasePrepared,
	)
	if err != nil {
		return fmt.Errorf("build active journal retirement record: %w", err)
	}
	rootAuthority, err := root.Authority()
	if err != nil {
		return fmt.Errorf("read journal retirement root authority: %w", err)
	}
	recoveryRoot := rootAuthority.PhysicalRoot()
	activePath := filepath.Join(recoveryRoot, plan.OperationID())
	if filepath.Clean(plan.OperationDir()) != filepath.Clean(activePath) {
		return fmt.Errorf("active recovery operation directory does not match its operation id")
	}

	execution := retirementExecution{
		record:             record,
		activePath:         activePath,
		activeAuthority:    activeAuthority,
		journalFingerprint: fingerprint,
		start:              retirementStartActive,
	}
	bindings, err := bindRetirement(
		root,
		execution,
	)
	if err != nil {
		return err
	}
	defer bindings.close()
	return executeRetirement(ctx, filesystem, stateCodec, execution, bindings)
}

// FinalizeJournalCleanup resumes only the exact cleanup phase selected by a
// cleanup-only plan. Root must be an open, caller-owned witness captured from
// the recovery directory used to build Plan. The function borrows Root and
// Filesystem and grants no host, statefile, or ownership authority.
func FinalizeJournalCleanup(
	ctx context.Context,
	plan retirement.CleanupPlan,
	legacyAuthority LegacyJournalAuthority,
	root *rootedpath.CapturedRoot,
	filesystem mutationfs.RootedStore,
	stateCodec durable.SnapshotCodec,
) error {
	if filesystem == nil {
		return fmt.Errorf("journal cleanup filesystem is required")
	}
	if root == nil {
		return fmt.Errorf("journal cleanup root authority is required")
	}
	authority := plan.Authority()
	record, err := authority.CurrentRecord()
	if err != nil {
		return err
	}
	if authority.RequiresLegacyMigration() {
		if err := legacyAuthority.Validate(); err != nil {
			return err
		}
		if stateCodec == nil {
			return fmt.Errorf("legacy journal migration state codec is required")
		}
	} else if legacyAuthority.valid() {
		return fmt.Errorf(
			"journal cleanup received unexpected legacy physical authority",
		)
	}

	start := retirementStartFinalizingWithoutResidue
	switch {
	case authority.RequiresLegacyMigration():
		start = retirementStartLegacy
	case authority.RequiresPhaseAdvance():
		start = retirementStartPrepared
	case authority.ResiduePresent():
		start = retirementStartFinalizingWithResidue
	}
	rootAuthority, err := root.Authority()
	if err != nil {
		return fmt.Errorf("read journal cleanup root authority: %w", err)
	}
	execution := retirementExecution{
		record: record,
		start:  start,
	}
	if authority.RequiresLegacyMigration() {
		execution.legacyPath = filepath.Join(
			rootAuthority.PhysicalRoot(),
			authority.LegacyTombstoneName(),
		)
		execution.legacyAuthority = legacyAuthority
		execution.journalFingerprint = authority.JournalAuthorityFingerprint()
	}
	bindings, err := bindRetirement(
		root,
		execution,
	)
	if err != nil {
		return err
	}
	defer bindings.close()
	return executeRetirement(ctx, filesystem, stateCodec, execution, bindings)
}

func bindRetirement(
	root *rootedpath.CapturedRoot,
	execution retirementExecution,
) (retirementBindings, error) {
	if !execution.valid() {
		return retirementBindings{}, fmt.Errorf("journal retirement execution is uninitialized")
	}
	authority, err := root.Authority()
	if err != nil {
		return retirementBindings{}, fmt.Errorf("read journal retirement root authority: %w", err)
	}
	recoveryRoot := authority.PhysicalRoot()
	identity := execution.record.Identity()
	paths := struct {
		active       string
		activeRecord string
		legacy       string
		legacyRecord string
		control      string
		record       string
		residue      string
		garbage      string
	}{
		active:       execution.activePath,
		activeRecord: filepath.Join(execution.activePath, recoveryJournalFileName),
		legacy:       execution.legacyPath,
		legacyRecord: filepath.Join(execution.legacyPath, recoveryJournalFileName),
		control:      filepath.Join(recoveryRoot, identity.ControlName()),
		record:       filepath.Join(recoveryRoot, identity.ControlName(), retirement.RecordFileName),
		residue:      filepath.Join(recoveryRoot, identity.ResidueName()),
		garbage:      filepath.Join(recoveryRoot, identity.GCName()),
	}

	var bindings retirementBindings
	bind := func(path string) (*rootedpath.EntryAuthority, error) {
		return rootedpath.BindSelectedEntryAuthority(root, recoveryRoot, path)
	}
	if paths.active != "" {
		bindings.active, err = bind(paths.active)
		if err != nil {
			return retirementBindings{}, fmt.Errorf("bind active recovery journal: %w", err)
		}
		bindings.activeRecord, err = bind(paths.activeRecord)
		if err != nil {
			return retirementBindings{}, errors.Join(
				fmt.Errorf("bind active recovery journal record: %w", err),
				bindings.close(),
			)
		}
	}
	if paths.legacy != "" {
		bindings.legacy, err = bind(paths.legacy)
		if err != nil {
			return retirementBindings{}, errors.Join(
				fmt.Errorf("bind legacy journal tombstone: %w", err),
				bindings.close(),
			)
		}
		bindings.legacyRecord, err = bind(paths.legacyRecord)
		if err != nil {
			return retirementBindings{}, errors.Join(
				fmt.Errorf("bind legacy journal tombstone record: %w", err),
				bindings.close(),
			)
		}
	}
	if bindings.control, err = bind(paths.control); err != nil {
		return retirementBindings{}, errors.Join(
			fmt.Errorf("bind journal retirement control: %w", err),
			bindings.close(),
		)
	}
	if bindings.record, err = bind(paths.record); err != nil {
		return retirementBindings{}, errors.Join(
			fmt.Errorf("bind journal retirement record: %w", err),
			bindings.close(),
		)
	}
	if bindings.residue, err = bind(paths.residue); err != nil {
		return retirementBindings{}, errors.Join(
			fmt.Errorf("bind journal retirement residue: %w", err),
			bindings.close(),
		)
	}
	if bindings.garbage, err = bind(paths.garbage); err != nil {
		return retirementBindings{}, errors.Join(
			fmt.Errorf("bind journal retirement GC residue: %w", err),
			bindings.close(),
		)
	}
	return bindings, nil
}

func executeRetirement(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	stateCodec durable.SnapshotCodec,
	execution retirementExecution,
	bindings retirementBindings,
) error {
	if ctx == nil {
		return fmt.Errorf("journal retirement context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !execution.valid() {
		return fmt.Errorf("journal retirement execution is uninitialized")
	}
	if execution.start == retirementStartActive {
		activeCapability, activeIdentity, err := captureRetirementEntry(
			ctx,
			filesystem,
			bindings.active,
			"active recovery journal",
		)
		if err != nil {
			return err
		}
		if !execution.activeAuthority.matches(activeIdentity) {
			return errors.Join(
				fmt.Errorf("active recovery journal identity changed before retirement"),
				activeCapability.Close(),
			)
		}
		activeCapabilityOpen := true
		defer func() {
			if activeCapabilityOpen {
				_ = activeCapability.Close()
			}
		}()
		if err := ensurePreparedControl(ctx, filesystem, execution.record, bindings); err != nil {
			return err
		}
		if err := requireJournalFingerprint(
			ctx,
			filesystem,
			bindings.activeRecord,
			execution.journalFingerprint,
			stateCodec,
			"active recovery journal",
		); err != nil {
			return err
		}
		activeCapabilityOpen = false
		if err := renameCapturedRetirementEntry(
			ctx,
			filesystem,
			activeCapability,
			activeIdentity,
			execution.record.Identity().ResidueName(),
			"active recovery journal",
		); err != nil {
			return err
		}
	}
	if execution.migratesLegacy() {
		legacyCapability, legacyIdentity, err := captureRetirementEntry(
			ctx,
			filesystem,
			bindings.legacy,
			"legacy journal tombstone",
		)
		if err != nil {
			return err
		}
		if !execution.legacyAuthority.matches(legacyIdentity) {
			return errors.Join(
				fmt.Errorf(
					"legacy journal tombstone identity changed before migration",
				),
				legacyCapability.Close(),
			)
		}
		legacyCapabilityOpen := true
		defer func() {
			if legacyCapabilityOpen {
				_ = legacyCapability.Close()
			}
		}()
		if err := requireRetirementResidueTree(
			ctx,
			filesystem,
			bindings.legacy,
		); err != nil {
			return fmt.Errorf("verify legacy journal tombstone before migration: %w", err)
		}
		if err := ensurePreparedControl(ctx, filesystem, execution.record, bindings); err != nil {
			return err
		}
		if err := requireJournalFingerprint(
			ctx,
			filesystem,
			bindings.legacyRecord,
			execution.journalFingerprint,
			stateCodec,
			"legacy journal tombstone",
		); err != nil {
			return err
		}
		legacyCapabilityOpen = false
		if err := renameCapturedRetirementEntry(
			ctx,
			filesystem,
			legacyCapability,
			legacyIdentity,
			execution.record.Identity().ResidueName(),
			"legacy journal tombstone",
		); err != nil {
			return err
		}
	}
	if execution.advancesPhase() {
		if err := requireRetirementControl(
			ctx,
			filesystem,
			bindings.control,
			execution.record,
		); err != nil {
			return fmt.Errorf("verify prepared journal retirement control: %w", err)
		}
		if err := requireRetirementResidueTree(
			ctx,
			filesystem,
			bindings.residue,
		); err != nil {
			return fmt.Errorf("verify journal retirement residue before finalizing: %w", err)
		}
		if err := advanceRetirementRecord(ctx, filesystem, execution.record, bindings.record); err != nil {
			return err
		}
	}
	finalizing, err := execution.record.Finalizing()
	if err != nil {
		return fmt.Errorf("derive finalizing journal retirement record: %w", err)
	}
	if err := requireRetirementControl(
		ctx,
		filesystem,
		bindings.control,
		finalizing,
	); err != nil {
		return fmt.Errorf("verify finalizing journal retirement control: %w", err)
	}
	if execution.hasResidue() {
		if err := cleanupRetirementEntry(
			ctx,
			filesystem,
			bindings.residue,
			"journal retirement residue",
		); err != nil {
			return err
		}
	}
	if err := renameRetirementControl(
		ctx,
		filesystem,
		bindings.control,
		execution.record.Identity().GCName(),
		"journal retirement control",
	); err != nil {
		return err
	}
	if err := cleanupRetirementEntry(
		ctx,
		filesystem,
		bindings.garbage,
		"journal retirement GC residue",
	); err != nil {
		return finalizedWithGCResidue(err)
	}
	return nil
}

func ensurePreparedControl(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	record retirement.Record,
	bindings retirementBindings,
) error {
	capability, err := bindings.control.Acquire()
	if err != nil {
		return fmt.Errorf("acquire journal retirement control: %w", err)
	}
	_, err = filesystem.CaptureRootedEntryIdentity(ctx, capability)
	switch {
	case err == nil:
		if closeErr := capability.Close(); closeErr != nil {
			return fmt.Errorf("close journal retirement control capability: %w", closeErr)
		}
		if err := requireRetirementControl(
			ctx,
			filesystem,
			bindings.control,
			record,
		); err != nil {
			return fmt.Errorf("validate prepared journal retirement control: %w", err)
		}
		return nil
	case errors.Is(err, os.ErrNotExist):
	default:
		return errors.Join(
			fmt.Errorf("inspect journal retirement control: %w", err),
			capability.Close(),
		)
	}

	content, err := retirement.Encode(record)
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	recordPath, err := mutationfs.NewTreeRelativePath(retirement.RecordFileName)
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	prepared, err := filesystem.PrepareRootedTree(
		ctx,
		capability,
		func(writer mutationfs.RootedTreeWriter) error {
			if err := writer.SetRootMode(retirement.DirectoryMode); err != nil {
				return err
			}
			return writer.WriteFile(
				recordPath,
				retirement.RecordMode,
				bytes.NewReader(content),
			)
		},
	)
	if err != nil {
		return fmt.Errorf("prepare journal retirement control: %w", err)
	}
	outcome, err := prepared.CommitWithOutcome(ctx)
	if err != nil {
		return fmt.Errorf(
			"publish journal retirement control (%s): %w",
			commitOutcomeDetail(outcome),
			err,
		)
	}
	return nil
}

func advanceRetirementRecord(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	expected retirement.Record,
	authority *rootedpath.EntryAuthority,
) error {
	finalizing, err := expected.Finalizing()
	if err != nil {
		return fmt.Errorf("derive finalizing journal retirement record: %w", err)
	}
	capability, identity, err := readRetirementRecord(
		ctx,
		filesystem,
		authority,
		expected,
	)
	if err != nil {
		return err
	}
	content, err := retirement.Encode(finalizing)
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	outcome, err := filesystem.ReplaceRootedFileWithOutcome(
		ctx,
		capability,
		content,
		retirement.RecordMode,
		identity,
	)
	if err != nil {
		return fmt.Errorf(
			"advance journal retirement record (%s): %w",
			commitOutcomeDetail(outcome),
			err,
		)
	}
	return nil
}

func renameRetirementEntry(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	destinationName string,
	label string,
) error {
	capability, identity, err := captureRetirementEntry(
		ctx,
		filesystem,
		authority,
		label,
	)
	if err != nil {
		return err
	}
	return renameCapturedRetirementEntry(
		ctx,
		filesystem,
		capability,
		identity,
		destinationName,
		label,
	)
}

func renameRetirementControl(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	control *rootedpath.EntryAuthority,
	destinationName string,
	label string,
) error {
	capability, identity, err := captureRetirementEntry(
		ctx,
		filesystem,
		control,
		label,
	)
	if err != nil {
		return err
	}
	outcome, err := filesystem.RenameRootedEntry(
		ctx,
		capability,
		destinationName,
		identity,
	)
	if err == nil {
		return nil
	}
	failure := fmt.Errorf(
		"retire %s (%s): %w",
		label,
		commitOutcomeDetail(outcome),
		err,
	)
	if outcome.State() == mutationfs.CommitOutcomeComplete {
		return finalizedWithGCResidue(failure)
	}
	return failure
}

func captureRetirementEntry(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	label string,
) (rootedpath.CommitCapability, mutationfs.EntryIdentity, error) {
	if authority == nil {
		return nil, nil, fmt.Errorf("%s authority is required", label)
	}
	capability, err := authority.Acquire()
	if err != nil {
		return nil, nil, fmt.Errorf("acquire %s: %w", label, err)
	}
	identity, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("capture %s identity: %w", label, err),
			capability.Close(),
		)
	}
	return capability, identity, nil
}

func renameCapturedRetirementEntry(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	capability rootedpath.CommitCapability,
	identity mutationfs.EntryIdentity,
	destinationName string,
	label string,
) error {
	outcome, err := filesystem.RenameRootedEntry(
		ctx,
		capability,
		destinationName,
		identity,
	)
	if err != nil {
		return fmt.Errorf(
			"retire %s (%s): %w",
			label,
			commitOutcomeDetail(outcome),
			err,
		)
	}
	return nil
}

func cleanupRetirementEntry(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	label string,
) error {
	if authority == nil {
		return fmt.Errorf("%s authority is required", label)
	}
	capability, err := authority.Acquire()
	if err != nil {
		return fmt.Errorf("acquire %s: %w", label, err)
	}
	identity, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return errors.Join(fmt.Errorf("capture %s identity: %w", label, err), capability.Close())
	}
	outcome, err := filesystem.CleanupRootedEntry(ctx, capability, identity)
	if err != nil {
		return fmt.Errorf(
			"clean %s (%s): %w",
			label,
			commitOutcomeDetail(outcome),
			err,
		)
	}
	return nil
}

func commitOutcomeDetail(outcome mutationfs.CommitOutcome) string {
	return string(outcome.State())
}

func (bindings *retirementBindings) close() error {
	if bindings == nil {
		return nil
	}
	var err error
	for _, authority := range []*rootedpath.EntryAuthority{
		bindings.active,
		bindings.activeRecord,
		bindings.legacy,
		bindings.legacyRecord,
		bindings.control,
		bindings.record,
		bindings.residue,
		bindings.garbage,
	} {
		if authority != nil {
			err = errors.Join(err, authority.Close())
		}
	}
	*bindings = retirementBindings{}
	return err
}
