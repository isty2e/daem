package refresh

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/operationplan"
)

type authorityEvidence struct {
	domains              []mutation.Domain
	revisions            []mutation.RevisionRequest
	authorityFingerprint mutation.OperationFingerprint
}

func validateRefreshPeerAuthority(
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
	return nil
}

func refreshAttemptPersistenceRevisionRequests(
	current plan,
) []mutation.RevisionRequest {
	selected := map[string]struct{}{
		current.paths.ManifestPath:  {},
		current.paths.LockfilePath:  {},
		current.paths.StatefilePath: {},
	}
	requests := make([]mutation.RevisionRequest, 0, len(selected)*2)
	for _, request := range current.authority.revisions {
		if _, ok := selected[request.Path]; ok {
			requests = append(requests, request)
		}
	}
	return requests
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
	rootIdentity := operationplan.RootIdentity{
		PhysicalRoot:         authority.PhysicalRoot(),
		AuthorityFingerprint: fingerprint,
	}
	barrierFingerprint, err := planned.barrier.IdentityFingerprint()
	if err != nil {
		return authorityEvidence{}, err
	}

	builder := operationplan.NewBuilder(
		operationplan.RevisionsRefreshFull,
		[]string{planned.paths.ManifestPath, planned.paths.LockfilePath},
		declarationartifact.MaximumBytes,
	)
	if err := builder.AddLogicalPair(
		planned.paths.ManifestPath,
		mutation.AccessShared,
		mutation.AccessShared,
	); err != nil {
		return authorityEvidence{}, err
	}
	if err := builder.AddLogicalPair(
		planned.paths.LockfilePath,
		mutation.AccessShared,
		mutation.AccessShared,
	); err != nil {
		return authorityEvidence{}, err
	}
	if err := builder.AddLogicalPair(
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
			if err := builder.AddFingerprintOnly(
				operationplan.FactRecoveryBarrier,
				path.value,
				path.access,
				effect,
				"",
			); err != nil {
				return authorityEvidence{}, err
			}
		}
	}
	builder.AddDomains(planned.barrier.Domains())
	for _, revision := range planned.barrier.RevisionRequests() {
		builder.AddRevision(revision)
	}
	for _, observedPath := range planned.authorityPaths {
		if err := builder.AddPhysicalPair(
			observedPath.Path(),
			mutation.AccessShared,
			string(observedPath.Target()),
			string(observedPath.Scope()),
		); err != nil {
			return authorityEvidence{}, err
		}
	}
	if err := builder.AddRoute(
		string(planned.result.Selection.Target),
		string(planned.result.Selection.Scope),
		planned.routeRequest.RouteID(),
		mutation.RouteContainmentUnknown,
	); err != nil {
		return authorityEvidence{}, err
	}

	plan := builder.Compile()
	authorityFingerprint, err := operationplan.RefreshAuthorityFingerprint(
		plan,
		rootIdentity,
		barrierFingerprint,
	)
	if err != nil {
		return authorityEvidence{}, err
	}
	return authorityEvidence{
		domains:              plan.Domains(),
		revisions:            plan.Revisions(),
		authorityFingerprint: authorityFingerprint,
	}, nil
}
