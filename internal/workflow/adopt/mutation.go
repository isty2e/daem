package adopt

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/assurance/observe/filesnapshot"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type importObservedPath struct {
	request       mutation.RevisionRequest
	authoritative bool
}

// ExecuteCommandPlan revalidates and writes an import candidate under one complete lease set.
func ExecuteCommandPlan(ctx context.Context, optimistic CommandPlan) (result CommandPlan, returnErr error) {
	return executeCommandPlan(ctx, optimistic, BuildPlan)
}

func executeCommandPlan(
	ctx context.Context,
	optimistic CommandPlan,
	buildCurrentPlan func(context.Context, adoptmodel.Request) (adoptmodel.Plan, error),
) (result CommandPlan, returnErr error) {
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
	if err := validateMCPSourceAuthoritiesCurrent(ctx, optimistic.plan); err != nil {
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
	selectorMembership, err := captureSelectorSkillMembershipWitness(ctx, optimistic.plan)
	if err != nil {
		return CommandPlan{}, err
	}
	currentPlan, err := buildCurrentPlan(ctx, optimistic.request)
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
	if matches, err := selectorMembership.MatchesCurrent(ctx); err != nil {
		return CommandPlan{}, err
	} else if !matches {
		return CommandPlan{}, mutation.StaleSnapshotError{}
	}
	if err := validateMCPSourceAuthoritiesCurrent(ctx, currentPlan); err != nil {
		return CommandPlan{}, err
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
		matches, err = selectorMembership.MatchesCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		if err := validateMCPSourceAuthoritiesCurrent(ctx, currentPlan); err != nil {
			return err
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
	physicalDomains := make(map[string]struct{})
	externallyValidated := make(map[string]struct{})
	addObserved := func(
		destination map[string]importObservedPath,
		key string,
		request mutation.RevisionRequest,
		authoritative bool,
	) error {
		if existing, exists := destination[key]; exists {
			switch {
			case existing.request.Equal(request):
				return nil
			case existing.authoritative && !authoritative:
				return nil
			case !existing.authoritative && authoritative:
				destination[key] = importObservedPath{request: request, authoritative: true}
				return nil
			default:
				return fmt.Errorf("import path %q carries conflicting revision semantics", request.Path)
			}
		}
		destination[key] = importObservedPath{request: request, authoritative: authoritative}
		return nil
	}
	addLogicalDomain := func(path string, access mutation.AccessMode, effect mutation.PathEffect) error {
		domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: access, Effect: effect})
		if err != nil {
			return err
		}
		domains = append(domains, domain)
		return nil
	}
	addLogical := func(path string, access mutation.AccessMode, effect mutation.PathEffect, stable bool) error {
		if err := addLogicalDomain(path, access, effect); err != nil {
			return err
		}
		request := mutation.RevisionRequest{Path: path, Effect: effect}
		key := importObservedPathKey(path, effect)
		if err := addObserved(observed, key, request, false); err != nil {
			return err
		}
		if stable {
			if err := addObserved(stableObserved, key, request, false); err != nil {
				return err
			}
		}
		return nil
	}
	ensurePhysicalDomain := func(
		path string,
		target string,
		scope string,
		effect mutation.PathEffect,
	) error {
		key := target + "\x00" + scope + "\x00" + importObservedPathKey(path, effect)
		if _, exists := physicalDomains[key]; exists {
			return nil
		}
		domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{
			Path: path, Access: mutation.AccessShared, Effect: effect, Target: target, Scope: scope,
		})
		if err != nil {
			return err
		}
		physicalDomains[key] = struct{}{}
		domains = append(domains, domain)
		return nil
	}
	addPhysicalRevision := func(
		path string,
		target string,
		scope string,
		effect mutation.PathEffect,
		revisionKey string,
		request mutation.RevisionRequest,
		authoritative bool,
	) error {
		if err := ensurePhysicalDomain(path, target, scope, effect); err != nil {
			return err
		}
		if err := addObserved(observed, revisionKey, request, authoritative); err != nil {
			return err
		}
		if err := addObserved(stableObserved, revisionKey, request, authoritative); err != nil {
			return err
		}
		return nil
	}
	addPhysical := func(path string, target string, scope string, effect mutation.PathEffect) error {
		request := mutation.RevisionRequest{Path: path, Effect: effect}
		return addPhysicalRevision(
			path,
			target,
			scope,
			effect,
			importObservedPathKey(path, effect),
			request,
			false,
		)
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
	_, selectorMembershipRequired, err := selectorSkillMergeEnvironment(plan)
	if err != nil {
		return nil, nil, nil, err
	}
	if selectorMembershipRequired {
		if err := addLogicalDomain(
			paths.LockfilePath,
			mutation.AccessShared,
			mutation.PathEffectDirectoryEntry,
		); err != nil {
			return nil, nil, nil, err
		}
		if err := addLogicalDomain(
			paths.LockfilePath,
			mutation.AccessShared,
			mutation.PathEffectReferent,
		); err != nil {
			return nil, nil, nil, err
		}
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
	}
	for _, authority := range plan.SkillSourceAuthorities() {
		for _, route := range authority.Routes {
			if err := addPhysical(route.LivePath, string(route.Target), string(authority.Scope), mutation.PathEffectDirectoryEntry); err != nil {
				return nil, nil, nil, err
			}
			if err := addPhysical(route.ReadPath, string(route.Target), string(authority.Scope), mutation.PathEffectReferent); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	for _, hook := range plan.Hooks() {
		if err := addPhysical(hook.LivePath, string(hook.Target), string(hook.Scope), mutation.PathEffectReferent); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, authority := range plan.MCPSourceAuthorities() {
		if err := ensurePhysicalDomain(
			authority.Route.PrimaryPath,
			string(authority.Target),
			string(authority.Scope),
			mutation.PathEffectReferent,
		); err != nil {
			return nil, nil, nil, err
		}
		externallyValidated[importObservedPathKey(
			authority.Route.PrimaryPath,
			mutation.PathEffectReferent,
		)] = struct{}{}
		for _, requiredAbsentPath := range authority.Route.RequiredAbsentPaths {
			if err := addPhysicalRevision(
				requiredAbsentPath,
				string(authority.Target),
				string(authority.Scope),
				mutation.PathEffectDirectoryEntry,
				importObservedPathKey(requiredAbsentPath, mutation.PathEffectDirectoryEntry),
				mutation.NewRequiredAbsentRevisionRequest(requiredAbsentPath),
				true,
			); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	for _, scan := range plan.Scans() {
		if err := addPhysical(scan.LivePath, string(scan.Target), string(scan.Scope), mutation.PathEffectReferent); err != nil {
			return nil, nil, nil, err
		}
	}
	for key := range externallyValidated {
		delete(observed, key)
		delete(stableObserved, key)
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
		revisions = append(revisions, observed[key].request)
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
		if !left[index].Equal(right[index]) {
			return false
		}
	}
	return true
}

func validateMCPSourceAuthoritiesCurrent(ctx context.Context, plan adoptmodel.Plan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	type primarySource struct {
		maximumBytes int64
		revision     string
	}
	primarySources := make(map[string]primarySource)
	requiredAbsent := make(map[string]struct{})
	for _, authority := range plan.MCPSourceAuthorities() {
		primarySources[authority.Route.PrimaryPath] = primarySource{
			maximumBytes: authority.Route.MaximumBytes,
			revision:     authority.Route.PrimaryRevision,
		}
		for _, path := range authority.Route.RequiredAbsentPaths {
			requiredAbsent[path] = struct{}{}
		}
	}
	primaryPaths := make([]string, 0, len(primarySources))
	for path := range primarySources {
		primaryPaths = append(primaryPaths, path)
	}
	sort.Strings(primaryPaths)
	for _, path := range primaryPaths {
		if err := ctx.Err(); err != nil {
			return err
		}
		expected := primarySources[path]
		snapshot, exists, err := filesnapshot.ReadRegularFileSnapshotContext(
			ctx,
			path,
			expected.maximumBytes,
		)
		if err != nil {
			if errors.Is(err, filesnapshot.ErrSymlink) ||
				errors.Is(err, filesnapshot.ErrNotRegular) ||
				errors.Is(err, filesnapshot.ErrLimitExceeded) ||
				errors.Is(err, filesnapshot.ErrChanged) {
				return mutation.StaleSnapshotError{}
			}
			return fmt.Errorf("revalidate MCP source %q: %w", path, err)
		}
		if !exists || snapshot.Revision() != expected.revision {
			return mutation.StaleSnapshotError{}
		}
	}
	if len(requiredAbsent) == 0 {
		return nil
	}
	absentPaths := make([]string, 0, len(requiredAbsent))
	for path := range requiredAbsent {
		absentPaths = append(absentPaths, path)
	}
	sort.Strings(absentPaths)
	requests := make([]mutation.RevisionRequest, 0, len(absentPaths))
	for _, path := range absentPaths {
		requests = append(requests, mutation.NewRequiredAbsentRevisionRequest(path))
	}
	_, err := mutation.CaptureRevisionSet(ctx, requests...)
	return err
}
