package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal/recovery"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type NativeRecoveryOptions struct {
	Filesystem         mutationfs.RootedStore
	OperationAuthority *rootedpath.EntryAuthority
	State              durable.SnapshotReader
	Registry           ownershipmutation.RegistryReader
	Codecs             aggregate.CodecCatalog
}

// NativeRecoveryAssessment retains the complete operation's observed phase.
// It does not supply leases or permission to change host or ownership state.
type NativeRecoveryAssessment struct {
	capture    CaptureResult
	active     ActiveJournalAuthority
	assessment journalqualification.Assessment
	valid      bool
}

func (captured CapturedNativeJournal) AssessRecovery(ctx context.Context, options NativeRecoveryOptions) (NativeRecoveryAssessment, error) {
	if err := captured.Validate(); err != nil {
		return NativeRecoveryAssessment{}, err
	}
	return assessNativeRecoveryBasis(ctx, nativeRecoveryBasis{
		capture: captured.capture, active: captured.active, journal: captured.journal,
		roots: captured.publication.roots, limits: captured.publication.limits, budget: captured.budget,
		resolver: captured.publication.resolver, ordinaryCapabilities: captured.publication.ordinaryCapabilities,
	}, options)
}

func (assessment NativeRecoveryAssessment) CleanBefore() bool {
	return assessment.valid && assessment.assessment.Facts().Classification() == recovery.ClassificationCleanBefore
}

func (assessment NativeRecoveryAssessment) CleanAfter() bool {
	return assessment.valid && assessment.assessment.Facts().Classification() == recovery.ClassificationCleanAfter
}

func (assessment NativeRecoveryAssessment) RequiresRollback() bool {
	return assessment.valid && assessment.assessment.Facts().Classification() == recovery.ClassificationNeedsRollback
}

func (assessment NativeRecoveryAssessment) RequiresFinalization() bool {
	return assessment.valid && assessment.assessment.Facts().Classification() == recovery.ClassificationNeedsFinalize
}

func (assessment NativeRecoveryAssessment) HostMatchesExpected() bool {
	return assessment.valid && assessment.assessment.Facts().HostMatchesExpected()
}

func (assessment NativeRecoveryAssessment) RollbackClaims() ownership.ClaimConvergence {
	return assessment.assessment.Facts().RollbackClaims()
}

func (assessment NativeRecoveryAssessment) FinalizeClaims() ownership.ClaimConvergence {
	return assessment.assessment.Facts().FinalizeClaims()
}

func (assessment NativeRecoveryAssessment) FinalizeBindings() ownership.DocumentBindingConvergence {
	return assessment.assessment.Facts().FinalizeBindings()
}

func (assessment NativeRecoveryAssessment) ObservedPersistence() (durable.Snapshot, ownership.Registry, bool) {
	state, registry := assessment.assessment.Facts().Persistence()
	return state, registry, assessment.valid
}

// NativeJournalRetirement owns the existing bounded retirement mechanism, but
// accepts only a clean native assessment of its exact published record.
type NativeJournalRetirement struct {
	capture  CaptureResult
	active   ActiveJournalAuthority
	prepared *RetirementContinuation
	budget   *physicalwork.Budget
}

func PrepareNativeJournalRetirement(ctx context.Context, captured CapturedNativeJournal, filesystem mutationfs.RootedStore, stateCodec durable.SnapshotCodec) (*NativeJournalRetirement, error) {
	if err := captured.Validate(); err != nil {
		return nil, err
	}
	return prepareNativeRetirement(ctx, captured.capture, captured.active, captured.budget, filesystem, stateCodec)
}

func prepareNativeRetirement(ctx context.Context, capture CaptureResult, active ActiveJournalAuthority, budget *physicalwork.Budget, filesystem mutationfs.RootedStore, stateCodec durable.SnapshotCodec) (*NativeJournalRetirement, error) {
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
	return &NativeJournalRetirement{capture: capture, active: active, prepared: prepared, budget: budget}, nil
}

func (retiring *NativeJournalRetirement) ExecuteWithGate(ctx context.Context, assessment NativeRecoveryAssessment, gate RetirementStepGate) error {
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
		if assessment.capture != retiring.capture || !assessment.active.Equal(retiring.active) {
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
