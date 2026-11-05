package journal

import (
	"context"
	"fmt"
	"path/filepath"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	"github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/journal/retirement"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func nativeCaptureResult(source journalinventory.ActiveJournal) journalpublication.CaptureResult {
	return journalpublication.CaptureResult{
		OperationID: source.Identity().OperationID(), Directory: source.OperationDir(),
		JournalPath:       filepath.Join(source.OperationDir(), journalrecord.FileName),
		RecordFingerprint: source.Identity().JournalAuthorityFingerprint(),
	}
}

// OpenCapturedNativeRecovery reloads and classifies a published operation for its containing
// Apply. Root grants and the allocation stay borrowed from that live operation.
func OpenCapturedNativeRecovery(ctx context.Context, captured journalpublication.CapturedNativeJournal, codec durable.SnapshotCodec, manifest *rootedpath.CapturedRoot, budget *physicalwork.Budget, options journalqualification.NativeRecoveryOptions) (*NativeRecoveryOperation, error) {
	if err := captured.RequireRecoverySelection(manifest, budget); err != nil {
		return nil, err
	}
	if codec == nil {
		return nil, fmt.Errorf("captured native recovery requires its selected codec and roots")
	}
	reader, ok := options.Filesystem.(mutationfs.Reader)
	if !ok {
		return nil, fmt.Errorf("captured native recovery requires inventory observation")
	}
	capture := captured.CaptureResult()
	inventory, err := journalinventory.LoadNativeRoot(ctx, filepath.Dir(capture.Directory), journalinventory.Options{Filesystem: reader, StateCodec: codec, PhysicalPathBudget: budget})
	if err != nil {
		return nil, err
	}
	if inventory.Active() == nil || !inventory.Active().PhysicalAuthority().Equal(captured.ActiveJournalAuthority()) || inventory.Active().OperationDir() != capture.Directory || inventory.Active().Identity().JournalAuthorityFingerprint() != capture.RecordFingerprint {
		return nil, fmt.Errorf("captured native recovery operation changed")
	}
	assessment, err := captured.AssessRecovery(ctx, options)
	if err != nil {
		return nil, err
	}
	journal, native := inventory.Active().Record().Native()
	if !native || !assessment.MatchesSource(*inventory.Active()) {
		return nil, fmt.Errorf("captured recovery assessment differs from its reread source")
	}
	inv, err := newRecoverableInventoryAuthority(inventory, inventory.Decision().State() == retirement.StatePrepared)
	if err != nil {
		return nil, err
	}
	qualified := assessment.Documents()
	observed := assessment
	scope, err := observed.RegistryScope()
	if err != nil {
		return nil, err
	}
	plan := NativeRecoveryPlan{source: *inventory.Active(), journal: journal, capture: nativeCaptureResult(*inventory.Active()), active: inventory.Active().PhysicalAuthority(), inventory: inv, assessment: observed.Facts(), registryScope: scope}
	return &NativeRecoveryOperation{
		plan: plan, captured: &captured, borrowedManifest: manifest,
		profile:   recoveryoperation.Profile{StateCodec: codec, Codecs: options.Codecs, Budget: budget},
		qualified: qualified, initial: observed,
		resolver: captured.ResolveOrdinaryDestination,
	}, nil
}

func (operation *NativeRecoveryOperation) StateCodec() durable.SnapshotCodec {
	return operation.profile.StateCodec
}

func (operation *NativeRecoveryOperation) Codecs() aggregate.CodecCatalog {
	return operation.profile.Codecs
}
