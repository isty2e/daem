package adopt

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type importObservedPath struct {
	path   string
	effect mutation.PathEffect
}

// ExecuteCommandPlan revalidates and writes an import candidate under one complete lease set.
func ExecuteCommandPlan(ctx context.Context, optimistic CommandPlan) (result CommandPlan, returnErr error) {
	if ctx == nil {
		return CommandPlan{}, fmt.Errorf("import context is required")
	}
	if err := ctx.Err(); err != nil {
		return CommandPlan{}, err
	}
	optimisticFingerprint, err := importPlanFingerprint(optimistic.plan)
	if err != nil {
		return CommandPlan{}, err
	}
	domains, revisionRequests, stableRevisionRequests, err := importMutationEvidence(optimistic.plan)
	if err != nil {
		return CommandPlan{}, err
	}
	paths, err := daempaths.Resolve(optimistic.plan.Output())
	if err != nil {
		return CommandPlan{}, err
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		return CommandPlan{}, err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return CommandPlan{}, err
	}
	defer func() {
		if err := leases.Release(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return CommandPlan{}, err
	} else if !matches {
		return CommandPlan{}, mutation.StaleSnapshotError{}
	}
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		return CommandPlan{}, err
	}

	revisions, err := mutation.CaptureRevisionSet(ctx, revisionRequests...)
	if err != nil {
		return CommandPlan{}, err
	}
	stableRevisions, err := mutation.CaptureRevisionSet(ctx, stableRevisionRequests...)
	if err != nil {
		return CommandPlan{}, err
	}
	currentPlan, err := BuildPlan(ctx, optimistic.request)
	if err != nil {
		return CommandPlan{}, err
	}
	currentFingerprint, err := importPlanFingerprint(currentPlan)
	if err != nil {
		return CommandPlan{}, err
	}
	if !optimisticFingerprint.Equal(currentFingerprint) {
		return CommandPlan{}, mutation.StaleSnapshotError{}
	}
	currentDomains, currentRequests, currentStableRequests, err := importMutationEvidence(currentPlan)
	if err != nil {
		return CommandPlan{}, err
	}
	if len(currentDomains) != len(domains) ||
		!equalImportRevisionRequests(currentRequests, revisionRequests) ||
		!equalImportRevisionRequests(currentStableRequests, stableRevisionRequests) {
		return CommandPlan{}, mutation.StaleSnapshotError{}
	}
	matches, err := revisions.MatchesCurrent(ctx)
	if err != nil {
		return CommandPlan{}, err
	}
	if !matches {
		return CommandPlan{}, mutation.StaleSnapshotError{}
	}
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return CommandPlan{}, err
	} else if !matches {
		return CommandPlan{}, mutation.StaleSnapshotError{}
	}

	validateStable := func() error {
		matches, err := leases.DomainsMatchCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		matches, err = stableRevisions.MatchesCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		return nil
	}
	if err := writePlan(ctx, currentPlan, validateStable); err != nil {
		return CommandPlan{}, err
	}
	return CommandPlan{request: optimistic.request, plan: currentPlan}, nil
}

func importPlanFingerprint(plan adoptmodel.Plan) (mutation.OperationFingerprint, error) {
	canonical, err := plan.IdentityBytes()
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint import plan: %w", err)
	}
	return mutation.NewOperationFingerprint(canonical), nil
}

func importMutationEvidence(plan adoptmodel.Plan) ([]mutation.Domain, []mutation.RevisionRequest, []mutation.RevisionRequest, error) {
	if err := plan.Validate(); err != nil {
		return nil, nil, nil, err
	}
	domains := make([]mutation.Domain, 0)
	observed := make(map[string]importObservedPath)
	stableObserved := make(map[string]importObservedPath)
	addObserved := func(destination map[string]importObservedPath, path string, effect mutation.PathEffect) {
		destination[importObservedPathKey(path, effect)] = importObservedPath{path: path, effect: effect}
	}
	addLogical := func(path string, access mutation.AccessMode, effect mutation.PathEffect, stable bool) error {
		domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: access, Effect: effect})
		if err != nil {
			return err
		}
		domains = append(domains, domain)
		addObserved(observed, path, effect)
		if stable {
			addObserved(stableObserved, path, effect)
		}
		return nil
	}
	addPhysical := func(path string, target string, scope string, effect mutation.PathEffect) error {
		domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{
			Path: path, Access: mutation.AccessShared, Effect: effect, Target: target, Scope: scope,
		})
		if err != nil {
			return err
		}
		domains = append(domains, domain)
		addObserved(observed, path, effect)
		addObserved(stableObserved, path, effect)
		return nil
	}

	if err := addLogical(plan.Output(), mutation.AccessExclusive, mutation.PathEffectDirectoryEntry, true); err != nil {
		return nil, nil, nil, err
	}
	if err := addLogical(plan.Output(), mutation.AccessShared, mutation.PathEffectReferent, true); err != nil {
		return nil, nil, nil, err
	}
	paths, err := daempaths.Resolve(plan.Output())
	if err != nil {
		return nil, nil, nil, err
	}
	metadataTransactionPath, err := transaction.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := addLogical(
		metadataTransactionPath,
		mutation.AccessExclusive,
		mutation.PathEffectDirectoryEntry,
		true,
	); err != nil {
		return nil, nil, nil, err
	}
	for _, source := range plan.Sources() {
		if err := addLogical(source.SourcePath, mutation.AccessExclusive, mutation.PathEffectDirectoryEntry, false); err != nil {
			return nil, nil, nil, err
		}
		if err := addPhysical(source.LivePath, string(source.Target), string(source.Scope), mutation.PathEffectReferent); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, skill := range plan.Skills() {
		if err := addLogical(skill.SourcePath, mutation.AccessExclusive, mutation.PathEffectDirectoryEntry, false); err != nil {
			return nil, nil, nil, err
		}
		for _, route := range skill.SourceRoutes {
			if err := addPhysical(route.LivePath, string(route.Target), string(skill.Scope), mutation.PathEffectDirectoryEntry); err != nil {
				return nil, nil, nil, err
			}
			if err := addPhysical(route.ReadPath, string(route.Target), string(skill.Scope), mutation.PathEffectReferent); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	for _, hook := range plan.Hooks() {
		if err := addPhysical(hook.LivePath, string(hook.Target), string(hook.Scope), mutation.PathEffectReferent); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, server := range plan.MCPServers() {
		if err := addPhysical(server.LivePath, string(server.Target), string(server.Scope), mutation.PathEffectReferent); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, scan := range plan.Scans() {
		if err := addPhysical(scan.LivePath, string(scan.Target), string(scan.Scope), mutation.PathEffectReferent); err != nil {
			return nil, nil, nil, err
		}
	}
	return domains, sortedImportRevisionRequests(observed), sortedImportRevisionRequests(stableObserved), nil
}

func sortedImportRevisionRequests(observed map[string]importObservedPath) []mutation.RevisionRequest {
	keys := make([]string, 0, len(observed))
	for key := range observed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	revisions := make([]mutation.RevisionRequest, 0, len(keys))
	for _, key := range keys {
		path := observed[key]
		revisions = append(revisions, mutation.RevisionRequest{Path: path.path, Effect: path.effect})
	}
	return revisions
}

func importObservedPathKey(path string, effect mutation.PathEffect) string {
	return strconv.Itoa(int(effect)) + ":" + path
}

func equalImportRevisionRequests(left []mutation.RevisionRequest, right []mutation.RevisionRequest) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
