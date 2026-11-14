package nativeoperation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/fileset"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/operationplan"
)

// ReleaseOptions has metadata dependencies only. No host resolver, host program
// or ordinary claim-convergence port is admitted by this operation purpose.
type ReleaseOptions struct {
	Filesystem                  mutationfs.Store
	RegistryBinder              ownershipmutation.OwnReleaseRegistryBinder
	Budget                      *physicalwork.Budget
	StateCodec                  durable.SnapshotCodec
	StateReader                 durable.SnapshotReader
	ValidateBeforeEffects       func(context.Context, mutation.PhysicalAuthoritySet) error
	ValidateVisibilityAuthority func(context.Context) error
	AcceptVisibilityChanges     func(context.Context) error
}

// ExecuteNativeRelease publishes the containing journal before the finite
// local FileSet. Failures retain that journal for explicit native recovery.
func ExecuteNativeRelease(ctx context.Context, release journalrecord.NativeRelease, local journalrecord.NativeReleaseControlPaths, files []fileset.FileTarget, paths execute.Paths, options ReleaseOptions) (resultErr error) {
	if err := release.Validate(); err != nil {
		return err
	}
	operation, err := newNativeReleaseOperation(ctx, release, local, filepath.Join(paths.RecoveryDir, release.OperationID()), paths, options)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, operation.close(), ctx.Err()) }()
	if err := operation.before(); err != nil {
		return err
	}
	registry, err := operation.store.LoadRecordedRegistry(ctx)
	if err != nil {
		return err
	}
	if err := release.Metadata().RequireBefore(registry); err != nil {
		return err
	}
	captured, err := journalpublication.CaptureNativeRelease(ctx, paths.JournalPaths(), release, journalpublication.NativeReleasePublicationOptions{
		OperationAuthority: operation.journal, Filesystem: options.Filesystem, StateCodec: options.StateCodec, Budget: options.Budget,
	})
	if err != nil {
		return err
	}
	if err := operation.after(); err != nil {
		return err
	}
	before, err := operation.assess(captured.Source())
	if err != nil {
		return err
	}
	if !before.CleanBefore() {
		return fmt.Errorf("native release changed before local publication")
	}
	return operation.settle(captured.Source(), func() error {
		return fileset.CommitFreshFileSetForIntent(ctx, fileset.FileSetInput{StateDir: paths.StateDir, Targets: files}, operation.targets)
	})
}

// ExecuteNativeReleaseRecovery settles the intent-matched FileSet before any
// current Snapshot interpretation. It cannot replay a host-effect program.
func ExecuteNativeReleaseRecovery(ctx context.Context, plan journal.NativeReleaseRecoveryPlan, paths execute.Paths, options ReleaseOptions) (resultErr error) {
	if plan.Blocked() {
		return fmt.Errorf("native release recovery is uninitialized")
	}
	operation, err := newNativeReleaseOperation(ctx, plan.Release(), plan.LocalPaths(), plan.OperationDir(), paths, options)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, operation.close(), ctx.Err()) }()
	return operation.settle(plan.Source(), func() error {
		return fileset.RecoverFileSetForIntent(ctx, paths.StateDir, operation.targets)
	})
}

type nativeReleaseOperation struct {
	ctx      context.Context
	release  journalrecord.NativeRelease
	local    journalrecord.NativeReleaseControlPaths
	options  ReleaseOptions
	roots    []*rootedpath.CapturedRoot
	entries  []*rootedpath.EntryAuthority
	journal  *rootedpath.EntryAuthority
	controls []*rootedpath.EntryAuthority
	store    ownershipmutation.OwnReleaseRegistryStore
	targets  []fileset.RecoveryTarget
	requests mutation.PhysicalAuthoritySet
}

func newNativeReleaseOperation(ctx context.Context, release journalrecord.NativeRelease, local journalrecord.NativeReleaseControlPaths, directory string, paths execute.Paths, options ReleaseOptions) (result *nativeReleaseOperation, resultErr error) {
	if ctx == nil || options.Filesystem == nil || options.ValidateBeforeEffects == nil || options.AcceptVisibilityChanges == nil || options.RegistryBinder == nil || options.StateReader == nil || options.StateCodec == nil || options.Budget == nil {
		return nil, fmt.Errorf("native release requires selected metadata dependencies and containing authority")
	}
	operation := &nativeReleaseOperation{ctx: ctx, release: release, local: local, options: options}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, operation.close())
		}
	}()
	targets, err := journalqualification.MatchNativeReleaseTargets(ctx, release, local, options.Budget)
	if err != nil {
		return nil, err
	}
	operation.targets = targets
	local = journalrecord.NativeReleaseControlPaths{Manifest: operation.targets[0].Path, DefaultLock: operation.targets[1].Path, State: operation.targets[2].Path}
	operation.local = local
	statePath, err := mutation.CanonicalDirectoryEntryPathBounded(paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, options.Budget)
	if err != nil {
		return nil, err
	}
	stateDir, err := fileset.CanonicalStateDirBounded(paths.StateDir, physicalwork.MaximumPhysicalPathDepth, options.Budget)
	if err != nil {
		return nil, err
	}
	if statePath != local.State || stateDir != filepath.Dir(local.State) {
		return nil, fmt.Errorf("native release local state differs from its containing FileSet boundary")
	}
	root, err := rootedpath.CaptureRootBounded(paths.ManifestRoot, physicalwork.MaximumPhysicalPathDepth, options.Budget)
	if err != nil {
		return nil, err
	}
	operation.roots = append(operation.roots, root)
	for index, path := range []string{directory, local.Manifest, local.DefaultLock, local.State} {
		entry, err := rootedpath.BindCanonicalSelectedEntryAuthorityBounded(root, paths.ManifestRoot, path, physicalwork.MaximumPhysicalPathDepth, options.Budget)
		if err != nil {
			return nil, err
		}
		operation.entries = append(operation.entries, entry)
		if index == 0 {
			operation.journal = entry
		} else {
			operation.controls = append(operation.controls, entry)
		}
	}
	registryRoot, destination, err := rootedpath.CaptureDestinationBounded(paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, options.Budget)
	if err != nil {
		return nil, err
	}
	operation.roots = append(operation.roots, registryRoot)
	operation.store, err = options.RegistryBinder(registryRoot, destination, physicalwork.MaximumPhysicalPathDepth, options.Budget)
	if err != nil {
		return nil, err
	}
	operation.requests, err = nativeReleaseAuthorityRequests(local, directory, paths.OwnershipRegistryPath)
	if err != nil {
		return nil, err
	}
	return operation, nil
}

