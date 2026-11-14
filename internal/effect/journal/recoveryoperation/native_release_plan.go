package recoveryoperation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/effect/fileset"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

// NativeReleasePaths are selected by the caller, never loaded from the journal.
type NativeReleasePaths = journalrecord.NativeReleaseControlPaths

// NativeReleaseRecoveryPlan deliberately has no current Snapshot observation.
// The containing native operation settles its intent-matched FileSet first.
type NativeReleaseRecoveryPlan struct {
	source      journalinventory.ActiveJournal
	release     journalrecord.NativeRelease
	inventory   recoverableInventoryAuthority
	paths       NativeReleasePaths
	targets     []fileset.RecoveryTarget
	fence       fileset.FileSetFenceKind
	disposition fileset.RecoveryDisposition
	initialized bool
}

func (NativeReleaseRecoveryPlan) recoverablePlan() {}
func (plan NativeReleaseRecoveryPlan) Clone() RecoverablePlan {
	plan.targets = slices.Clone(plan.targets)
	return plan
}

func (NativeReleaseRecoveryPlan) AuthorityKind() RecoveryAuthorityKind {
	return RecoveryAuthorityActiveJournal
}
func (plan NativeReleaseRecoveryPlan) Blocked() bool        { return !plan.initialized }
func (plan NativeReleaseRecoveryPlan) HasErrors() bool      { return plan.Blocked() }
func (plan NativeReleaseRecoveryPlan) OperationID() string  { return plan.release.OperationID() }
func (plan NativeReleaseRecoveryPlan) OperationDir() string { return plan.source.OperationDir() }
func (plan NativeReleaseRecoveryPlan) RecordFingerprint() string {
	return plan.source.Identity().JournalAuthorityFingerprint()
}

func (plan NativeReleaseRecoveryPlan) Release() journalrecord.NativeRelease   { return plan.release }
func (plan NativeReleaseRecoveryPlan) Source() journalinventory.ActiveJournal { return plan.source }
func (plan NativeReleaseRecoveryPlan) LocalPaths() NativeReleasePaths         { return plan.paths }
func (plan NativeReleaseRecoveryPlan) LocalTargets() []fileset.RecoveryTarget {
	return slices.Clone(plan.targets)
}
func (plan NativeReleaseRecoveryPlan) FileSetFence() fileset.FileSetFenceKind { return plan.fence }
func (plan NativeReleaseRecoveryPlan) FileSetDisposition() fileset.RecoveryDisposition {
	return plan.disposition
}

func (plan NativeReleaseRecoveryPlan) ObservationFingerprint() string {
	return journalrecord.Fingerprint([]byte(plan.RecordFingerprint() + "\x00" + string(plan.fence) + "\x00" + string(plan.disposition)))
}

func (plan NativeReleaseRecoveryPlan) SameExecutionAuthority(other RecoverablePlan) bool {
	typed, ok := other.(NativeReleaseRecoveryPlan)
	return ok && plan.initialized && typed.initialized && plan.source.SameSource(typed.source) &&
		plan.inventory.equal(typed.inventory) && plan.paths == typed.paths && plan.fence == typed.fence &&
		plan.disposition == typed.disposition && slices.Equal(plan.targets, typed.targets)
}

func NativeReleaseActiveRecoveryPlan(selection RecoverablePlan) (NativeReleaseRecoveryPlan, bool) {
	plan, ok := selection.(NativeReleaseRecoveryPlan)
	return plan, ok
}

func loadNativeReleaseRecoveryPlan(ctx context.Context, paths journalpublication.Paths, options PlanLoadOptions, inventory journalinventory.RootInventory) (NativeReleaseRecoveryPlan, error) {
	if options.NativeReleasePaths == nil || options.NativeDocuments == nil {
		return NativeReleaseRecoveryPlan{}, fmt.Errorf("native release recovery requires independently selected control paths and native codecs")
	}
	active := inventory.Active()
	if active == nil {
		return NativeReleaseRecoveryPlan{}, fmt.Errorf("native release active journal is unavailable")
	}
	release, present := active.Record().NativeRelease()
	if !present {
		return NativeReleaseRecoveryPlan{}, fmt.Errorf("native release recovery requires its closed record purpose")
	}
	if options.NativeDocuments.RawRegistry == nil {
		return NativeReleaseRecoveryPlan{}, fmt.Errorf("native release recovery requires recorded Registry observation")
	}
	registry, err := options.NativeDocuments.RawRegistry(ctx)
	if err != nil {
		return NativeReleaseRecoveryPlan{}, err
	}
	if err := release.Metadata().RequireProtectionClear(registry); err != nil {
		return NativeReleaseRecoveryPlan{}, err
	}
	targets, err := journalqualification.MatchNativeReleaseTargets(ctx, release, *options.NativeReleasePaths, options.PhysicalPathBudget)
	if err != nil {
		return NativeReleaseRecoveryPlan{}, err
	}
	fileSetBudget, bounded := options.PhysicalPathBudget.(fileset.PhysicalWorkBudget)
	if !bounded {
		return NativeReleaseRecoveryPlan{}, fmt.Errorf("native release FileSet inspection requires the shared physical-work allocation")
	}
	stateDir := filepath.Dir(targets[2].Path)
	fenceErr := fileset.ObserveClearFenceAt(ctx, stateDir, physicalwork.MaximumPhysicalPathDepth, fileSetBudget)
	fence := fileset.FileSetFenceKindOf(fenceErr)
	var disposition fileset.RecoveryDisposition
	if fenceErr != nil {
		if fence != fileset.FileSetFencePublishedTransaction {
			return NativeReleaseRecoveryPlan{}, errors.Join(ErrInterruptedApply, fenceErr)
		}
		disposition, err = fileset.InspectFileSetRecoveryForIntent(ctx, stateDir, targets)
		if err != nil {
			return NativeReleaseRecoveryPlan{}, err
		}
	}
	authority, err := newRecoverableInventoryAuthority(inventory, inventory.Decision().State() == retirement.StatePrepared)
	if err != nil {
		return NativeReleaseRecoveryPlan{}, err
	}
	return NativeReleaseRecoveryPlan{
		source: *active, release: release, inventory: authority,
		paths:   NativeReleasePaths{Manifest: targets[0].Path, DefaultLock: targets[1].Path, State: targets[2].Path},
		targets: targets, fence: fence, disposition: disposition, initialized: true,
	}, nil
}
