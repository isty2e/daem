package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
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

// NativeRecoveryAssessment retains a complete native-only phase observation.
// It does not supply leases or permission to change host or ownership state.
type NativeRecoveryAssessment struct {
	captured   CapturedNativeJournal
	assessment nativeDocumentAssessment
	valid      bool
}

func (captured CapturedNativeJournal) AssessRecovery(ctx context.Context, options NativeRecoveryOptions) (result NativeRecoveryAssessment, resultErr error) {
	if err := captured.Validate(); err != nil {
		return NativeRecoveryAssessment{}, err
	}
	if len(captured.journal.record.Entries)+len(captured.journal.record.ClaimTransitions)+len(captured.journal.record.ProvisionalAcquires)+len(captured.journal.record.RemovalIntents) != 0 {
		return NativeRecoveryAssessment{}, fmt.Errorf("mixed native recovery requires ordinary phase integration")
	}
	if err := captured.ValidateActive(ctx, options.Filesystem, options.OperationAuthority, captured.budget); err != nil {
		return NativeRecoveryAssessment{}, err
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(captured.capture.Directory, recovery.MaximumPhysicalPathDepth, captured.budget)
	if err != nil {
		return NativeRecoveryAssessment{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close(), ctx.Err())
		if resultErr != nil {
			result = NativeRecoveryAssessment{}
		}
	}()
	assessment, err := captured.journal.assessDocumentRecovery(ctx, nativeDocumentRecoveryReadOptions{
		documents: nativeDocumentReadOptions{
			artifacts: root, filesystem: options.Filesystem, budget: captured.budget,
			limits: captured.qualified.records.groups[0].comparison.limits, codecs: options.Codecs,
		},
		roots: captured.qualified.namespaces[0].roots, state: options.State, registry: options.Registry,
	})
	if err != nil {
		return NativeRecoveryAssessment{}, err
	}
	if err := captured.ValidateActive(ctx, options.Filesystem, options.OperationAuthority, captured.budget); err != nil {
		return NativeRecoveryAssessment{}, err
	}
	return NativeRecoveryAssessment{captured: captured, assessment: assessment, valid: true}, nil
}

func (assessment NativeRecoveryAssessment) CleanBefore() bool {
	return assessment.valid && assessment.assessment.disposition == nativeDocumentsBefore
}

func (assessment NativeRecoveryAssessment) CleanAfter() bool {
	return assessment.valid && assessment.assessment.disposition == nativeDocumentsAfter
}

func (assessment NativeRecoveryAssessment) RequiresRollback() bool {
	return assessment.valid && assessment.assessment.disposition == nativeDocumentsRollback
}

func (assessment NativeRecoveryAssessment) RequiresFinalization() bool {
	return assessment.valid && assessment.assessment.disposition == nativeDocumentsFinalize
}

func (assessment NativeRecoveryAssessment) HostMatchesExpected() bool {
	if !assessment.valid {
		return false
	}
	for _, stage := range assessment.assessment.stages {
		if !stage.expected {
			return false
		}
	}
	return true
}

func (assessment NativeRecoveryAssessment) RollbackClaims() ownership.ClaimConvergence {
	return assessment.assessment.rollback
}

func (assessment NativeRecoveryAssessment) FinalizeClaims() ownership.ClaimConvergence {
	return assessment.assessment.finalize
}

func (assessment NativeRecoveryAssessment) FinalizeBindings() ownership.DocumentBindingConvergence {
	return assessment.assessment.bindings
}

// NativeJournalRetirement owns the existing bounded retirement mechanism, but
// accepts only a clean native assessment of its exact published record.
type NativeJournalRetirement struct {
	captured CapturedNativeJournal
	prepared *RetirementContinuation
}

func PrepareNativeJournalRetirement(ctx context.Context, captured CapturedNativeJournal, filesystem mutationfs.RootedStore, stateCodec durable.SnapshotCodec) (*NativeJournalRetirement, error) {
	if err := captured.Validate(); err != nil {
		return nil, err
	}
	if stateCodec == nil {
		return nil, fmt.Errorf("native retirement requires its selected state codec")
	}
	record, err := retirement.NewRecord(captured.capture.OperationID, captured.capture.RecordFingerprint, retirement.PhasePrepared)
	if err != nil {
		return nil, err
	}
	prepared, err := prepareRetirement(ctx, retirementExecution{
		record: record, activePath: captured.capture.Directory, activeAuthority: captured.active,
		journalFingerprint: captured.capture.RecordFingerprint, start: retirementStartActive,
	}, filepath.Dir(captured.capture.Directory), recovery.MaximumPhysicalPathDepth, captured.budget, filesystem, stateCodec)
	if err != nil {
		return nil, err
	}
	if !journalFingerprintMatchesEvidence(captured.capture.RecordFingerprint, prepared.evidence.active) {
		return nil, errors.Join(fmt.Errorf("native retirement record changed during reservation"), prepared.Close())
	}
	prepared.journalDecoder = decodeNativeInventoryJournal
	return &NativeJournalRetirement{captured: captured, prepared: prepared}, nil
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
		if assessment.captured.capture != retiring.captured.capture || !assessment.captured.active.equal(retiring.captured.active) {
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
