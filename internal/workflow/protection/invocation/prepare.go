package invocation

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/protection"
	"github.com/isty2e/daem/internal/effect/protection/namespace"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

type invocationQualification struct {
	work      physicalwork.InvocationWorkReservation
	paths     daempaths.Paths
	leases    *mutation.LeaseSet
	row       ownership.PendingProtection
	requests  []mutation.PhysicalAuthorityRequest
	authority mutation.PreparedExclusiveRequests
	registry  ownershipstore.PreparedProtectionStore
	owners    map[stateauthority.Authority]mutation.DirectoryEntryObservationPlan
	documents map[string]invocationDocument
}

type invocationDocument struct {
	bindings []invocationBinding
	layout   rootedpath.PathLayout
}

type invocationBinding struct {
	owner       stateauthority.Authority
	observation mutation.DirectoryEntryObservationPlan
}

func (input InvocationInput) matchesPreparation() bool {
	qualified := input.qualification
	return qualified != nil && input.Paths == qualified.paths && input.Leases == qualified.leases && qualified.row.Equal(input.Protection) && slices.Equal(input.Requests, qualified.requests)
}

// PrepareInvocation captures read-only layout before any invocation credit is
// transferred. The containing operation supplies its shared preflight budget.
func PrepareInvocation(ctx context.Context, input InvocationInput, budget *physicalwork.Budget) (InvocationInput, error) {
	if ctx == nil || budget == nil || input.Protection.Validate() != nil || input.Protection.Phase() != ownership.ProtectionArmed || input.Leases == nil {
		return InvocationInput{}, fmt.Errorf("invocation preparation requires an armed candidate, authority and allocation")
	}
	controls := []string{input.Paths.StatefilePath, input.Paths.OwnershipRegistryPath, input.Paths.RecoveryDir}
	requests := slices.Clone(input.Requests)
	authority, err := input.Leases.PrepareCurrentExclusiveRequestsBounded(ctx, controls, requests, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return InvocationInput{}, err
	}
	if err := recoverygate.ObserveBounded(ctx, input.Paths, budget); err != nil {
		return InvocationInput{}, err
	}
	owner, err := namespace.ObserveOwner(input.Paths, budget)
	if err != nil || !owner.Equal(input.Protection.Owner()) {
		return InvocationInput{}, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	contexts, err := namespace.NewResolution(input.Paths, owner).BindingContexts(ctx, input.Protection, budget)
	if err != nil {
		return InvocationInput{}, err
	}
	var documents []pathauthority.Exact
	for _, document := range input.Protection.Documents() {
		documents = append(documents, document.PathAuthority())
	}
	covered, err := authority.MatchCurrentExclusiveDocuments(ctx, documents, budget)
	if err != nil || !covered {
		return InvocationInput{}, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	qualification := &invocationQualification{
		paths: input.Paths, leases: input.Leases, row: input.Protection, requests: requests, authority: authority,
		owners:    make(map[stateauthority.Authority]mutation.DirectoryEntryObservationPlan),
		documents: make(map[string]invocationDocument),
	}
	for _, document := range input.Protection.Documents() {
		var target invocationDocument
		for _, binding := range document.Bindings() {
			paths := contexts[binding.Owner()]
			path, err := namespace.ResolveBindingDestination(paths, binding.Destination(), binding.PathAuthority().Key(), budget)
			if err != nil {
				return InvocationInput{}, err
			}
			observation, err := mutation.PrepareReferentAuthorityBounded(ctx, path, physicalwork.MaximumPhysicalPathDepth, budget)
			if err != nil {
				return InvocationInput{}, err
			}
			target.bindings = append(target.bindings, invocationBinding{owner: binding.Owner(), observation: observation})
			if len(target.bindings) == 1 {
				target.layout, err = rootedpath.CaptureReferentLayoutBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
				if err != nil {
					return InvocationInput{}, err
				}
			}
			if _, present := qualification.owners[binding.Owner()]; !present {
				plan, err := mutation.PrepareDirectoryEntryAuthorityBounded(ctx, paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
				if err != nil {
					return InvocationInput{}, err
				}
				qualification.owners[binding.Owner()] = plan
			}
		}
		qualification.documents[document.PathAuthority().Key()] = target
	}
	qualification.registry, err = ownershipstore.PrepareProtectionStore(ctx, input.Paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return InvocationInput{}, err
	}
	input.Requests = slices.Clone(requests)
	input.qualification = qualification
	if err := qualifyInvocationStart(ctx, qualification, input.Protection, budget); err != nil {
		return InvocationInput{}, err
	}
	return input, nil
}

func (qualification *invocationQualification) CaptureDocument(ctx context.Context, document ownership.ProtectedDocument, allocation rootedpath.PhysicalTraversalBudget) (*rootedpath.CapturedRoot, rootedpath.Destination, error) {
	budget, supported := allocation.(mutation.PhysicalObservationBudget)
	if !supported {
		return nil, rootedpath.Destination{}, fmt.Errorf("prepared qualification requires complete physical allocation")
	}
	target, present := qualification.documents[document.PathAuthority().Key()]
	if !present || len(target.bindings) != len(document.Bindings()) {
		return nil, rootedpath.Destination{}, fmt.Errorf("protected document was not prepared")
	}
	for index, binding := range document.Bindings() {
		observed, err := target.bindings[index].observation.ObserveBounded(ctx, budget)
		if err != nil {
			return nil, rootedpath.Destination{}, protection.Refusal(protection.DocumentNamespaceUnqualified, err)
		}
		if err := namespace.RequireObservation(observed, document.PathAuthority()); err != nil {
			return nil, rootedpath.Destination{}, err
		}
		owner, err := qualification.owners[binding.Owner()].ObserveBounded(ctx, budget)
		if err != nil {
			return nil, rootedpath.Destination{}, protection.Refusal(protection.ResolutionContextUnqualified, err)
		}
		exact, present := owner.Exact()
		if !present || !exact.Equal(binding.Owner().StatefileAuthority()) {
			return nil, rootedpath.Destination{}, protection.Refusal(protection.ResolutionContextUnqualified, fmt.Errorf("protected document cohort authority changed"))
		}
	}
	return target.layout.CaptureDestinationBounded(physicalwork.MaximumPhysicalPathDepth, budget)
}
