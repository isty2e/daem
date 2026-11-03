package apply

import (
	"context"
	"fmt"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/target"
)

const delegateRouteFamilyPrefix = "delegate-runner."

type applyAuthorityEvidence struct {
	domains               []mutation.Domain
	documentWrites        mutation.PhysicalAuthoritySet
	documentWriteRequests []mutation.PhysicalAuthorityRequest
	metadataRequests      []mutation.PhysicalAuthorityRequest
	documentView          ownership.DocumentRegistryView
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
	registryAccess := mutation.AccessShared
	requiresRegistry := planned.nativePlan != nil
	if err := builder.AddLogicalPair(result.ManifestPath, mutation.AccessShared, mutation.AccessShared); err != nil {
		return applyAuthorityEvidence{}, err
	}
	if err := builder.AddLogicalPair(result.LockfilePath, mutation.AccessShared, mutation.AccessShared); err != nil {
		return applyAuthorityEvidence{}, err
	}
	stateReferentAccess := mutation.AccessShared
	if planned.nativePlan != nil && planned.nativePlan.opaque {
		stateReferentAccess = mutation.AccessExclusive
	}
	if err := builder.AddLogicalPair(planned.assessment.StatePath, mutation.AccessExclusive, stateReferentAccess); err != nil {
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
	if planned.nativePlan != nil {
		registryAccess = mutation.AccessExclusive
	}
	for _, decision := range planned.assessment.Reconciliation.MutatingManagedPaths() {
		if !decision.InvolvesScope(target.ScopeGlobal) {
			continue
		}
		requiresRegistry, registryAccess = true, mutation.AccessExclusive
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
	addOpaqueNamespaces := func(key desiredextension.CarrierKey, operation lock.OperationKind) error {
		profile, err := relationhost.NativeSettingsProfileFor(key, operation)
		if err != nil {
			return err
		}
		paths, err := profile.ExclusionPaths(planned.context.Paths)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if err := builder.AddDocumentWritePair(path.Path(), string(path.Target()), string(path.Scope())); err != nil {
				return err
			}
		}
		return nil
	}
	for _, action := range planned.assessment.Reconciliation.Relations() {
		if action.InvokesHostRoute() {
			if err := addOpaqueNamespaces(action.CarrierIdentity().Carrier().Key(), lock.OperationInstall); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}
	for _, action := range planned.assessment.Reconciliation.CarrierAbsences() {
		if action.InvokesHostRoute() {
			if err := addOpaqueNamespaces(action.Claim().Identity().Carrier().Key(), lock.OperationRemove); err != nil {
				return applyAuthorityEvidence{}, err
			}
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
		selectedTarget, _, admitted := hostsurfacecatalog.Product().ExtensionOrderCapabilityForClass(
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
		write bool,
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
			if write {
				if err := builder.AddDocumentWritePair(path, string(consumer), string(scope)); err != nil {
					return err
				}
				continue
			}
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
		kind, documentTarget := physicalOccupancyWholePath, target.Target("")
		if aggregate.IsHostConfigDocumentSubject(decision.Subject()) {
			if len(consumers) != 1 {
				return applyAuthorityEvidence{}, fmt.Errorf("native whole authority requires one consumer")
			}
			kind, documentTarget = physicalOccupancyAggregate, consumers[0]
		}
		if err := addManagedPath(
			decision.Scope(), decision.Destination(), consumers, kind, documentTarget, true,
		); err != nil {
			return applyAuthorityEvidence{}, err
		}
		if hasPrevious && (previous.Scope() != decision.Scope() || previous.Destination() != decision.Destination()) {
			if err := addManagedPath(
				previous.Scope(), previous.Destination(), previous.ConsumerTargets(), kind, documentTarget, true,
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
			true,
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
				false,
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}
	if planned.nativePlan != nil {
		for _, document := range planned.nativePlan.stateDocuments {
			if err := addManagedPath(document.Scope(), document.AggregateRoot(), []target.Target{document.Target()}, physicalOccupancyAggregate, document.Target(), false); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}
	var metadataRequests []mutation.PhysicalAuthorityRequest
	if planned.nativePlan != nil && planned.nativePlan.opaque {
		for _, group := range planned.assessment.NativeApproval {
			for _, document := range group.Aliases {
				if err := addManagedPath(document.Scope(), document.AggregateRoot(), []target.Target{document.Target()}, physicalOccupancyAggregate, document.Target(), false); err != nil {
					return applyAuthorityEvidence{}, err
				}
				path, err := projectDestinationAuthorityPathFor(planned, document.Scope(), document.AggregateRoot())
				if err != nil {
					return applyAuthorityEvidence{}, err
				}
				metadataRequests = append(metadataRequests, mutation.PhysicalAuthorityRequest{Path: path, Target: string(document.Target()), Scope: string(document.Scope())})
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

	requests := builder.Compile().DocumentWriteRequests()
	physical, err := mutation.NewPhysicalAuthoritySet(requests...)
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
	scope, err := recoverygate.DocumentScope(physical)
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
	view, err := ownership.EmptyRegistry().Documents(scope)
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
	if requiresRegistry || len(requests) != 0 {
		if planned.nativePlan != nil {
			view, err = planned.assessment.NativeObservedDocuments(scope)
		} else {
			view, err = recoverygate.ObserveDocumentRegistry(ctx, planned.context.Paths.OwnershipRegistryPath, physical)
		}
		if err != nil {
			return applyAuthorityEvidence{}, err
		}
		if err := view.RequireProtectionClear(); err != nil {
			return applyAuthorityEvidence{}, err
		}
		identity, err := view.Fingerprint()
		if err != nil {
			return applyAuthorityEvidence{}, err
		}
		if err := builder.AddOwnershipRegistry(planned.context.Paths.OwnershipRegistryPath, registryAccess, identity); err != nil {
			return applyAuthorityEvidence{}, err
		}
	}
	plan := builder.Compile()
	domains, err := lowerApplyAuthorityDomainSteps(plan.DomainSteps())
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
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
		domains:               domains,
		documentWrites:        physical,
		documentWriteRequests: requests,
		metadataRequests:      metadataRequests,
		documentView:          view,
		firstEffectRevisions:  plan.Revisions(),
		facts:                 plan.Facts(),
		authorityFingerprint:  fingerprint,
	}, nil
}

func lowerApplyAuthorityDomainSteps(steps []operationplan.DomainStep) ([]mutation.Domain, error) {
	domains := make([]mutation.Domain, 0, len(steps))
	for _, step := range steps {
		if domain, ok := step.Compiled(); ok {
			domains = append(domains, domain)
			continue
		}
		request, ok := step.Path()
		if !ok {
			return nil, fmt.Errorf("apply authority domain step is invalid")
		}
		if logical, logicalPath := request.Logical(); logicalPath {
			domain, err := mutation.NewLogicalPathDomain(logical)
			if err != nil {
				return nil, err
			}
			domains = append(domains, domain)
			continue
		}
		physical, physicalPath := request.Physical()
		if !physicalPath {
			return nil, fmt.Errorf("apply authority path-domain request is invalid")
		}
		domain, err := mutation.NewPhysicalPathDomain(physical)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}
