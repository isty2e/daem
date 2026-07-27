package apply

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

const delegateRouteFamilyPrefix = "delegate-runner."

type applyAuthorityEvidence struct {
	domains              []mutation.Domain
	revisions            []mutation.RevisionRequest
	authorityFingerprint mutation.OperationFingerprint
}

type applyAuthorityFingerprintFacts struct {
	Domains     []applyAuthorityFact
	ProjectRoot *projectRootFingerprintFacts
}

type applyAuthorityFact struct {
	Kind        string
	Path        string
	Access      mutation.AccessMode
	Effect      mutation.PathEffect
	Target      string
	Scope       string
	Family      string
	Containment mutation.RouteContainment
}

func buildApplyAuthorityEvidence(ctx context.Context, planned commandPlan) (applyAuthorityEvidence, error) {
	projectRoot, err := projectRootFingerprint(planned)
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
	result := planned.result
	facts := make([]applyAuthorityFact, 0)
	domains := make([]mutation.Domain, 0)
	revisions := make(map[string]mutation.RevisionRequest)
	physicalOccupancies := make(physicalOccupancyIndex)
	addPath := func(kind string, path string, access mutation.AccessMode, effect mutation.PathEffect, target string, scope string) error {
		fact := applyAuthorityFact{Kind: kind, Path: path, Access: access, Effect: effect, Target: target, Scope: scope}
		var domain mutation.Domain
		var err error
		if kind == "physical" {
			domain, err = mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{
				Path: path, Access: access, Effect: effect, Target: target, Scope: scope,
			})
		} else {
			domain, err = mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: access, Effect: effect})
		}
		if err != nil {
			return err
		}
		facts = append(facts, fact)
		domains = append(domains, domain)
		revisions[revisionRequestKey(path, effect)] = mutation.RevisionRequest{Path: path, Effect: effect}
		return nil
	}
	addLogicalPair := func(path string, entryAccess mutation.AccessMode, referentAccess mutation.AccessMode) error {
		if err := addPath("logical", path, entryAccess, mutation.PathEffectDirectoryEntry, "", ""); err != nil {
			return err
		}
		return addPath("logical", path, referentAccess, mutation.PathEffectReferent, "", "")
	}

	if err := addLogicalPair(result.ManifestPath, mutation.AccessShared, mutation.AccessShared); err != nil {
		return applyAuthorityEvidence{}, err
	}
	if err := addLogicalPair(result.LockfilePath, mutation.AccessShared, mutation.AccessShared); err != nil {
		return applyAuthorityEvidence{}, err
	}
	if err := addLogicalPair(planned.assessment.StatePath, mutation.AccessExclusive, mutation.AccessShared); err != nil {
		return applyAuthorityEvidence{}, err
	}
	if err := addLogicalPair(planned.context.Paths.RecoveryDir, mutation.AccessExclusive, mutation.AccessExclusive); err != nil {
		return applyAuthorityEvidence{}, err
	}
	metadataTransactionPath, err := transaction.FileSetAuthorityPath(planned.context.Paths.StateDir)
	if err != nil {
		return applyAuthorityEvidence{}, err
	}
	if err := addPath("logical", metadataTransactionPath, mutation.AccessExclusive, mutation.PathEffectDirectoryEntry, "", ""); err != nil {
		return applyAuthorityEvidence{}, err
	}
	for _, decision := range planned.assessment.Reconciliation.MutatingManagedPaths() {
		if !decision.InvolvesScope(target.ScopeGlobal) {
			continue
		}
		if err := addLogicalPair(planned.context.Paths.OwnershipRegistryPath, mutation.AccessExclusive, mutation.AccessExclusive); err != nil {
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
		if err := addLogicalPair(path, mutation.AccessShared, mutation.AccessShared); err != nil {
			return applyAuthorityEvidence{}, err
		}
	}
	for _, authority := range relationAuthorityPathFacts(
		planned.assessment.Reconciliation.CarrierAbsences(),
		planned.assessment.RelationObservations.AuthorityPaths(),
	) {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			if err := addPath(
				"physical",
				authority.path,
				authority.access,
				effect,
				string(authority.target),
				string(authority.scope),
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
			if err := addPath("physical", path, mutation.AccessExclusive, mutation.PathEffectDirectoryEntry, string(consumer), string(scope)); err != nil {
				return err
			}
			if err := addPath("physical", path, mutation.AccessExclusive, mutation.PathEffectReferent, string(consumer), string(scope)); err != nil {
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
	addRoute := func(target string, scope string, family string) error {
		domain, err := mutation.NewHostRouteDomain(mutation.HostRouteRequest{
			Target: target, Scope: scope, Family: family, Containment: mutation.RouteContainmentUnknown,
		})
		if err != nil {
			return err
		}
		facts = append(facts, applyAuthorityFact{
			Kind: "route", Target: target, Scope: scope, Family: family, Containment: mutation.RouteContainmentUnknown,
		})
		domains = append(domains, domain)
		return nil
	}
	carrierRegistryAdded := false
	for _, action := range planned.assessment.Reconciliation.Relations() {
		if action.Scope() == target.ScopeGlobal && !carrierRegistryAdded {
			if err := addLogicalPair(
				planned.context.Paths.CarrierClaimRegistryPath,
				mutation.AccessExclusive,
				mutation.AccessExclusive,
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
			carrierRegistryAdded = true
		}
		if action.InvokesHostRoute() {
			if err := addRoute(string(action.Target()), string(action.Scope()), action.RouteRequest().RouteID()); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}
	for _, action := range planned.assessment.Reconciliation.CarrierAbsences() {
		if action.Scope() == target.ScopeGlobal && action.RetiresClaim() && !carrierRegistryAdded {
			if err := addLogicalPair(
				planned.context.Paths.CarrierClaimRegistryPath,
				mutation.AccessExclusive,
				mutation.AccessExclusive,
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
			carrierRegistryAdded = true
		}
		if request, present := action.HostRouteRequest(); present {
			if err := addRoute(
				string(action.Target()),
				string(action.Scope()),
				request.RouteID(),
			); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}
	for _, action := range result.Reconciliation.Delegates() {
		if action.SchedulesAttempt() {
			family := delegateRouteFamilyPrefix + string(action.PlanIdentity().RunnerKind)
			if err := addRoute(string(action.Target()), string(action.Scope()), family); err != nil {
				return applyAuthorityEvidence{}, err
			}
		}
	}

	sort.Slice(facts, func(left int, right int) bool {
		return applyAuthorityFactKey(facts[left]) < applyAuthorityFactKey(facts[right])
	})
	canonical, err := json.Marshal(applyAuthorityFingerprintFacts{
		Domains:     facts,
		ProjectRoot: projectRoot,
	})
	if err != nil {
		return applyAuthorityEvidence{}, fmt.Errorf("fingerprint apply authority: %w", err)
	}
	return applyAuthorityEvidence{
		domains:              domains,
		revisions:            sortedRevisionRequests(revisions),
		authorityFingerprint: mutation.NewOperationFingerprint(canonical),
	}, nil
}
