package adopt

import (
	"context"
	"errors"
	"fmt"
	"sort"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

// ExecuteCommandPlan revalidates and writes an import candidate under one complete lease set.
func ExecuteCommandPlan(
	ctx context.Context,
	optimistic CommandPlan,
	progressEvents ProgressEventSink,
) (result CommandPlan, returnErr error) {
	return executeCommandPlan(
		ctx,
		optimistic,
		func(currentContext context.Context, request adoptmodel.Request) (observedImportPlan, error) {
			return buildPlan(currentContext, request, ProgressPhaseRevalidation, progressEvents)
		},
		progressEvents,
	)
}

func executeCommandPlan(
	ctx context.Context,
	optimistic CommandPlan,
	buildCurrentPlan func(context.Context, adoptmodel.Request) (observedImportPlan, error),
	progressEvents ProgressEventSink,
) (result CommandPlan, returnErr error) {
	if ctx == nil {
		return CommandPlan{}, fmt.Errorf("import context is required")
	}
	if err := ctx.Err(); err != nil {
		return CommandPlan{}, err
	}
	progressTotal := importProgressTotal(optimistic.request.Targets(), optimistic.request.Scopes())
	progressEvents.emit(ProgressEvent{
		Kind:  ProgressEventPhaseStarted,
		Phase: ProgressPhaseRevalidation,
		Total: progressTotal,
	})
	optimisticFingerprint, err := importPlanFingerprint(optimistic.plan)
	if err != nil {
		return CommandPlan{}, err
	}
	domains, revisionRequests, stableRevisionRequests, err := importMutationEvidence(optimistic.plan, optimistic.barrier)
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
	if err := optimistic.barrier.Validate(ctx); err != nil {
		return CommandPlan{}, err
	}
	if err := validateMCPSourceAuthoritiesCurrent(ctx, optimistic.plan); err != nil {
		return CommandPlan{}, err
	}
	if optimistic.skillSearchRoots != nil {
		if err := optimistic.skillSearchRoots.Validate(ctx); err != nil {
			return CommandPlan{}, fmt.Errorf("revalidate planned Skill search roots: %w", err)
		}
	}

	revisions := optimistic.revisions
	stableRevisions := optimistic.stableRevisions
	if matches, err := revisions.MatchesCurrent(ctx); err != nil {
		return CommandPlan{}, err
	} else if !matches {
		return CommandPlan{}, mutation.StaleSnapshotError{}
	}
	selectorMembership, err := captureSelectorSkillMembershipWitness(ctx, optimistic.plan)
	if err != nil {
		return CommandPlan{}, err
	}
	currentObserved, err := buildCurrentPlan(ctx, optimistic.request)
	if err != nil {
		return CommandPlan{}, err
	}
	currentPlan := currentObserved.plan
	if err := optimistic.barrier.Validate(ctx); err != nil {
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
	currentBarrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return CommandPlan{}, err
	}
	if !optimistic.barrier.Equal(currentBarrier) {
		return CommandPlan{}, mutation.StaleSnapshotError{}
	}
	currentDomains, currentRequests, currentStableRequests, err := importMutationEvidence(currentPlan, currentBarrier)
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
		if err := currentObserved.validateSkillSearchRoots(ctx); err != nil {
			return err
		}
		return optimistic.barrier.Validate(ctx)
	}
	progressEvents.emit(ProgressEvent{
		Kind:      ProgressEventPhaseCompleted,
		Phase:     ProgressPhaseRevalidation,
		Completed: progressTotal,
		Total:     progressTotal,
	})
	progressEvents.emit(ProgressEvent{
		Kind:  ProgressEventPhaseStarted,
		Phase: ProgressPhasePublication,
	})
	if err := optimistic.barrier.Validate(ctx); err != nil {
		return CommandPlan{}, err
	}
	if err := currentObserved.validateSkillSearchRoots(ctx); err != nil {
		return CommandPlan{}, err
	}
	if err := writePlan(ctx, currentPlan, validateStable); err != nil {
		return CommandPlan{}, err
	}
	progressEvents.emit(ProgressEvent{
		Kind:  ProgressEventPhaseCompleted,
		Phase: ProgressPhasePublication,
	})
	return CommandPlan{
		request:          optimistic.request,
		plan:             currentPlan,
		skillSearchRoots: currentObserved.skillSearchRoots,
		revisions:        revisions,
		stableRevisions:  stableRevisions,
		barrier:          optimistic.barrier,
	}, nil
}

func captureImportRevisionEvidence(
	ctx context.Context,
	plan adoptmodel.Plan,
	barrier recoverygate.EffectAuthority,
) (mutation.RevisionSet, mutation.RevisionSet, error) {
	_, requests, stableRequests, err := importMutationEvidence(plan, barrier)
	if err != nil {
		return mutation.RevisionSet{}, mutation.RevisionSet{}, err
	}
	revisions, err := mutation.CaptureRevisionSet(ctx, requests...)
	if err != nil {
		return mutation.RevisionSet{}, mutation.RevisionSet{}, err
	}
	stableRevisions, err := revisions.Subset(stableRequests...)
	if err != nil {
		return mutation.RevisionSet{}, mutation.RevisionSet{}, err
	}
	return revisions, stableRevisions, nil
}

func importPlanFingerprint(plan adoptmodel.Plan) (mutation.OperationFingerprint, error) {
	canonical, err := plan.IdentityBytes()
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint import plan: %w", err)
	}
	return operationplan.HashCanonical(canonical), nil
}

