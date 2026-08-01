package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// recoveryBackup is one content-addressed recovery-private artifact view.
// Verification consumes the same bytes that are returned or copied.
type recoveryBackup struct {
	view     access.View
	identity artifact.ExactIdentity
}

func newRecoveryBackup(
	path string,
	reference string,
	kind string,
	contentHash string,
) (recoveryBackup, error) {
	view, err := access.OpenView(path)
	if err != nil {
		return recoveryBackup{}, err
	}
	identity, err := artifact.NewExactIdentity(
		artifact.SourceID("recovery:backup"),
		artifact.ResolvedRef(reference),
		artifact.ArtifactKind(kind),
		artifact.ContentHash(contentHash),
	)
	if err != nil {
		return recoveryBackup{}, err
	}
	return recoveryBackup{view: view, identity: identity}, nil
}

func (backup recoveryBackup) readFile(ctx context.Context) ([]byte, error) {
	content, err := backup.view.ReadRootFileVerified(
		ctx,
		backup.identity,
		journal.MaximumRecoveryBackupFileBytes,
	)
	if err != nil {
		return nil, err
	}
	return content.Bytes(), nil
}

func (backup recoveryBackup) copyDirectory(
	ctx context.Context,
	writer mutationfs.RootedTreeWriter,
) error {
	if backup.identity.Kind() != artifact.ArtifactKindDirectory {
		return fmt.Errorf("recovery backup is not a directory")
	}
	sink, err := artifactstage.New(writer)
	if err != nil {
		return err
	}
	return backup.view.CopyVerified(ctx, backup.identity, sink)
}

// RecoveryOptions configures workflow-owned validation before and after each
// visibility-changing recovery effect.
type RecoveryOptions struct {
	ValidateBeforeEffects       func(context.Context, mutation.PhysicalAuthoritySet) error
	ValidateVisibilityAuthority func(context.Context) error
	AcceptVisibilityChanges     func(context.Context) error
	ActiveJournalAuthority      journal.ActiveJournalAuthority
	Resolver                    DestinationResolver
	Codecs                      aggregate.CodecCatalog
	OwnershipRegistryBinder     ownershipmutation.RootedRegistryBinder
	StateCodec                  durable.SnapshotCodec
	StateReader                 durable.SnapshotReader
	Filesystem                  mutationfs.Store
	reloadPlan                  func(context.Context, journal.PlanLoadOptions) (recovery.Plan, error)
	mutationAuthority           *mutationAuthority
	beforeHostAction            func(int) error
}