func (operation *nativeReleaseOperation) close() (err error) {
	for _, entry := range operation.entries {
		err = errors.Join(err, entry.Close())
	}
	for _, root := range operation.roots {
		err = errors.Join(err, root.Close())
	}
	return err
}

func (operation *nativeReleaseOperation) before() error {
	ctx, options := operation.ctx, operation.options
	if err := ctx.Err(); err != nil {
		return err
	}
	if options.ValidateVisibilityAuthority != nil {
		if err := options.ValidateVisibilityAuthority(ctx); err != nil {
			return err
		}
	}
	if err := options.ValidateBeforeEffects(ctx, operation.requests); err != nil {
		return err
	}
	registry, err := operation.store.LoadRecordedRegistry(ctx)
	if err != nil {
		return err
	}
	return operation.release.Metadata().RequireProtectionClear(registry)
}

func (operation *nativeReleaseOperation) after() error {
	return operation.options.AcceptVisibilityChanges(operation.ctx)
}

func (operation *nativeReleaseOperation) assess(source journalinventory.ActiveJournal) (journalqualification.NativeReleaseAssessment, error) {
	return journalqualification.AssessNativeRelease(operation.ctx, source, journalqualification.NativeReleaseReadOptions{
		Paths: operation.local, OperationAuthority: operation.journal, LocalAuthorities: operation.controls,
		Filesystem: operation.options.Filesystem, Budget: operation.options.Budget,
		State: operation.options.StateReader, Registry: operation.store.LoadRecordedRegistry,
	})
}

func (operation *nativeReleaseOperation) settle(source journalinventory.ActiveJournal, localFiles func() error) (resultErr error) {
	ctx, options := operation.ctx, operation.options
	if err := operation.before(); err != nil {
		return err
	}
	if err := options.Budget.AdmitRemovalDemands(recovery.RemovalDemandSet{}); err != nil {
		return err
	}
	retiring, err := journal.PrepareNativeReleaseJournalRetirement(ctx, source, options.Budget, options.Filesystem, options.StateCodec)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, retiring.Close()) }()
	steps := []nativeRecoveryStep{
		{name: "native-release/local-files", kind: operationplan.EffectStepPersistence, run: func() error {
			if err := operation.before(); err != nil {
				return err
			}
			if err := localFiles(); err != nil {
				return err
			}
			return operation.after()
		}},
		{name: "native-release/own-metadata", kind: operationplan.EffectStepPersistence, run: func() error {
			assessment, err := operation.assess(source)
			if err != nil {
				return err
			}
			if assessment.CleanBefore() || assessment.CleanAfter() {
				return nil
			}
			if !assessment.NeedsOwnRelease() {
				return fmt.Errorf("native release has no admitted metadata phase")
			}
			if err := operation.before(); err != nil {
				return err
			}
			if _, err := operation.store.ConvergeOwnReleaseAfterCommit(ctx, operation.release.Metadata()); err != nil {
				return err
			}
			return operation.after()
		}},
		{name: "native-release/retirement", kind: operationplan.EffectStepRetirement, run: func() error {
			assessment, err := operation.assess(source)
			if err != nil {
				return err
			}
			if !assessment.CleanBefore() && !assessment.CleanAfter() {
				return fmt.Errorf("native release is not clean enough to retire")
			}
			structure, err := journal.CompileRetirementSchedule()
			if err != nil {
				return err
			}
			cursor := journal.NewRetirementExecution(structure)
			gate := nativeRetirementGate{ctx: ctx, before: operation.before, after: operation.after, execution: cursor}
			err = retiring.ExecuteReleaseWithGate(ctx, assessment, &gate)
			return errors.Join(err, cursor.Finish(err))
		}},
	}
	structure, err := compileNativeRecoverySteps(steps)
	if err != nil {
		return err
	}
	return runNativeRecoverySteps(steps, structure)
}

func nativeReleaseAuthorityRequests(paths journalrecord.NativeReleaseControlPaths, operationDir, registry string) (mutation.PhysicalAuthoritySet, error) {
	marker, err := fileset.FileSetAuthorityPath(filepath.Dir(paths.State))
	if err != nil {
		return mutation.PhysicalAuthoritySet{}, err
	}
	var requests []mutation.PhysicalAuthorityRequest
	for _, path := range []string{paths.Manifest, paths.DefaultLock, paths.State, marker, operationDir, filepath.Join(operationDir, journalrecord.FileName), registry} {
		requests = append(requests, mutation.PhysicalAuthorityRequest{Path: path, Target: "metadata", Scope: "project"})
	}
	return mutation.NewPhysicalAuthoritySet(requests...)
}
