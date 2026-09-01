package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type retirementStart uint8

const (
	retirementStartInvalid retirementStart = iota
	retirementStartActive
	retirementStartPrepared
	retirementStartFinalizingWithResidue
	retirementStartFinalizingWithoutResidue
)

type retirementExecution struct {
	record             retirement.Record
	activePath         string
	activeAuthority    ActiveJournalAuthority
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
			execution.journalFingerprint != "" &&
			execution.record.Phase() == retirement.PhasePrepared
	case retirementStartPrepared:
		return execution.activePath == "" &&
			!execution.activeAuthority.valid() &&
			execution.journalFingerprint == "" &&
			execution.record.Phase() == retirement.PhasePrepared
	case retirementStartFinalizingWithResidue,
		retirementStartFinalizingWithoutResidue:
		return execution.activePath == "" &&
			!execution.activeAuthority.valid() &&
			execution.journalFingerprint == "" &&
			execution.record.Phase() == retirement.PhaseFinalizing
	default:
		return false
	}
}

func (execution retirementExecution) advancesPhase() bool {
	return execution.start == retirementStartActive ||
		execution.start == retirementStartPrepared
}

func (execution retirementExecution) hasResidue() bool {
	return execution.start != retirementStartFinalizingWithoutResidue
}

type retirementBindings struct {
	active       *rootedpath.EntryAuthority
	activeRecord *rootedpath.EntryAuthority
	control      *rootedpath.EntryAuthority
	record       *rootedpath.EntryAuthority
	residue      *rootedpath.EntryAuthority
	garbage      *rootedpath.EntryAuthority
}

