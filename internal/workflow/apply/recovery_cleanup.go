package apply

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/recoverygate"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	"github.com/isty2e/daem/internal/workflow/workspacecleanup"
)

var errRecoveryCleanupFailed = errors.New("approved recovery cleanup did not complete")

type RecoveryCleanupStep = workspacecleanup.Step

func canPlanRecoveryCleanup(input CommandInput, paths daempaths.Paths, lockfile string) bool {
	return input.nativeReplacement == nil && lockfile == paths.LockfilePath
}

func recoveryCleanupPlanningError(fence, proof error) error {
	if proof == nil || !workspacecleanup.MayQualify(fence) {
		return proof
	}
	if recoverygate.IsCancellation(proof) || recoverygate.IsGlobalMetadataRefusal(proof) {
		return proof
	}
	return errors.Join(fence, fmt.Errorf("verify recovery cleanup: %w", proof))
}

func planDryRunWithRecoveryCleanup(ctx context.Context, input CommandInput, paths daempaths.Paths, strict commandPlan, fence error) (_ DryRunPlan, returnErr error) {
	barrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return newDryRunPlan(strict), err
	}
	root, err := captureProjectRootAuthorityBeforeLoad(paths)
	if err != nil {
		return newDryRunPlan(strict), err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	declarations, err := captureDeclarationRevisions(ctx, paths.ManifestPath, paths.LockfilePath)
	if err != nil {
		return newDryRunPlan(strict), recoveryCleanupPlanningError(fence, err)
	}
	pending, err := workspacecleanup.Observe(ctx, paths, barrier, fence)
	if err != nil {
		return newDryRunPlan(strict), recoveryCleanupPlanningError(fence, err)
	}
	planned, err := planReadinessAtPathsWithBarrierValidation(ctx, input, reconcile.ContextDryRun, paths, &barrier, pending.Validate)
	if err != nil {
		return newDryRunPlan(planned), err
	}
	if err := rejectLocalSourceMutationOverlap(planned); err != nil {
		return newDryRunPlan(planned), err
	}
	if err := rejectUnsupportedPreviewActions(planned.assessment); err != nil {
		return newDryRunPlan(planned), err
	}

	// Get the same location facts as write planning, without retaining a
	// project-root capability in the preview or allowing a write.
	planned.projectRoot = root
	authority, err := buildApplyAuthorityEvidence(ctx, planned)
	planned.projectRoot = nil
	if err != nil {
		return newDryRunPlan(planned), err
	}
	if err := sealRecoveryCleanup(ctx, &planned, pending, authority); err != nil {
		return newDryRunPlan(planned), err
	}
	if err := newApplyExecutionGuard(declarations, false).requireDeclarationsCurrent(ctx, "seal apply cleanup preview"); err != nil {
		planned.result.RecoveryCleanup = nil
		return newDryRunPlan(planned), err
	}
	if err := root.ValidateSelection(paths.ManifestRoot); err != nil {
		planned.result.RecoveryCleanup = nil
		return newDryRunPlan(planned), err
	}
	planned.cleanup = nil
	return newDryRunPlan(planned), nil
}

func sealRecoveryCleanup(ctx context.Context, planned *commandPlan, pending *workspacecleanup.Pending, authority applyAuthorityEvidence) error {
	if planned.result.Reconciliation.HasErrors() || planned.result.Reconciliation.IsProspective() {
		return fmt.Errorf("recovery cleanup requires an executable apply plan")
	}
	paths := planned.context.Paths
	protected, err := localEntityArtifactSourceAuthorityPaths(paths, planned.context.RuntimeEnvironment)
	if err != nil {
		return err
	}
	var carrierContext extensiontopology.LocalSourceContext
	for _, extension := range planned.context.RuntimeEnvironment.Extensions() {
		source, err := extensiontopology.InterpretCarrierSource(extension.CarrierKey())
		if err != nil {
			return err
		}
		if source.Class() != extensiontopology.CarrierSourceLocal {
			continue
		}
		if carrierContext.BaseRoot() == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			carrierContext, err = extensiontopology.NewLocalSourceContext(paths.ManifestRoot, home)
			if err != nil {
				return err
			}
		}
		identity, err := source.ResolveLocal(carrierContext)
		if err != nil {
			return err
		}
		protected = append(protected, identity.Path())
	}
	protected = append(protected, planned.assessment.StatePath)
	hostPaths, err := hostMutationPaths(*planned)
	if err != nil {
		return err
	}
	protected = append(protected, hostPaths...)
	for _, requests := range [][]mutation.PhysicalAuthorityRequest{authority.documentWriteRequests, authority.metadataRequests} {
		for _, request := range requests {
			protected = append(protected, request.Path)
		}
	}

	// A no-op still relies on known managed destinations remaining intact.
	resolver := destinationResolver(paths)
	for _, known := range planned.assessment.CurrentState.ManagedPaths() {
		path, err := resolver.Resolve(known.Destination(), planned.context.physicalBudget)
		if err != nil {
			return err
		}
		protected = append(protected, path)
	}
	for _, known := range planned.assessment.CurrentState.ManagedAggregates() {
		path, err := resolver.Resolve(known.Contribution().AggregateRoot(), planned.context.physicalBudget)
		if err != nil {
			return err
		}
		protected = append(protected, path)
	}
	cleanup, err := pending.Seal(ctx, protected)
	if err != nil {
		return err
	}
	planned.cleanup = cleanup
	planned.result.RecoveryCleanup = cleanup.Steps()
	return nil
}
