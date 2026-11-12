package metadataconversion

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

// Recovery retains a preview of the exact original operation, not current cwd's
// selected project. The caller still selects the owning DataDir.
type Recovery struct {
	plan     Plan
	revision string
	action   fileset.RecoveryDisposition
}

func (recovery Recovery) Action() fileset.RecoveryDisposition { return recovery.action }

func (recovery Recovery) OriginalManifestPath() string { return recovery.plan.selection.manifestPath }

func (recovery Recovery) ParticipantCount() int { return recovery.plan.ParticipantCount() }

// InspectRecovery reconstructs the complete expected intent from verified
// Before evidence and freshly qualified original references. Live mixed registry
// bytes and recorded evidence alone cannot authorize recovery effects.
func InspectRecovery(ctx context.Context, selected daempaths.Paths, budget fileset.PhysicalWorkBudget) (Recovery, error) {
	if ctx == nil || budget == nil {
		return Recovery{}, fmt.Errorf("metadata recovery requires context and shared physical work budget")
	}
	view, err := fileset.ReadPublicationIntentEvidence5(ctx, selected.GlobalMetadataDir(), mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return Recovery{}, err
	}
	original, err := decodeSelection(view.Intent().Selection())
	if err != nil {
		return Recovery{}, err
	}
	paths, err := daempaths.Resolve(original.manifestPath)
	if err != nil {
		return Recovery{}, err
	}
	paths, err = paths.WithDataDir(selected.DataDir)
	if err != nil {
		return Recovery{}, err
	}
	reader := beforeSources(paths.GlobalMetadataDir(), view.MarkerRevision(), budget)
	plan, err := compilePlan(ctx, compilerInput{Paths: paths, PhysicalWorkBudget: budget}, reader, &original)
	if err != nil {
		return Recovery{}, err
	}
	barrier, err := recoverygate.CaptureMetadataRecoveryBarrier(ctx, plan.paths, plan.selection.barrierDirs, view.MarkerRevision(), budget)
	if err != nil {
		return Recovery{}, err
	}
	if err := barrier.Validate(ctx); err != nil {
		return Recovery{}, err
	}
	plan.barrier = barrier
	action, err := fileset.InspectFileSetRecovery5(ctx, plan.paths.GlobalMetadataDir(), plan.intent, view.MarkerRevision(), budget)
	if err != nil {
		return Recovery{}, err
	}
	return Recovery{plan: plan, revision: view.MarkerRevision(), action: action}, nil
}

// Execute preserves the preview's exact revision, selection and disposition
// under complete leases. Only existing rollback/finalize recovery is available.
func (recovery Recovery) Execute(ctx context.Context) (returnErr error) {
	plan := recovery.plan
	if ctx == nil || plan.budget == nil || plan.intent.Validate() != nil || recovery.revision == "" {
		return fmt.Errorf("metadata recovery is not initialized")
	}
	domains, err := plan.domains()
	if err != nil {
		return err
	}
	store, err := mutation.NewStore(plan.paths.DataDir)
	if err != nil {
		return err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, leases.Release()) }()
	if err := validateHeldMetadataLeases(ctx, leases); err != nil {
		return err
	}
	if err := plan.barrier.Validate(ctx); err != nil {
		return err
	}
	fresh, err := compilePlan(ctx, compilerInput{Paths: plan.paths, PhysicalWorkBudget: plan.budget}, beforeSources(plan.paths.GlobalMetadataDir(), recovery.revision, plan.budget), &plan.selection)
	if err != nil {
		return err
	}
	if !plan.intent.Equal(fresh.intent) {
		return mutation.StaleSnapshotError{}
	}
	action, err := fileset.InspectFileSetRecovery5(ctx, plan.paths.GlobalMetadataDir(), plan.intent, recovery.revision, plan.budget)
	if err != nil {
		return err
	}
	if action != recovery.action {
		return mutation.StaleSnapshotError{}
	}
	if err := validateHeldMetadataLeases(ctx, leases); err != nil {
		return err
	}
	if err := plan.barrier.Validate(ctx); err != nil {
		return err
	}
	return fileset.RecoverFileSet5(ctx, plan.paths.GlobalMetadataDir(), plan.intent, recovery.revision, plan.budget)
}

func beforeSources(root, revision string, budget fileset.PhysicalWorkBudget) sourceReader {
	return func(ctx context.Context, path string, kind sourceKind) (sourceEntry, error) {
		view, err := fileset.ReadPublicationBeforeEvidence5(ctx, root, path, revision, mutationfs.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return sourceEntry{}, err
		}
		entry := sourceEntry{image: view.Image(), content: view.Content()}
		if err := validateSourceEntry(kind, entry); err != nil {
			return sourceEntry{}, err
		}
		return entry, nil
	}
}
