package refresh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type authorityEvidence struct {
	domains              []mutation.Domain
	revisions            []mutation.RevisionRequest
	authorityFingerprint mutation.OperationFingerprint
}

type authorityFact struct {
	Kind        string
	Path        string
	Access      mutation.AccessMode
	Effect      mutation.PathEffect
	Target      string
	Scope       string
	Family      string
	Containment mutation.RouteContainment
}

type rootFact struct {
	PhysicalRoot         string
	AuthorityFingerprint string
}

func validateBeforeHostAttempt(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	current plan,
	revisions mutation.RevisionSet,
	leases *mutation.LeaseSet,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.ValidateSelection(current.paths.ManifestRoot); err != nil {
		return errors.Join(mutation.StalePlanError{}, err)
	}
	if matches, err := revisions.MatchesCurrent(ctx); err != nil {
		return err
	} else if !matches {
		return mutation.StalePlanError{}
	}
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return err
	} else if !matches {
		return mutation.StalePlanError{}
	}
	return current.barrier.Validate(ctx)
}

func buildAuthorityEvidence(
	planned plan,
	root *rootedpath.CapturedRoot,
) (authorityEvidence, error) {
	if root == nil {
		return authorityEvidence{}, fmt.Errorf("refresh project-root witness is required")
	}
	if err := root.ValidateSelection(planned.paths.ManifestRoot); err != nil {
		return authorityEvidence{}, err
	}
	authority, err := root.Authority()
	if err != nil {
		return authorityEvidence{}, err
	}
	fingerprint, err := authority.OperationFingerprint()
	if err != nil {
		return authorityEvidence{}, err
	}
	rootIdentity := rootFact{
		PhysicalRoot:         authority.PhysicalRoot(),
		AuthorityFingerprint: fingerprint,
	}
	barrierFingerprint, err := planned.barrier.IdentityFingerprint()
	if err != nil {
		return authorityEvidence{}, err
	}

	facts := make([]authorityFact, 0)
	domains := make([]mutation.Domain, 0)
	revisions := make(map[string]mutation.RevisionRequest)
	declarationPaths := map[string]struct{}{
		planned.paths.ManifestPath: {},
		planned.paths.LockfilePath: {},
	}
	addPath := func(
		kind string,
		path string,
		access mutation.AccessMode,
		effect mutation.PathEffect,
		targetValue string,
		scopeValue string,
	) error {
		var domain mutation.Domain
		var err error
		switch kind {
		case "logical":
			domain, err = mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
				Path: path, Access: access, Effect: effect,
			})
		case "physical":
			domain, err = mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{
				Path: path, Access: access, Effect: effect,
				Target: targetValue, Scope: scopeValue,
			})
		default:
			return fmt.Errorf("unsupported refresh authority path kind %q", kind)
		}
		if err != nil {
			return err
		}
		facts = append(facts, authorityFact{
			Kind: kind, Path: path, Access: access, Effect: effect,
			Target: targetValue, Scope: scopeValue,
		})
		domains = append(domains, domain)
		revision := mutation.NewBoundedContentRevisionRequest(path, effect)
		if _, declaration := declarationPaths[path]; declaration {
			revision, err = mutation.NewBoundedFileRevisionRequest(
				declarationartifact.MaximumBytes,
				path,
				effect,
			)
			if err != nil {
				return err
			}
		}
		revisions[revisionKey(path, effect)] = revision
		return nil
	}
	addLogicalPair := func(
		path string,
		entryAccess mutation.AccessMode,
		referentAccess mutation.AccessMode,
	) error {
		if err := addPath(
			"logical",
			path,
			entryAccess,
			mutation.PathEffectDirectoryEntry,
			"",
			"",
		); err != nil {
			return err
		}
		return addPath(
			"logical",
			path,
			referentAccess,
			mutation.PathEffectReferent,
			"",
			"",
		)
	}

	if err := addLogicalPair(
		planned.paths.ManifestPath,
		mutation.AccessShared,
		mutation.AccessShared,
	); err != nil {
		return authorityEvidence{}, err
	}
	if err := addLogicalPair(
		planned.paths.LockfilePath,
		mutation.AccessShared,
		mutation.AccessShared,
	); err != nil {
		return authorityEvidence{}, err
	}
	if err := addLogicalPair(
		planned.paths.StatefilePath,
		mutation.AccessExclusive,
		mutation.AccessShared,
	); err != nil {
		return authorityEvidence{}, err
	}
	for _, path := range []struct {
		value  string
		access mutation.AccessMode
	}{
		{value: planned.paths.RecoveryDir, access: mutation.AccessExclusive},
		{value: planned.paths.StateDir, access: mutation.AccessShared},
	} {
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			facts = append(facts, authorityFact{
				Kind: "recovery_barrier", Path: path.value,
				Access: path.access, Effect: effect,
			})
		}
	}
	domains = append(domains, planned.barrier.Domains()...)
	for _, revision := range planned.barrier.RevisionRequests() {
		revisions[revisionKey(revision.Path, revision.Effect)] = revision
	}
	for _, observedPath := range planned.authorityPaths {
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			if err := addPath(
				"physical",
				observedPath.Path(),
				mutation.AccessShared,
				effect,
				string(observedPath.Target()),
				string(observedPath.Scope()),
			); err != nil {
				return authorityEvidence{}, err
			}
		}
	}
	routeDomain, err := mutation.NewHostRouteDomain(mutation.HostRouteRequest{
		Target:      string(planned.result.Selection.Target),
		Scope:       string(planned.result.Selection.Scope),
		Family:      planned.routeRequest.RouteID(),
		Containment: mutation.RouteContainmentUnknown,
	})
	if err != nil {
		return authorityEvidence{}, err
	}
	facts = append(facts, authorityFact{
		Kind:        "route",
		Target:      string(planned.result.Selection.Target),
		Scope:       string(planned.result.Selection.Scope),
		Family:      planned.routeRequest.RouteID(),
		Containment: mutation.RouteContainmentUnknown,
	})
	domains = append(domains, routeDomain)

	sort.Slice(facts, func(left int, right int) bool {
		return authorityFactKey(facts[left]) < authorityFactKey(facts[right])
	})
	canonical, err := json.Marshal(struct {
		Domains         []authorityFact
		Root            rootFact
		RecoveryBarrier string
	}{
		Domains:         facts,
		Root:            rootIdentity,
		RecoveryBarrier: barrierFingerprint,
	})
	if err != nil {
		return authorityEvidence{}, fmt.Errorf("fingerprint refresh authority: %w", err)
	}
	return authorityEvidence{
		domains:              domains,
		revisions:            sortedRevisions(revisions),
		authorityFingerprint: mutation.NewOperationFingerprint(canonical),
	}, nil
}

func revisionKey(path string, effect mutation.PathEffect) string {
	return fmt.Sprintf("%d\x00%s", effect, path)
}

func sortedRevisions(
	byKey map[string]mutation.RevisionRequest,
) []mutation.RevisionRequest {
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	revisions := make([]mutation.RevisionRequest, 0, len(keys))
	for _, key := range keys {
		revisions = append(revisions, byKey[key])
	}
	return revisions
}

func authorityFactKey(fact authorityFact) string {
	return fmt.Sprintf(
		"%s\x00%s\x00%d\x00%d\x00%s\x00%s\x00%s\x00%d",
		fact.Kind,
		fact.Path,
		fact.Access,
		fact.Effect,
		fact.Target,
		fact.Scope,
		fact.Family,
		fact.Containment,
	)
}