func executePreparedRetirement(
	ctx context.Context,
	prepared *RetirementContinuation,
	gate RetirementStepGate,
) error {
	if ctx == nil {
		return fmt.Errorf("journal retirement context is required")
	}
	if prepared == nil || prepared.retirementContinuationState == nil || !prepared.execution.valid() {
		return fmt.Errorf("journal retirement execution is uninitialized")
	}
	execution := prepared.execution
	bindings := prepared.bindings
	filesystem := prepared.filesystem
	evidence := prepared.evidence
	finalizing, err := execution.record.Finalizing()
	if err != nil {
		return fmt.Errorf("derive finalizing journal retirement record: %w", err)
	}
	if err := executeRetirementStep(
		gate,
		RetirementStepValidatePreparedLayout,
		func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return requirePreparedRetirementLayout(ctx, prepared)
		},
	); err != nil {
		return err
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
		if err := ensurePreparedControl(
			ctx,
			filesystem,
			execution.record,
			bindings,
			evidence.controlPresent,
		); err != nil {
			return err
		}
		if err := requireJournalFingerprint(
			ctx,
			filesystem,
			bindings.activeRecord,
			execution.journalFingerprint,
			prepared.stateCodec,
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
	if execution.advancesPhase() {
		if err := executeRetirementStep(
			gate,
			RetirementStepValidatePhaseAdvanceLayout,
			func() error {
				if err := requireRetirementControl(
					ctx,
					filesystem,
					bindings.control,
					execution.record,
					evidence.controlCurrentLimits,
				); err != nil {
					return fmt.Errorf("verify prepared journal retirement control: %w", err)
				}
				residueEvidence := evidence.residue
				if execution.start == retirementStartActive {
					residueEvidence = evidence.active
				}
				if err := requireRetirementTreeEvidence(
					ctx,
					filesystem,
					bindings.residue,
					residueEvidence,
					residueLimitsForExecution(evidence, execution),
					false,
					"journal retirement residue",
				); err != nil {
					return fmt.Errorf("verify journal retirement residue before finalizing: %w", err)
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := executeRetirementStep(
			gate,
			RetirementStepAdvanceRecord,
			func() error {
				return advanceRetirementRecord(ctx, filesystem, execution.record, bindings.record)
			},
		); err != nil {
			return err
		}
	}
	if err := executeRetirementStep(
		gate,
		RetirementStepValidateFinalizingLayout,
		func() error {
			if err := requireRetirementControl(
				ctx,
				filesystem,
				bindings.control,
				finalizing,
				evidence.controlFinalLimits,
			); err != nil {
				return fmt.Errorf("verify finalizing journal retirement control: %w", err)
			}
			if execution.hasResidue() {
				residueEvidence := evidence.residue
				if execution.start == retirementStartActive {
					residueEvidence = evidence.active
				}
				return requireRetirementTreeEvidence(
					ctx,
					filesystem,
					bindings.residue,
					residueEvidence,
					residueLimitsForExecution(evidence, execution),
					false,
					"journal retirement residue",
				)
			}
			return requireRetirementEntryAbsent(
				ctx,
				filesystem,
				bindings.residue,
				"journal retirement residue",
			)
		},
	); err != nil {
		return err
	}
	if execution.hasResidue() {
		residueLimits := residueLimitsForExecution(evidence, execution)
		if err := executeRetirementStep(
			gate,
			RetirementStepCleanupResidue,
			func() error {
				return cleanupRetirementEntry(
					ctx,
					filesystem,
					bindings.residue,
					"journal retirement residue",
					residueLimits,
				)
			},
		); err != nil {
			return err
		}
	}
	if err := executeRetirementStep(
		gate,
		RetirementStepRetireControl,
		func() error {
			return renameRetirementControl(
				ctx,
				filesystem,
				bindings.control,
				execution.record.Identity().GCName(),
				"journal retirement control",
			)
		},
	); err != nil {
		return err
	}
	if err := executeRetirementStep(
		gate,
		RetirementStepCleanupGarbage,
		func() error {
			return cleanupRetirementEntry(
				ctx,
				filesystem,
				bindings.garbage,
				"journal retirement GC residue",
				evidence.controlFinalLimits,
			)
		},
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
	present bool,
) error {
	if present {
		return nil
	}
	capability, err := bindings.control.Acquire()
	if err != nil {
		return fmt.Errorf("acquire journal retirement control: %w", err)
	}
	_, err = filesystem.CaptureRootedEntryIdentity(ctx, capability)
	switch {
	case err == nil:
		return errors.Join(
			fmt.Errorf("journal retirement control appeared after preparation"),
			capability.Close(),
		)
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
	limits mutationfs.TreeTraversalLimits,
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
	outcome, err := filesystem.CleanupRootedEntry(
		ctx,
		capability,
		identity,
		limits,
	)
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

func requirePreparedRetirementLayout(
	ctx context.Context,
	prepared *RetirementContinuation,
) error {
	execution := prepared.execution
	evidence := prepared.evidence
	bindings := prepared.bindings
	filesystem := prepared.filesystem

	if execution.start == retirementStartActive {
		if err := requireRetirementTreeEvidence(
			ctx,
			filesystem,
			bindings.active,
			evidence.active,
			evidence.activeLimits,
			true,
			"active recovery journal",
		); err != nil {
			return err
		}
	}
	if evidence.controlPresent {
		current, err := observeRetirementControl(
			ctx,
			filesystem,
			bindings.control,
			execution.record,
			evidence.controlCurrentLimits,
		)
		if err != nil {
			return fmt.Errorf("verify prepared journal retirement control: %w", err)
		}
		if !evidence.control.sameTree(current) ||
			!evidence.control.identity.Equal(current.identity) {
			return fmt.Errorf("journal retirement control changed after preparation")
		}
	} else if err := requireRetirementEntryAbsent(
		ctx,
		filesystem,
		bindings.control,
		"journal retirement control",
	); err != nil {
		return err
	}
	if evidence.residuePresent {
		if err := requireRetirementTreeEvidence(
			ctx,
			filesystem,
			bindings.residue,
			evidence.residue,
			evidence.residueLimits,
			true,
			"journal retirement residue",
		); err != nil {
			return err
		}
	} else if err := requireRetirementEntryAbsent(
		ctx,
		filesystem,
		bindings.residue,
		"journal retirement residue",
	); err != nil {
		return err
	}
	return requireRetirementEntryAbsent(
		ctx,
		filesystem,
		bindings.garbage,
		"journal retirement GC residue",
	)
}

func requireRetirementTreeEvidence(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	expected retirementTreeEvidence,
	limits mutationfs.TreeTraversalLimits,
	requireIdentity bool,
	label string,
) error {
	if !expected.valid() {
		return fmt.Errorf("%s evidence is unavailable", label)
	}
	capability, err := authority.Acquire()
	if err != nil {
		return err
	}
	sink := newRetirementTreeSnapshotSink()
	identity, snapshotErr := filesystem.SnapshotRootedDirectory(
		ctx,
		capability,
		limits,
		sink,
	)
	current, evidenceErr := sink.evidence(identity)
	closeErr := capability.Close()
	if snapshotErr != nil || evidenceErr != nil || closeErr != nil {
		return errors.Join(snapshotErr, evidenceErr, closeErr)
	}
	if !expected.sameTree(current) ||
		(requireIdentity && !expected.identity.Equal(current.identity)) {
		return fmt.Errorf("%s changed after retirement preparation", label)
	}
	return nil
}

func residueLimitsForExecution(
	evidence retirementExecutionEvidence,
	execution retirementExecution,
) mutationfs.TreeTraversalLimits {
	if execution.start == retirementStartActive {
		return evidence.activeLimits
	}
	return evidence.residueLimits
}

func requireRetirementEntryAbsent(
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
		return fmt.Errorf("acquire %s absence authority: %w", label, err)
	}
	_, observeErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	closeErr := capability.Close()
	if errors.Is(observeErr, os.ErrNotExist) {
		return closeErr
	}
	if observeErr != nil {
		return errors.Join(fmt.Errorf("observe %s absence: %w", label, observeErr), closeErr)
	}
	return errors.Join(fmt.Errorf("%s appeared after cleanup planning", label), closeErr)
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
