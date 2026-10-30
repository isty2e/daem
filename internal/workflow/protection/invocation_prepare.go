package protection

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalrecovery "github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

type invocationQualification struct {
	work      journalrecovery.InvocationWorkReservation
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
	bindings []mutation.DirectoryEntryObservationPlan
	layout   rootedpath.PathLayout
}

func (input InvocationInput) matchesPreparation() bool {
	qualified := input.qualification
	return qualified != nil && input.Paths == qualified.paths && input.Leases == qualified.leases && qualified.row.Equal(input.Protection) && slices.Equal(input.Requests, qualified.requests)
}

// PrepareInvocation captures read-only layout before any invocation credit is
// transferred. The containing operation supplies its shared preflight budget.
func PrepareInvocation(ctx context.Context, input InvocationInput, budget *journalrecovery.PhysicalWorkBudget) (InvocationInput, error) {
	if ctx == nil || budget == nil || input.Protection.Validate() != nil || input.Protection.Phase() != ownership.ProtectionArmed || input.Leases == nil {
		return InvocationInput{}, fmt.Errorf("invocation preparation requires an armed candidate, authority and allocation")
	}
	controls := []string{input.Paths.StatefilePath, input.Paths.OwnershipRegistryPath, input.Paths.RecoveryDir}
	requests := slices.Clone(input.Requests)
	authority, err := input.Leases.PrepareCurrentExclusiveRequestsBounded(ctx, controls, requests, journalrecovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return InvocationInput{}, err
	}
	if err := recoverygate.ObserveBounded(ctx, input.Paths, budget); err != nil {
		return InvocationInput{}, err
	}
	prepared := Prepared{paths: input.Paths, owner: input.Protection.Owner(), budget: budget}
	owner, err := observeOwner(input.Paths, budget)
	if err != nil || !owner.Equal(prepared.owner) {
		return InvocationInput{}, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	contexts, err := prepared.bindingContexts(ctx, input.Protection)
	if err != nil {
		return InvocationInput{}, err
	}
	if err := requireInvocationCoverage(ctx, input, controls, budget); err != nil {
		return InvocationInput{}, err
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
			path, err := managedhostpath.Resolver(paths).Resolve(binding.Destination())
			if err != nil {
				return InvocationInput{}, err
			}
			observation, err := mutation.PrepareDirectoryEntryAuthorityBounded(ctx, path, journalrecovery.MaximumPhysicalPathDepth, budget)
			if err != nil {
				return InvocationInput{}, err
			}
			target.bindings = append(target.bindings, observation)
			if len(target.bindings) == 1 {
				target.layout, err = rootedpath.CaptureDirectoryEntryLayoutBounded(path, journalrecovery.MaximumPhysicalPathDepth, budget)
				if err != nil {
					return InvocationInput{}, err
				}
			}
			if _, present := qualification.owners[binding.Owner()]; !present {
				plan, err := mutation.PrepareDirectoryEntryAuthorityBounded(ctx, paths.StatefilePath, journalrecovery.MaximumPhysicalPathDepth, budget)
				if err != nil {
					return InvocationInput{}, err
				}
				qualification.owners[binding.Owner()] = plan
			}
		}
		qualification.documents[document.PathAuthority().Key()] = target
	}
	qualification.registry, err = ownershipstore.PrepareProtectionStore(ctx, input.Paths.OwnershipRegistryPath, journalrecovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return InvocationInput{}, err
	}
	input.Requests = slices.Clone(requests)
	input.qualification = qualification
	prepared.qualification = qualification
	if err := qualifyInvocationStart(ctx, prepared, input.Protection); err != nil {
		return InvocationInput{}, err
	}
	return input, nil
}

func (qualification *invocationQualification) qualifyTarget(ctx context.Context, document ownership.ProtectedDocument, budget mutation.PhysicalObservationBudget) (*rootedpath.CapturedRoot, rootedpath.Destination, error) {
	target, present := qualification.documents[document.PathAuthority().Key()]
	if !present || len(target.bindings) != len(document.Bindings()) {
		return nil, rootedpath.Destination{}, fmt.Errorf("protected document was not prepared")
	}
	for index, binding := range document.Bindings() {
		observed, err := target.bindings[index].ObserveBounded(ctx, budget)
		if err != nil {
			return nil, rootedpath.Destination{}, refusal(DocumentNamespaceUnqualified, err)
		}
		if err := requireNamespaceObservation(observed, document.PathAuthority()); err != nil {
			return nil, rootedpath.Destination{}, err
		}
		owner, err := qualification.owners[binding.Owner()].ObserveBounded(ctx, budget)
		if err != nil {
			return nil, rootedpath.Destination{}, refusal(ResolutionContextUnqualified, err)
		}
		exact, present := owner.Exact()
		if !present || !exact.Equal(binding.Owner().StatefileAuthority()) {
			return nil, rootedpath.Destination{}, refusal(ResolutionContextUnqualified, fmt.Errorf("protected document cohort authority changed"))
		}
	}
	return target.layout.CaptureDestinationBounded(journalrecovery.MaximumPhysicalPathDepth, budget)
}
