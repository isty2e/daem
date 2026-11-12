package metadataconversion

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/recoverygate"
)

// Prepare compiles the entire reachable cohort without conversion effects.
// Ordinary command eligibility uses the caller's existing selected authority;
// it is not inferred from a recorded manifest or the conversion marker.
func Prepare(ctx context.Context, authority recoverygate.EffectAuthority, budget fileset.PhysicalWorkBudget, candidates []pathauthority.Reference) (Plan, error) {
	if ctx == nil || budget == nil {
		return Plan{}, fmt.Errorf("metadata preparation requires context and shared physical work budget")
	}
	paths, err := authority.SelectedPaths()
	if err != nil {
		return Plan{}, err
	}
	if err := authority.Validate(ctx); err != nil {
		return Plan{}, err
	}
	plan, err := compilePlan(ctx, compilerInput{Paths: paths, References: candidates, PhysicalWorkBudget: budget}, liveSources(budget), nil)
	if err != nil {
		return Plan{}, err
	}
	barrier, err := recoverygate.CaptureMetadataConversionBarrier(ctx, plan.paths, plan.selection.barrierDirs, budget)
	if err != nil {
		return Plan{}, err
	}
	if err := barrier.Validate(ctx); err != nil {
		return Plan{}, err
	}
	plan.authority = authority
	plan.barrier = barrier
	return plan, nil
}

// Execute requalifies the same selection under complete leases before calling
// FileSet5. A true no-op does not acquire leases or create publication storage.
// The plan and its operation budget must not be used concurrently.
func (plan Plan) Execute(ctx context.Context) (returnErr error) {
	if ctx == nil || plan.budget == nil || plan.intent.Validate() != nil {
		return fmt.Errorf("metadata conversion plan is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !plan.changed {
		return nil
	}
	if _, err := plan.authority.SelectedPaths(); err != nil {
		return err
	}
	domains, err := plan.domains()
	if err != nil {
		return err
	}
	store, err := mutation.NewStore(plan.paths.DataDir)
	if err != nil {
		return err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, leases.Release()) }()
	if err := validateHeldMetadataLeases(ctx, leases); err != nil {
		return err
	}
	if err := plan.authority.Validate(ctx); err != nil {
		return err
	}
	if err := plan.barrier.Validate(ctx); err != nil {
		return err
	}
	fresh, err := compilePlan(ctx, compilerInput{Paths: plan.paths, PhysicalWorkBudget: plan.budget}, liveSources(plan.budget), &plan.selection)
	if err != nil {
		return err
	}
	if !plan.intent.Equal(fresh.intent) {
		return mutation.StaleSnapshotError{}
	}
	if err := validateHeldMetadataLeases(ctx, leases); err != nil {
		return err
	}
	if err := plan.barrier.Validate(ctx); err != nil {
		return err
	}
	return fileset.CommitFreshFileSet5(ctx, fileset.PublicationInput{
		EvidenceRoot: plan.paths.GlobalMetadataDir(), Intent: plan.intent,
		Materializers: plan.materialize, PhysicalWorkBudget: plan.budget,
	})
}

func (plan Plan) domains() ([]mutation.Domain, error) {
	domains := append(plan.barrier.Domains(), plan.authority.Domains()...)
	global, err := recoverygate.GlobalMetadataAuthorityPath(plan.paths)
	if err != nil {
		return nil, err
	}
	requests := []mutation.LogicalPathRequest{
		{Path: global, Access: mutation.AccessExclusive},
		{Path: plan.paths.GlobalMetadataDir(), Access: mutation.AccessExclusive},
	}
	for _, target := range plan.intent.Targets() {
		requests = append(requests, mutation.LogicalPathRequest{Path: target.Path(), Access: mutation.AccessExclusive})
	}
	for _, reference := range plan.selection.references.References() {
		requests = append(requests, mutation.LogicalPathRequest{Path: reference.Locator(), Access: mutation.AccessShared})
	}
	for _, request := range requests {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			request.Effect = effect
			domain, err := mutation.NewLogicalPathDomain(request)
			if err != nil {
				return nil, err
			}
			domains = append(domains, domain)
		}
	}
	return domains, nil
}

func validateHeldMetadataLeases(ctx context.Context, leases *mutation.LeaseSet) error {
	current, err := leases.DomainsMatchCurrent(ctx)
	if err != nil {
		return err
	}
	if !current {
		return mutation.StaleSnapshotError{}
	}
	return nil
}