func importMutationEvidence(
	plan adoptmodel.Plan,
	barrier recoverygate.EffectAuthority,
) ([]mutation.Domain, []mutation.RevisionRequest, []mutation.RevisionRequest, error) {
	program, err := compileImportOperationProgram(plan, barrier)
	if err != nil {
		return nil, nil, nil, err
	}
	return executeImportOperationProgram(program)
}

func compileImportOperationProgram(
	plan adoptmodel.Plan,
	barrier recoverygate.EffectAuthority,
) (operationplan.AdoptProgram, error) {
	if err := plan.Validate(); err != nil {
		return operationplan.AdoptProgram{}, err
	}
	paths, err := daempaths.Resolve(plan.Output())
	if err != nil {
		return operationplan.AdoptProgram{}, err
	}
	_, selectorMembershipRequired, err := selectorSkillMergeEnvironment(plan)
	if err != nil {
		return operationplan.AdoptProgram{}, err
	}
	metadataTransactionPath, err := fileset.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return operationplan.AdoptProgram{}, err
	}
	input := operationplan.AdoptInput{
		BarrierDomains:          barrier.Domains(),
		BarrierRevisions:        barrier.RevisionRequests(),
		OutputPath:              plan.Output(),
		OutputMaximumBytes:      declarationartifact.MaximumBytes,
		MetadataTransactionPath: metadataTransactionPath,
	}
	if selectorMembershipRequired {
		input.SelectorLockfilePath = paths.LockfilePath
	}
	for _, source := range plan.Sources() {
		input.Sources = append(input.Sources, operationplan.AdoptSource{
			SourcePath: source.SourcePath,
			LivePath:   source.LivePath,
			Target:     string(source.Target),
			Scope:      string(source.Scope),
		})
	}
	for _, skill := range plan.Skills() {
		input.SkillSourcePaths = append(input.SkillSourcePaths, skill.SourcePath)
	}
	for _, authority := range plan.SkillSourceAuthorities() {
		for _, route := range authority.Routes {
			input.SkillRoutes = append(input.SkillRoutes, operationplan.AdoptSkillRoute{
				LivePath: route.LivePath,
				ReadPath: route.ReadPath,
				Target:   string(route.Target),
				Scope:    string(authority.Scope),
			})
		}
	}
	for _, hook := range plan.Hooks() {
		input.Hooks = append(input.Hooks, operationplan.AdoptPhysicalPath{
			Path:   hook.LivePath,
			Target: string(hook.Target),
			Scope:  string(hook.Scope),
		})
	}
	for _, authority := range plan.MCPSourceAuthorities() {
		input.MCPSources = append(input.MCPSources, operationplan.AdoptMCPSource{
			PrimaryPath:         authority.PrimaryPath,
			Target:              string(authority.Target),
			Scope:               string(authority.Scope),
			RequiredAbsentPaths: append([]string(nil), authority.RequiredAbsentPaths...),
		})
	}
	for _, scan := range plan.Scans() {
		kind := operationplan.AdoptScanKind(scan.Evidence.Kind)
		switch scan.Evidence.Kind {
		case adoptmodel.ScanEvidenceBoundedFile:
			kind = operationplan.AdoptScanBoundedFile
		case adoptmodel.ScanEvidenceDirectoryListing:
			kind = operationplan.AdoptScanDirectoryListing
		}
		input.Scans = append(input.Scans, operationplan.AdoptScan{
			Path:         scan.LivePath,
			Target:       string(scan.Target),
			Scope:        string(scan.Scope),
			Kind:         kind,
			MaximumBytes: scan.Evidence.MaximumBytes,
		})
	}
	return operationplan.CompileAdopt(input), nil
}

func executeImportOperationProgram(
	program operationplan.AdoptProgram,
) ([]mutation.Domain, []mutation.RevisionRequest, []mutation.RevisionRequest, error) {
	compiler, err := program.NewRevisionCompiler()
	if err != nil {
		return nil, nil, nil, err
	}
	steps := program.Steps()
	domains := make([]mutation.Domain, 0, len(steps))
	for _, step := range steps {
		if err := step.Preflight(); err != nil {
			return nil, nil, nil, err
		}
		if domainStep, ok := step.Domain(); ok {
			domain, err := lowerImportDomainStep(domainStep)
			if err != nil {
				return nil, nil, nil, err
			}
			domains = append(domains, domain)
		}
		if err := compiler.ApplyAfterDomain(step); err != nil {
			return nil, nil, nil, err
		}
	}
	revisions, err := compiler.Compile()
	if err != nil {
		return nil, nil, nil, err
	}
	return domains, revisions.Revisions(), revisions.StableRevisions(), nil
}

func lowerImportDomainStep(step operationplan.DomainStep) (mutation.Domain, error) {
	if domain, ok := step.Compiled(); ok {
		return domain, nil
	}
	request, ok := step.Path()
	if !ok {
		return mutation.Domain{}, fmt.Errorf("import operation domain step is invalid")
	}
	if logical, ok := request.Logical(); ok {
		return mutation.NewLogicalPathDomain(logical)
	}
	physical, ok := request.Physical()
	if !ok {
		return mutation.Domain{}, fmt.Errorf("import operation path-domain request is invalid")
	}
	return mutation.NewPhysicalPathDomain(physical)
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
		primarySources[authority.PrimaryPath] = primarySource{
			maximumBytes: authority.MaximumBytes,
			revision:     authority.PrimaryRevision,
		}
		for _, path := range authority.RequiredAbsentPaths {
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
