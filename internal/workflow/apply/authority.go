package apply

import (
	"context"
	"fmt"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

const delegateRouteFamilyPrefix = "delegate-runner."

type applyAuthorityEvidence struct {
	domains []mutation.Domain
	// Declaration revisions are owned by PreparedWrite's bounded witness.
	firstEffectRevisions []mutation.RevisionRequest
	facts                []operationplan.Fact
	authorityFingerprint mutation.OperationFingerprint
}

func buildApplyAuthorityEvidence(ctx context.Context, planned commandPlan) (applyAuthorityEvidence, error) {
	projectRoot, err := projectRootFingerprint(planned)
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
	barrierFingerprint, err := planned.barrier.IdentityFingerprint()
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
	result := planned.result
	builder := operationplan.NewBuilder(
		operationplan.RevisionsFirstEffect,
		[]string{result.ManifestPath, result.LockfilePath},
		0,
	)
	physicalOccupancies := make(physicalOccupancyIndex)
	if err := builder.AddLogicalPair(result.ManifestPath, mutation.AccessShared, mutation.AccessShared); err != nil {
		return applyAuthorityEvidence{}, err
	}
	if err := builder.AddLogicalPair(result.LockfilePath, mutation.AccessShared, mutation.AccessShared); err != nil {
		return applyAuthorityEvidence{}, err
	}
	if err := builder.AddLogicalPair(planned.assessment.StatePath, mutation.AccessExclusive, mutation.AccessShared); err != nil {
		return applyAuthorityEvidence{}, err
	}
	for _, path := range []struct {
		value  string
		access mutation.AccessMode
	}{
		{value: planned.context.Paths.RecoveryDir, access: mutation.AccessExclusive},
		{value: planned.context.Paths.StateDir, access: mutation.AccessShared},
	} {
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			if err := builder.AddFingerprintOnly(
				operationplan.FactRecoveryBarrier,
				path.value,
				path.access,
				effect,
				"",
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}
	builder.AddDomains(planned.barrier.Domains())
	for _, revision := range planned.barrier.RevisionRequests() {
		builder.AddRevision(revision)
	}
	metadataTransactionPath, err := fileset.FileSetAuthorityPath(planned.context.Paths.StateDir)
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
	if err := builder.AddLogical(metadataTransactionPath, mutation.AccessExclusive, mutation.PathEffectDirectoryEntry); err != nil {
		return applyAuthorityEvidence{}, err
	}
	for _, decision := range planned.assessment.Reconciliation.MutatingManagedPaths() {
		if !decision.InvolvesScope(target.ScopeGlobal) {
			continue
		}
		if err := builder.AddLogicalPair(
			planned.context.Paths.OwnershipRegistryPath,
			mutation.AccessExclusive,
			mutation.AccessExclusive,
		); err != nil {
			return applyAuthorityEvidence{}, err
		}
		break
	}
	localSources, err := localEntityArtifactSourceAuthorityPaths(
		planned.context.Paths,
		planned.context.RuntimeEnvironment,
	)
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
	for _, path := range localSources {
		if err := builder.AddLogicalPair(path, mutation.AccessShared, mutation.AccessShared); err != nil {
			return applyAuthorityEvidence{}, err
		}
	}
	for _, authority := range relationAuthorityPathFacts(
		planned.assessment.Reconciliation.CarrierAbsences(),
		planned.assessment.RelationObservations.AuthorityPaths(),
	) {
		if err := builder.AddPhysicalPair(
			authority.path,
			authority.access,
			string(authority.target),
			string(authority.scope),
		); err != nil {
			return applyAuthorityEvidence{}, err
		}
	}
	for _, constraint := range planned.context.Lockfile.Locked.OrderConstraints() {
		selectedTarget, _, admitted := profile.ExtensionOrderCapabilityForClass(
			constraint.ClassID(),
		)
		if !admitted || !planned.assessment.SelectedTargets.Contains(selectedTarget) {
			continue
		}
		authorities, err := relationhost.OrderAuthorityPaths(relationhost.OrderInput{
			Paths:      planned.context.Paths,
			Lockfile:   planned.context.Lockfile,
			Constraint: constraint,
		})
		if err != nil {
			return applyAuthorityEvidence{}, fmt.Errorf(
				"derive extension order authority for class %q: %w",
				constraint.ClassID(),
				err,
			)
		}
		for _, authority := range authorities {
			if err := builder.AddPhysicalPair(
				authority.Path(),
				mutation.AccessExclusive,
				string(authority.Target()),
				string(authority.Scope()),
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}

	addManagedPath := func(
		scope target.Scope,
		destination output.Destination,
		consumers []target.Target,
		kind physicalOccupancyKind,
		aggregateTarget target.Target,
	) error {
		if err := observeProjectDestinationAuthorityFor(ctx, planned, scope, destination); err != nil {
			return err
		}
		path, err := projectDestinationAuthorityPathFor(planned, scope, destination)
		if err != nil {
			return err
		}
		if err := physicalOccupancies.register(path, physicalOccupancy{
			scope: scope, destination: destination, kind: kind, target: aggregateTarget,
		}); err != nil {
			return err
		}
		for _, consumer := range consumers {
			if err := builder.AddPhysicalPair(
				path,
				mutation.AccessExclusive,
				string(consumer),
				string(scope),
			); err != nil {
				return err
			}
		}
		return nil
	}
	for _, decision := range planned.assessment.Reconciliation.MutatingManagedPaths() {
		consumers := decision.ConsumerTargets()
		previous, hasPrevious := decision.PreviousState()
		if len(consumers) == 0 && hasPrevious {
			consumers = previous.ConsumerTargets()
		}
		if err := addManagedPath(
			decision.Scope(), decision.Destination(), consumers, physicalOccupancyWholePath, "",
		); err != nil {
			return applyAuthorityEvidence{}, err
		}
		if hasPrevious && (previous.Scope() != decision.Scope() || previous.Destination() != decision.Destination()) {
			if err := addManagedPath(
				previous.Scope(), previous.Destination(), previous.ConsumerTargets(), physicalOccupancyWholePath, "",
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}
	for _, decision := range planned.assessment.Reconciliation.MutatingAggregates() {
		document := decision.DocumentAddress()
		if err := addManagedPath(
			document.Scope(),
			document.AggregateRoot(),
			[]target.Target{document.Target()},
			physicalOccupancyAggregate,
			document.Target(),
		); err != nil {
			return applyAuthorityEvidence{}, err
		}
		for _, precondition := range decision.OperationPreconditions() {
			document := precondition.DocumentAddress()
			if err := addManagedPath(
				document.Scope(),
				document.AggregateRoot(),
				[]target.Target{document.Target()},
				physicalOccupancyAggregate,
				document.Target(),
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}
	carrierRegistryAdded := false
	for _, action := range planned.assessment.Reconciliation.Relations() {
		if action.Scope() == target.ScopeGlobal && !carrierRegistryAdded {
			if err := builder.AddLogicalPair(
				planned.context.Paths.CarrierClaimRegistryPath,
				mutation.AccessExclusive,
				mutation.AccessExclusive,
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
			carrierRegistryAdded = true
		}
		if action.InvokesHostRoute() {
			if err := builder.AddRoute(
				string(action.Target()),
				string(action.Scope()),
				action.RouteRequest().RouteID(),
				mutation.RouteContainmentUnknown,
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}
	for _, prerequisite := range planned.assessment.MCPProviders {
		action, present, err := prerequisite.InstallAction()
		if err != nil {
			return applyAuthorityEvidence{}, fmt.Errorf("derive MCP provider route authority: %w", err)
		}
		if !present {
			continue
		}
		if err := builder.AddRoute(
			string(action.Target()),
			string(action.Scope()),
			action.RouteRequest().RouteID(),
			mutation.RouteContainmentUnknown,
		); err != nil {
			return applyAuthorityEvidence{}, err
		}
	}
	for _, action := range planned.assessment.Reconciliation.CarrierAbsences() {
		if action.Scope() == target.ScopeGlobal && action.RetiresClaim() && !carrierRegistryAdded {
			if err := builder.AddLogicalPair(
				planned.context.Paths.CarrierClaimRegistryPath,
				mutation.AccessExclusive,
				mutation.AccessExclusive,
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
			carrierRegistryAdded = true
		}
		if request, present := action.HostRouteRequest(); present {
			if err := builder.AddRoute(
				string(action.Target()),
				string(action.Scope()),
				request.RouteID(),
				mutation.RouteContainmentUnknown,
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}
	for _, action := range result.Reconciliation.Delegates() {
		if action.SchedulesAttempt() {
			family := delegateRouteFamilyPrefix + string(action.Plan().Runner().Kind())
			if err := builder.AddRoute(
				string(action.Target()),
				string(action.Scope()),
				family,
				mutation.RouteContainmentUnknown,
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}

	plan := builder.Compile()
	var root *operationplan.RootIdentity
	if projectRoot != nil {
		root = &operationplan.RootIdentity{
			PhysicalRoot:         projectRoot.PhysicalRoot,
			AuthorityFingerprint: projectRoot.AuthorityFingerprint,
		}
	}
	fingerprint, err := operationplan.ApplyAuthorityFingerprint(plan, root, barrierFingerprint)
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
	return applyAuthorityEvidence{
		domains:              plan.Domains(),
		firstEffectRevisions: plan.Revisions(),
		facts:                plan.Facts(),
		authorityFingerprint: fingerprint,
	}, nil
}