// ExecuteRecoveryPlanWithOptions applies a journal-derived recovery plan after
// invoking the workflow's final authority validation.
func ExecuteRecoveryPlanWithOptions(ctx context.Context, plan recovery.Plan, paths Paths, options RecoveryOptions) error {
	if ctx == nil {
		return fmt.Errorf("recovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.Classification() == recovery.ClassificationBlocked {
		return fmt.Errorf("recovery is blocked")
	}
	if options.Resolver == nil {
		return fmt.Errorf("recovery destination resolver is required")
	}
	if options.StateCodec == nil {
		return fmt.Errorf("recovery state codec is required")
	}
	if options.reloadPlan == nil && options.StateReader == nil {
		return fmt.Errorf("recovery state reader is required")
	}
	if options.Filesystem == nil {
		return fmt.Errorf("recovery filesystem is required")
	}
	if err := options.ActiveJournalAuthority.Validate(); err != nil {
		return err
	}
	if options.reloadPlan == nil {
		options.reloadPlan = func(
			ctx context.Context,
			loadOptions journal.PlanLoadOptions,
		) (recovery.Plan, error) {
			return journal.LoadActivePlanWithOptions(
				ctx,
				paths.journalPaths(),
				loadOptions,
			)
		}
	}
	authority := options.mutationAuthority
	ownsAuthority := false
	if authority == nil {
		var err error
		authority, err = newRecoveryMutationAuthority(
			paths,
			plan.GuardedActions(),
			options.Resolver,
			options.Filesystem,
			options.OwnershipRegistryBinder,
		)
		if err != nil {
			return err
		}
		ownsAuthority = true
		options.mutationAuthority = authority
	}
	if ownsAuthority {
		defer authority.close()
	}
	if err := authority.bindRecoveryJournal(paths.ManifestRoot, plan.OperationDir()); err != nil {
		return err
	}
	if err := authority.setActiveJournalAuthority(options.ActiveJournalAuthority); err != nil {
		return err
	}
	if err := authority.validateActiveJournalAuthority(ctx); err != nil {
		return err
	}
	if err := executeRecoveryPlanEffects(ctx, plan, paths, options); err != nil {
		return err
	}
	if err := authority.validateProjectSelection(paths.ManifestRoot); err != nil {
		return fmt.Errorf("%w; recovery effects committed; recovery journal retained", err)
	}
	visibilityGate := recoveryVisibilityGate(options)
	if err := visibilityGate.validateBefore(ctx); err != nil {
		return fmt.Errorf("%w; recovery effects committed; recovery journal retained", err)
	}
	retirementPlan, err := reloadRecoveryPlanAfterEffects(ctx, plan, options, authority)
	if err != nil {
		return fmt.Errorf("%w; recovery effects committed; recovery journal retained", err)
	}
	if err := authority.retireActiveJournal(
		ctx,
		paths,
		retirementPlan,
		options.StateCodec,
	); err != nil {
		return fmt.Errorf("retire recovery journal: %w", err)
	}
	if err := visibilityGate.acceptAfter(ctx); err != nil {
		return fmt.Errorf("%w; recovery effects committed; recovery journal retired", err)
	}
	if err := authority.validateProjectSelection(paths.ManifestRoot); err != nil {
		return fmt.Errorf("%w; recovery effects committed; recovery journal retired", err)
	}
	return nil
}

func executeRecoveryPlanEffects(ctx context.Context, plan recovery.Plan, paths Paths, options RecoveryOptions) error {
	authority := options.mutationAuthority
	ownsAuthority := false
	var err error
	if authority == nil {
		if len(plan.ClaimTransitions()) != 0 && options.OwnershipRegistryBinder == nil {
			return fmt.Errorf("recovery ownership registry binder is required")
		}
		authority, err = newRecoveryMutationAuthority(
			paths,
			plan.GuardedActions(),
			options.Resolver,
			options.Filesystem,
			options.OwnershipRegistryBinder,
		)
		ownsAuthority = true
	} else {
		if authority.ownershipRegistryBinder == nil {
			authority.ownershipRegistryBinder = options.OwnershipRegistryBinder
		}
		if len(plan.ClaimTransitions()) != 0 &&
			!authority.hasOwnershipRegistry &&
			authority.ownershipRegistryBinder == nil {
			return fmt.Errorf("recovery ownership registry binder is required")
		}
		err = requireRecoveryGlobalBindings(authority, plan.GuardedActions())
	}
	if err != nil {
		return err
	}
	if ownsAuthority {
		defer authority.close()
	}
	var registryStore ownershipmutation.RegistryStore
	if len(plan.ClaimTransitions()) != 0 {
		if err := authority.bindOwnershipRegistry(paths.OwnershipRegistryPath); err != nil {
			return err
		}
		registryStore, err = authority.rootedOwnershipRegistry()
		if err != nil {
			return err
		}
	}
	physicalAuthority, err := authority.physicalAuthority()
	if err != nil {
		return err
	}

	current, err := recoveryPlanBeforeEffects(
		ctx,
		plan,
		options,
		physicalAuthority,
		recoveryPlanLoadOptions(options, authority),
	)
	if err != nil {
		return err
	}
	plan = current
	visibilityGate := recoveryVisibilityGate(options)

	switch plan.Classification() {
	case recovery.ClassificationCleanBefore, recovery.ClassificationCleanAfter:
		return nil
	case recovery.ClassificationNeedsRollback:
		return executeRecoveryRollbackEffects(
			ctx,
			plan,
			paths,
			authority,
			registryStore,
			options.beforeHostAction,
			options.Codecs,
			visibilityGate,
		)
	case recovery.ClassificationNeedsFinalize:
		if err := finalizeClaimTransitions(ctx, registryStore, plan.ClaimTransitions(), visibilityGate); err != nil {
			return fmt.Errorf("finalize recovery ownership claims: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported recovery classification %q", plan.Classification())
	}
}

func recoveryPlanLoadOptions(
	options RecoveryOptions,
	authority *mutationAuthority,
) journal.PlanLoadOptions {
	return journal.PlanLoadOptions{
		Filesystem:        options.Filesystem,
		RootedCapability:  authority.rootedJournalCapability,
		Resolver:          authority.rootedJournalResolver(options.Resolver),
		OwnershipRegistry: authority.rootedOwnershipRegistryOption(),
		Codecs:            options.Codecs,
		StateCodec:        options.StateCodec,
		StateReader:       options.StateReader,
	}
}

func reloadRecoveryPlanAfterEffects(
	ctx context.Context,
	expected recovery.Plan,
	options RecoveryOptions,
	authority *mutationAuthority,
) (recovery.Plan, error) {
	current, err := options.reloadPlan(ctx, recoveryPlanLoadOptions(options, authority))
	if err != nil {
		return recovery.Plan{}, fmt.Errorf("reload recovery plan after effects: %w", err)
	}
	if err := requireSameActiveJournal(expected, current, "after effects"); err != nil {
		return recovery.Plan{}, err
	}
	if current.Blocked() || current.HasErrors() {
		return recovery.Plan{}, fmt.Errorf("recovery is blocked by current evidence")
	}
	switch current.Classification() {
	case recovery.ClassificationCleanBefore, recovery.ClassificationCleanAfter:
		return current, nil
	default:
		return recovery.Plan{}, fmt.Errorf(
			"recovery effects left journal classified as %q",
			current.Classification(),
		)
	}
}

func recoveryPlanBeforeEffects(
	ctx context.Context,
	plan recovery.Plan,
	options RecoveryOptions,
	physicalAuthority mutation.PhysicalAuthoritySet,
	loadOptions journal.PlanLoadOptions,
) (recovery.Plan, error) {
	if err := validateRecoveryBeforeEffects(ctx, options, physicalAuthority); err != nil {
		return recovery.Plan{}, err
	}
	if options.reloadPlan == nil {
		return plan, nil
	}
	current, err := options.reloadPlan(ctx, loadOptions)
	if err != nil {
		return recovery.Plan{}, fmt.Errorf("reload recovery plan before effects: %w", err)
	}
	if err := requireSameActiveJournal(plan, current, "before effects"); err != nil {
		return recovery.Plan{}, err
	}
	if current.Blocked() || current.HasErrors() {
		return recovery.Plan{}, fmt.Errorf("recovery is blocked by current evidence")
	}
	if !plan.SameExecutionAuthority(current) {
		return recovery.Plan{}, fmt.Errorf("recovery execution authority changed before effects")
	}
	return current, nil
}

func requireSameActiveJournal(
	expected recovery.Plan,
	current recovery.Plan,
	phase string,
) error {
	if current.OperationID() != expected.OperationID() ||
		current.OperationDir() != expected.OperationDir() {
		return fmt.Errorf("active recovery operation changed %s", phase)
	}
	expectedJournal, err := expected.JournalAuthorityFingerprint()
	if err != nil {
		return fmt.Errorf("fingerprint expected recovery journal: %w", err)
	}
	currentJournal, err := current.JournalAuthorityFingerprint()
	if err != nil {
		return fmt.Errorf("fingerprint current recovery journal: %w", err)
	}
	if currentJournal != expectedJournal {
		return fmt.Errorf("durable recovery journal changed %s", phase)
	}
	return nil
}

func executeRecoveryRollbackEffects(
	ctx context.Context,
	plan recovery.Plan,
	paths Paths,
	authority *mutationAuthority,
	registryStore ownershipmutation.RegistryStore,
	beforeHostAction func(int) error,
	codecs aggregate.CodecCatalog,
	gate visibilityEffectGate,
) error {
	actions := plan.Actions()
	hostActions := make([]recoveryHostAction, 0, len(actions))
	for _, action := range actions {
		switch action.Kind {
		case recovery.ActionKindNoOp:
			continue
		case recovery.ActionKindRestoreWrite:
			hostActions = append(hostActions, recoveryHostAction{
				Kind:                recovery.ActionKindRestoreWrite,
				Scope:               action.Scope,
				Destination:         action.Destination,
				ContentPath:         action.ContentPath,
				BackupPath:          action.BackupPath,
				BackupHash:          action.BackupHash,
				BackupKind:          action.BackupKind,
				BeforePathMode:      action.BeforePathMode,
				BeforePathExisted:   action.BeforePathExisted,
				BeforeParentExisted: action.BeforeParentExisted,
				ExpectedAfter:       action.ExpectedAfter.Clone(),
				AggregateContract:   action.AggregateContract,
			})
		case recovery.ActionKindRestoreDelete:
			hostActions = append(hostActions, recoveryHostAction{
				Kind:                recovery.ActionKindRestoreDelete,
				Scope:               action.Scope,
				Destination:         action.Destination,
				ContentPath:         action.ContentPath,
				BeforePathMode:      action.BeforePathMode,
				BeforePathExisted:   action.BeforePathExisted,
				BeforeParentExisted: action.BeforeParentExisted,
				ExpectedAfter:       action.ExpectedAfter.Clone(),
				AggregateContract:   action.AggregateContract,
			})
		case recovery.ActionKindError:
			return fmt.Errorf("recovery plan contains error action: %s", action.Reason)
		default:
			return fmt.Errorf("unsupported recovery action %q for %q", action.Kind, action.Destination)
		}
	}
	hostActions = orderRecoveryHostActions(hostActions)

	if hasProjectRecoveryAction(hostActions) {
		if authority.capturedRoot == nil {
			if err := authority.captureProjectRoot(paths, nil); err != nil {
				return err
			}
		}
		provenance, err := authority.projectAuthority.Provenance()
		if err != nil {
			return fmt.Errorf("derive recovery project root provenance: %w", err)
		}
		canonicalProvenance, err := recovery.NewProjectRootProvenance(
			provenance.PhysicalRoot(),
			provenance.ObjectFingerprint(),
			provenance.MountFingerprint(),
		)
		if err != nil {
			return fmt.Errorf("canonicalize recovery project root provenance: %w", err)
		}
		if err := plan.MatchProjectRootProvenance(canonicalProvenance); err != nil {
			return fmt.Errorf("match recovery project root authority: %w", err)
		}
	}

	rollback, err := stageRecoveryRollback(ctx, authority, hostActions, codecs)
	if err != nil {
		return err
	}

	if err := executeRecoveryHostActions(
		ctx,
		plan.OperationDir(),
		authority,
		hostActions,
		rollback.entries,
		beforeHostAction,
		codecs,
		gate,
	); err != nil {
		rollbackErr := rollback.restore(context.WithoutCancel(ctx), authority, gate)
		cleanupErr := rollback.cleanup()
		return recoveryRollbackFailure(err, rollbackErr, cleanupErr)
	}
	if err := ctx.Err(); err != nil {
		rollbackErr := rollback.restore(context.WithoutCancel(ctx), authority, gate)
		cleanupErr := rollback.cleanup()
		return recoveryRollbackFailure(err, rollbackErr, cleanupErr)
	}
	if err := rollbackClaimsToBefore(ctx, registryStore, plan.ClaimTransitions(), gate); err != nil {
		return errors.Join(
			fmt.Errorf("rollback recovery ownership claims: %w; recovery journal retained", err),
			rollback.cleanup(),
		)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(
			fmt.Errorf("%w; recovery writes committed; recovery journal retained", err),
			rollback.cleanup(),
		)
	}
	if err := rollback.cleanup(); err != nil {
		return fmt.Errorf("cleanup recovery rollback stage: %w; recovery journal retained", err)
	}
	return nil
}

func recoveryRollbackFailure(primary error, rollbackErr error, cleanupErr error) error {
	result := primary
	if rollbackErr != nil {
		result = rollbackError(result, rollbackErr)
	}
	if cleanupErr != nil {
		result = errors.Join(
			result,
			fmt.Errorf("cleanup recovery rollback stage: %w; recovery journal retained", cleanupErr),
		)
	}
	return result
}

func validateRecoveryBeforeEffects(
	ctx context.Context,
	options RecoveryOptions,
	authority mutation.PhysicalAuthoritySet,
) error {
	if options.ValidateBeforeEffects == nil {
		return nil
	}
	return options.ValidateBeforeEffects(ctx, authority)
}

func recoveryVisibilityGate(options RecoveryOptions) visibilityEffectGate {
	return visibilityEffectGate{
		before: options.ValidateVisibilityAuthority,
		after:  options.AcceptVisibilityChanges,
	}
}
