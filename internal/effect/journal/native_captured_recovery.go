package journal

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	"github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/journal/retirement"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// OpenCapturedNativeRecovery reloads and classifies a published operation for its containing
// Apply. Root grants and the allocation stay borrowed from that live operation.
func OpenCapturedNativeRecovery(ctx context.Context, captured CapturedNativeJournal, codec durable.SnapshotCodec, options NativeRecoveryOptions) (*NativeRecoveryOperation, error) {
	if err := captured.Validate(); err != nil {
		return nil, err
	}
	if codec == nil || captured.publication.roots.Manifest.Root == nil {
		return nil, fmt.Errorf("captured native recovery requires its selected codec and roots")
	}
	reader, ok := options.Filesystem.(mutationfs.Reader)
	if !ok {
		return nil, fmt.Errorf("captured native recovery requires inventory observation")
	}
	inventory, err := journalinventory.LoadNativeRoot(ctx, filepath.Dir(captured.capture.Directory), journalinventory.Options{Filesystem: reader, StateCodec: codec, PhysicalPathBudget: captured.budget})
	if err != nil {
		return nil, err
	}
	if inventory.Active() == nil || !inventory.Active().PhysicalAuthority().Equal(captured.active) || inventory.Active().OperationDir() != captured.capture.Directory || inventory.Active().Identity().JournalAuthorityFingerprint() != captured.capture.RecordFingerprint {
		return nil, fmt.Errorf("captured native recovery operation changed")
	}
	assessment, err := captured.AssessRecovery(ctx, options)
	if err != nil {
		return nil, err
	}
	inv, err := newRecoverableInventoryAuthority(inventory, inventory.Decision().State() == retirement.StatePrepared)
	if err != nil {
		return nil, err
	}
	qualified := assessment.assessment.Documents()
	observed := assessment.assessment
	scope, err := observed.RegistryScope()
	if err != nil {
		return nil, err
	}
	plan := NativeRecoveryPlan{journal: captured.journal, capture: captured.capture, active: captured.active, inventory: inv, assessment: observed.Facts(), registryScope: scope}
	return &NativeRecoveryOperation{
		plan:    plan,
		profile: recoveryoperation.Profile{StateCodec: codec, Codecs: options.Codecs, Limits: captured.publication.limits, Budget: captured.budget},
		roots:   captured.publication.roots, qualified: qualified, initial: observed, capturedRemovalReservations: true,
		resolver: captured.publication.resolver, ordinaryCapabilities: captured.publication.ordinaryCapabilities,
	}, nil
}

func (operation *NativeRecoveryOperation) StateCodec() durable.SnapshotCodec {
	return operation.profile.StateCodec
}

func (operation *NativeRecoveryOperation) Codecs() aggregate.CodecCatalog {
	return operation.profile.Codecs
}
