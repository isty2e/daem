package journal

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// OpenRecovery reloads and classifies a published operation for its containing
// Apply. Root grants and the allocation stay borrowed from that live operation.
func (captured CapturedNativeJournal) OpenRecovery(ctx context.Context, codec durable.SnapshotCodec, options NativeRecoveryOptions) (*NativeRecoveryOperation, error) {
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
	inventory, err := loadNativeRecoveryRootInventory(ctx, filepath.Dir(captured.capture.Directory), inventoryOptions{Filesystem: reader, StateCodec: codec, PhysicalPathBudget: captured.budget})
	if err != nil {
		return nil, err
	}
	if inventory.active == nil || !inventory.active.physicalAuthority.equal(captured.active) || inventory.active.operationDir != captured.capture.Directory || inventory.active.identity.JournalAuthorityFingerprint() != captured.capture.RecordFingerprint {
		return nil, fmt.Errorf("captured native recovery operation changed")
	}
	assessment, err := captured.AssessRecovery(ctx, options)
	if err != nil {
		return nil, err
	}
	inv, err := newRecoverableInventoryAuthority(inventory, inventory.decision.State() == retirement.StatePrepared)
	if err != nil {
		return nil, err
	}
	qualified := assessment.assessment.qualified
	observed := assessment.assessment
	scope, err := observed.registryScope()
	if err != nil {
		return nil, err
	}
	observed.qualified = nativeQualifiedDocumentRecords{records: qualified.records}
	plan := NativeRecoveryPlan{journal: captured.journal, capture: captured.capture, active: captured.active, inventory: inv, assessment: observed, registryScope: scope}
	return &NativeRecoveryOperation{
		plan:    plan,
		profile: NativeRecoveryProfile{StateCodec: codec, Codecs: options.Codecs, Limits: captured.publication.limits, Budget: captured.budget},
		roots:   captured.publication.roots, qualified: qualified, capturedRemovalReservations: true,
		resolver: captured.publication.resolver, ordinaryCapabilities: captured.publication.ordinaryCapabilities,
	}, nil
}

func (operation *NativeRecoveryOperation) StateCodec() durable.SnapshotCodec {
	return operation.profile.StateCodec
}

func (operation *NativeRecoveryOperation) Codecs() aggregate.CodecCatalog {
	return operation.profile.Codecs
}
