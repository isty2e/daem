package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

type nativeRecoveryBasis struct {
	capture              CaptureResult
	active               ActiveJournalAuthority
	journal              journalrecord.Native
	roots                journalnamespace.Roots
	limits               configdocument.Limits
	budget               *physicalwork.Budget
	resolver             func(output.Destination) (string, error)
	ordinaryCapabilities RootedCapabilityResolver
}

func (operation *NativeRecoveryOperation) InitialAssessment() NativeRecoveryAssessment {
	if operation == nil || operation.closed {
		return NativeRecoveryAssessment{}
	}
	return NativeRecoveryAssessment{capture: operation.plan.capture, active: operation.plan.active, assessment: operation.plan.assessment, valid: true}
}

func (operation *NativeRecoveryOperation) ObservedState() durable.Snapshot {
	return operation.plan.assessment.state
}

func (operation *NativeRecoveryOperation) ObservedRegistry() ownership.Registry {
	return operation.plan.assessment.registry
}

func (operation *NativeRecoveryOperation) basis() nativeRecoveryBasis {
	return nativeRecoveryBasis{capture: operation.plan.capture, active: operation.plan.active, journal: operation.plan.journal, roots: operation.roots, limits: operation.profile.Limits, budget: operation.profile.Budget, resolver: operation.resolver, ordinaryCapabilities: operation.ordinaryCapabilities}
}

func (operation *NativeRecoveryOperation) Assess(ctx context.Context, options NativeRecoveryOptions) (NativeRecoveryAssessment, error) {
	if operation == nil || operation.closed {
		return NativeRecoveryAssessment{}, fmt.Errorf("live native recovery operation is required")
	}
	return assessNativeRecoveryBasis(ctx, operation.basis(), options)
}

func (operation *NativeRecoveryOperation) PrepareRetirement(ctx context.Context, filesystem mutationfs.RootedStore) (*NativeJournalRetirement, error) {
	if operation == nil || operation.closed {
		return nil, fmt.Errorf("live native recovery operation is required")
	}
	retiring, err := prepareNativeRetirement(ctx, operation.plan.capture, operation.plan.active, operation.profile.Budget, filesystem, operation.profile.StateCodec)
	if err == nil && operation.inventoryProfile.decoder != nil {
		retiring.prepared.journalDecoder = operation.inventoryProfile.decoder
	}
	return retiring, err
}

func assessNativeRecoveryBasis(ctx context.Context, basis nativeRecoveryBasis, options NativeRecoveryOptions) (result NativeRecoveryAssessment, resultErr error) {
	if err := validateNativeActive(ctx, basis.capture, basis.active, options.Filesystem, options.OperationAuthority, basis.budget); err != nil {
		return NativeRecoveryAssessment{}, err
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(basis.capture.Directory, physicalwork.MaximumPhysicalPathDepth, basis.budget)
	if err != nil {
		return NativeRecoveryAssessment{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close(), ctx.Err())
		if resultErr != nil {
			result = NativeRecoveryAssessment{}
		}
	}()
	assessment, err := assessDocumentRecovery(basis.journal, ctx, nativeDocumentRecoveryReadOptions{
		documents: nativeDocumentReadOptions{artifacts: root, filesystem: options.Filesystem, budget: basis.budget, limits: basis.limits, codecs: options.Codecs, ordinaryCapabilities: basis.ordinaryCapabilities},
		roots:     basis.roots, state: options.State, registry: options.Registry,
		directory: basis.capture.Directory, active: basis.active, resolver: basis.resolver,
	})
	if err != nil {
		return NativeRecoveryAssessment{}, err
	}
	if err := validateNativeActive(ctx, basis.capture, basis.active, options.Filesystem, options.OperationAuthority, basis.budget); err != nil {
		return NativeRecoveryAssessment{}, err
	}
	return NativeRecoveryAssessment{capture: basis.capture, active: basis.active, assessment: assessment, valid: true}, nil
}

func validateNativeActive(ctx context.Context, capture CaptureResult, active ActiveJournalAuthority, filesystem mutationfs.RootedReader, entry *rootedpath.EntryAuthority, budget *physicalwork.Budget) (resultErr error) {
	if ctx == nil || filesystem == nil || entry == nil || budget == nil {
		return fmt.Errorf("native active validation requires selected authority, filesystem and the original allocation")
	}
	if err := active.Validate(); err != nil {
		return err
	}
	capability, err := entry.Acquire()
	if err != nil {
		return err
	}
	pathErr := validateOperationCapabilityPath(capability, capture.Directory)
	identity, identityErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err := errors.Join(pathErr, identityErr, capability.Close()); err != nil {
		return err
	}
	if !active.matches(identity) {
		return fmt.Errorf("published native journal directory identity changed")
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(capture.Directory, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close(), ctx.Err()) }()
	if err := ValidateActiveJournalRoot(ctx, filesystem, root, budget, active); err != nil {
		return err
	}
	snapshot, err := readNativeCaptureRecord(ctx, root, filesystem, budget)
	if err != nil {
		return err
	}
	if snapshot.Mode().Perm() != journalrecord.Mode || journalrecord.Fingerprint(snapshot.Content()) != capture.RecordFingerprint {
		return fmt.Errorf("published native journal record revision changed")
	}
	return ValidateActiveJournalRoot(ctx, filesystem, root, budget, active)
}
