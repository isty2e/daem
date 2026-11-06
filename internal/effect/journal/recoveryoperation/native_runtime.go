package recoveryoperation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

// NativeJournalRetirement owns the existing bounded retirement mechanism, but
// accepts only a clean native assessment of its exact published record.
type NativeJournalRetirement struct {
	identity  retirement.Identity
	directory string
	active    journalinventory.ActiveJournalAuthority
	prepared  *RetirementContinuation
	budget    *physicalwork.Budget
}

func PrepareNativeJournalRetirement(ctx context.Context, captured journalpublication.CapturedNativeJournal, budget *physicalwork.Budget, filesystem mutationfs.RootedStore, stateCodec durable.SnapshotCodec) (*NativeJournalRetirement, error) {
	if err := captured.RequireAllocation(budget); err != nil {
		return nil, err
	}
	return prepareNativeRetirement(ctx, captured.CaptureResult(), captured.ActiveJournalAuthority(), budget, filesystem, stateCodec)
}

func prepareNativeRetirement(ctx context.Context, capture journalpublication.CaptureResult, active journalinventory.ActiveJournalAuthority, budget *physicalwork.Budget, filesystem mutationfs.RootedStore, stateCodec durable.SnapshotCodec) (*NativeJournalRetirement, error) {
	if stateCodec == nil {
		return nil, fmt.Errorf("native retirement requires its selected state codec")
	}
	record, err := retirement.NewRecord(capture.OperationID, capture.RecordFingerprint, retirement.PhasePrepared)
	if err != nil {
		return nil, err
	}
	prepared, err := prepareRetirement(ctx, retirementExecution{
		record: record, activePath: capture.Directory, activeAuthority: active, journalFingerprint: capture.RecordFingerprint, start: retirementStartActive,
	}, filepath.Dir(capture.Directory), physicalwork.MaximumPhysicalPathDepth, budget, filesystem, stateCodec)
	if err != nil {
		return nil, err
	}
	if !journalFingerprintMatchesEvidence(capture.RecordFingerprint, prepared.evidence.active) {
		return nil, errors.Join(fmt.Errorf("native retirement record changed during reservation"), prepared.Close())
	}
	prepared.journalDecoder = journalrecord.DecodeNativeSnapshot
	return &NativeJournalRetirement{identity: record.Identity(), directory: capture.Directory, active: active, prepared: prepared, budget: budget}, nil
}

func (retiring *NativeJournalRetirement) ExecuteWithGate(ctx context.Context, assessment journalqualification.NativeRecoveryAssessment, gate RetirementStepGate) error {
	if ctx == nil || retiring == nil || retiring.prepared == nil {
		return fmt.Errorf("native journal retirement is required")
	}
	prepared := retiring.prepared
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.closed || prepared.consumed {
		return fmt.Errorf("native journal retirement was already consumed")
	}
	if err := executeRetirementStep(gate, RetirementStepValidateActivePlan, func() error {
		if !assessment.CleanBefore() && !assessment.CleanAfter() {
			return fmt.Errorf("native journal is not clean enough to retire")
		}
		if !assessment.MatchesRevision(retiring.identity, retiring.directory, retiring.active) {
			return fmt.Errorf("native retirement assessment belongs to another published record")
		}
		return ctx.Err()
	}); err != nil {
		return err
	}
	prepared.consumed = true
	return executePreparedRetirement(ctx, prepared, gate)
}

func (retiring *NativeJournalRetirement) Close() error {
	if retiring == nil || retiring.prepared == nil {
		return nil
	}
	return retiring.prepared.Close()
}
