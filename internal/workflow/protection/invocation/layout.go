package invocation

import (
	"context"
	"errors"
	"fmt"
	"slices"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	physicalhostpath "github.com/isty2e/daem/internal/assurance/observe/hostpath"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/protection"
	"github.com/isty2e/daem/internal/effect/protection/namespace"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

type InvocationLayoutInput struct {
	Paths       daempaths.Paths
	Owner       stateauthority.Authority
	Invocation  executionidentity.Invocation
	Correlation ownership.ProtectionCorrelation
	Leases      *mutation.LeaseSet
	Requests    []mutation.PhysicalAuthorityRequest
	Members     []journalobservation.Member
}

// InvocationLayout retains prospective facts, not permission metadata or a fence.
// Copies share the same one-shot attachment and execution allocation.
type InvocationLayout struct{ prepared *prospectiveInvocation }

type prospectiveInvocation struct {
	qualification *invocationQualification
	owner         stateauthority.Authority
	invocation    executionidentity.Invocation
	correlation   ownership.ProtectionCorrelation
	documents     []prospectiveInvocationDocument
}

type prospectiveInvocationDocument struct {
	target  invocationDocument
	intents []ownership.DocumentBindingIntent
}

type invocationBindingKey struct {
	owner       stateauthority.Key
	destination output.Destination
}

// PrepareInvocationLayout captures the expected post-prefix cohort without
// inventing metadata for documents that do not exist yet.
func PrepareInvocationLayout(ctx context.Context, input InvocationLayoutInput, budget *physicalwork.Budget) (InvocationLayout, error) {
	if ctx == nil || budget == nil || input.Leases == nil || len(input.Members) == 0 {
		return InvocationLayout{}, fmt.Errorf("invocation layout requires members, authority and allocation")
	}
	if err := errors.Join(ctx.Err(), input.Owner.Validate(), input.Invocation.Validate(), input.Correlation.Validate(), budget.RequireCapturePending()); err != nil {
		return InvocationLayout{}, err
	}
	controls := []string{input.Paths.StatefilePath, input.Paths.OwnershipRegistryPath, input.Paths.RecoveryDir}
	requests := slices.Clone(input.Requests)
	authority, err := input.Leases.PrepareCurrentExclusiveRequestsBounded(ctx, controls, requests, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return InvocationLayout{}, err
	}
	if err := recoverygate.ObserveBounded(ctx, input.Paths, budget); err != nil {
		return InvocationLayout{}, err
	}
	owner, err := namespace.ObserveOwner(input.Paths, budget)
	if err != nil || !owner.Equal(input.Owner) {
		return InvocationLayout{}, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	qualified := &invocationQualification{
		paths: input.Paths, leases: input.Leases, requests: requests, authority: authority,
		owners: make(map[stateauthority.Authority]mutation.DirectoryEntryObservationPlan),
	}
	qualified.registry, err = ownershipstore.PrepareProtectionStore(ctx, input.Paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return InvocationLayout{}, err
	}
	current, err := readInvocationRegistry(ctx, qualified.registry, budget)
	if err != nil {
		return InvocationLayout{}, err
	}
	prepared := &prospectiveInvocation{qualification: qualified, owner: input.Owner, invocation: input.Invocation, correlation: input.Correlation}
	for _, member := range input.Members {
		if err := member.RegistryScope().RequireWritable(current); err != nil {
			return InvocationLayout{}, err
		}
		document, err := prepared.prepareDocument(ctx, member, current, budget)
		if err != nil {
			return InvocationLayout{}, err
		}
		prepared.documents = append(prepared.documents, document)
	}
	if err := ctx.Err(); err != nil {
		return InvocationLayout{}, err
	}
	return InvocationLayout{prepared: prepared}, nil
}

func readInvocationRegistry(ctx context.Context, prepared ownershipstore.PreparedProtectionStore, budget mutation.PhysicalObservationBudget) (registry ownership.Registry, returnErr error) {
	store, root, err := prepared.OpenBounded(ctx, budget)
	if err != nil {
		return registry, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	return store.LoadProtectionRegistry(ctx)
}

func (prepared *prospectiveInvocation) prepareDocument(ctx context.Context, member journalobservation.Member, current ownership.Registry, budget *physicalwork.Budget) (prospectiveInvocationDocument, error) {
	observation := member.Namespace().Observation()
	intents, err := prospectiveBindingCohort(current, observation, prepared.owner, member.BindingIntents())
	if err != nil {
		return prospectiveInvocationDocument{}, err
	}
	document := prospectiveInvocationDocument{intents: intents}
	for _, intent := range intents {
		paths, err := namespace.NewResolution(prepared.qualification.paths, prepared.owner).BindingContext(intent.Owner(), budget)
		if err != nil {
			return prospectiveInvocationDocument{}, err
		}
		path, err := physicalhostpath.NewResolver(paths).Resolve(intent.Destination(), budget)
		if err != nil {
			return prospectiveInvocationDocument{}, err
		}
		plan, err := mutation.PrepareReferentAuthorityBounded(ctx, path, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return prospectiveInvocationDocument{}, err
		}
		observed, err := plan.ObserveBounded(ctx, budget)
		if err != nil || !observed.Equal(observation) {
			return prospectiveInvocationDocument{}, errors.Join(mutation.StaleSnapshotError{}, err)
		}
		document.target.bindings = append(document.target.bindings, invocationBinding{owner: intent.Owner(), observation: plan})
		if len(document.target.bindings) == 1 {
			document.target.layout, err = rootedpath.CaptureReferentLayoutBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
			if err != nil {
				return prospectiveInvocationDocument{}, err
			}
		}
		if _, exists := prepared.qualification.owners[intent.Owner()]; !exists {
			owner, err := mutation.PrepareDirectoryEntryAuthorityBounded(ctx, paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, budget)
			if err != nil {
				return prospectiveInvocationDocument{}, err
			}
			prepared.qualification.owners[intent.Owner()] = owner
		}
	}
	return document, nil
}

func prospectiveBindingCohort(current ownership.Registry, observation mutation.DirectoryEntryAuthorityObservation, owner stateauthority.Authority, own []ownership.DocumentBindingIntent) ([]ownership.DocumentBindingIntent, error) {
	if err := observation.Validate(); err != nil {
		return nil, err
	}
	if len(own) == 0 {
		return nil, fmt.Errorf("prospective document requires its owner's binding intents")
	}
	var cohort []ownership.DocumentBinding
	var err error
	if exact, known := observation.Exact(); known {
		cohort, err = current.DocumentBindingsAt(exact)
	} else {
		missing, _ := observation.Provisional()
		cohort, err = current.DocumentBindingsForMissing(missing)
	}
	if err != nil {
		return nil, err
	}
	var intents []ownership.DocumentBindingIntent
	indices := make(map[invocationBindingKey]int)
	for _, binding := range cohort {
		value, _ := ownership.PresentDocumentBinding(binding)
		intent, err := ownership.NewExactDocumentBindingIntent(binding.Owner(), binding.Destination(), value, value)
		if err != nil {
			return nil, err
		}
		indices[invocationBindingKey{binding.Owner().Key(), binding.Destination()}] = len(intents)
		intents = append(intents, intent)
	}
	seen := make(map[invocationBindingKey]bool)
	for _, intent := range own {
		if err := intent.Validate(); err != nil || !intent.Owner().Equal(owner) {
			return nil, errors.Join(fmt.Errorf("prospective binding belongs to another owner"), err)
		}
		key := invocationBindingKey{intent.Owner().Key(), intent.Destination()}
		actual := ownership.NoDocumentBinding()
		if binding, present := current.DocumentBinding(key.owner, key.destination); present {
			actual, _ = ownership.PresentDocumentBinding(binding)
		}
		if seen[key] || !actual.Equal(intent.Before()) {
			return nil, mutation.StaleSnapshotError{}
		}
		seen[key] = true
		if after, known := intent.KnownAfter(); known {
			if _, present := after.Get(); !present {
				intent, err = ownership.NewExactDocumentBindingIntent(intent.Owner(), intent.Destination(), intent.Before(), intent.Before())
				if err != nil {
					return nil, err
				}
			}
		}
		if index, present := indices[key]; present {
			intents[index] = intent
		} else {
			indices[key] = len(intents)
			intents = append(intents, intent)
		}
	}
	return intents, nil
}

func (layout InvocationLayout) ReserveWork(budget *physicalwork.Budget, correlation physicalwork.InvocationWork) error {
	if layout.prepared == nil || budget == nil {
		return fmt.Errorf("prepared invocation layout and shared allocation are required")
	}
	qualified := layout.prepared.qualification
	if qualified.work != (physicalwork.InvocationWorkReservation{}) {
		return fmt.Errorf("invocation work was already reserved")
	}
	attachment := &invocationWorkProjection{budget: physicalwork.NewCaptureBudget(nil)}
	if err := qualified.authority.ReserveVisibilityAcceptance(attachment); err != nil {
		return err
	}
	if err := qualified.authority.ReserveCheck(attachment); err != nil {
		return err
	}
	if err := qualified.registry.ReserveOpen(attachment); err != nil {
		return err
	}
	if err := qualified.registry.ReserveRead(attachment); err != nil {
		return err
	}
	var targets []invocationDocument
	for _, document := range layout.prepared.documents {
		targets = append(targets, document.target)
		if err := qualified.reserveTargetQualification(document.target, attachment); err != nil {
			return err
		}
		if err := reserveTargetInspection(document.target, attachment); err != nil {
			return err
		}
	}
	attachmentWork, err := attachment.compiled()
	if err != nil {
		return err
	}
	forward, safety, err := qualified.compileWork(targets, correlation)
	if err != nil {
		return err
	}
	qualified.work, err = budget.ReserveInvocationWithAttachment(attachmentWork, forward, safety)
	return err
}

// AttachProtection consumes the prepaid attachment after successful prefix work.
// It observes actual metadata but publishes no protection or command correlation.
func (layout InvocationLayout) AttachProtection(ctx context.Context) (InvocationInput, error) {
	if ctx == nil || layout.prepared == nil {
		return InvocationInput{}, fmt.Errorf("prepared invocation layout and context are required")
	}
	if err := ctx.Err(); err != nil {
		return InvocationInput{}, err
	}
	prepared := layout.prepared
	qualified := prepared.qualification
	budget, err := qualified.work.TakeAttachment()
	if err != nil {
		return InvocationInput{}, err
	}
	accepted, err := qualified.authority.AcceptVisibilityChanges(ctx, budget)
	if err != nil || !accepted {
		return InvocationInput{}, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	current, err := readInvocationRegistry(ctx, qualified.registry, budget)
	if err != nil {
		return InvocationInput{}, err
	}
	var documents []ownership.ProtectedDocument
	var physical []pathauthority.Exact
	targets := make(map[string]invocationDocument)
	for _, prospective := range prepared.documents {
		document, target, err := prepared.attachDocument(ctx, prospective, budget)
		if err != nil {
			return InvocationInput{}, err
		}
		documents = append(documents, document)
		physical = append(physical, document.PathAuthority())
		targets[document.PathAuthority().Key()] = target
	}
	covered, err := qualified.authority.MatchCurrentExclusiveDocuments(ctx, physical, budget)
	if err != nil || !covered {
		return InvocationInput{}, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	row, err := ownership.NewArmedProtection(prepared.owner, prepared.invocation, prepared.correlation, documents)
	if err != nil {
		return InvocationInput{}, err
	}
	arm, err := ownership.NewArmProtection(row)
	if err != nil {
		return InvocationInput{}, err
	}
	if _, _, err := current.ApplyProtection(prepared.owner, arm); err != nil {
		return InvocationInput{}, err
	}
	if err := ctx.Err(); err != nil {
		return InvocationInput{}, err
	}
	qualified.row, qualified.documents = row, targets
	return InvocationInput{Paths: qualified.paths, Protection: row, Leases: qualified.leases, Requests: slices.Clone(qualified.requests), qualification: qualified}, nil
}

func (prepared *prospectiveInvocation) attachDocument(ctx context.Context, prospective prospectiveInvocationDocument, budget *physicalwork.Budget) (document ownership.ProtectedDocument, target invocationDocument, returnErr error) {
	target.layout = prospective.target.layout
	var bindings []ownership.DocumentBinding
	plans := make(map[invocationBindingKey]invocationBinding)
	for index, intent := range prospective.intents {
		plan := prospective.target.bindings[index]
		observation, err := plan.observation.ObserveBounded(ctx, budget)
		if err != nil {
			return document, target, err
		}
		exact, present := observation.Exact()
		if !present {
			return document, target, protection.Refusal(protection.DocumentNamespaceUnqualified, mutation.StaleSnapshotError{})
		}
		owner, err := prepared.qualification.owners[intent.Owner()].ObserveBounded(ctx, budget)
		if err != nil {
			return document, target, err
		}
		if observed, present := owner.Exact(); !present || !observed.Equal(intent.Owner().StatefileAuthority()) {
			return document, target, protection.Refusal(protection.ResolutionContextUnqualified, mutation.StaleSnapshotError{})
		}
		if _, pending := intent.ProvisionalAfter(); pending {
			intent, err = intent.Promote(exact)
			if err != nil {
				return document, target, err
			}
		}
		after, _ := intent.KnownAfter()
		binding, present := after.Get()
		if !present || !binding.PathAuthority().Equal(exact) {
			return document, target, protection.Refusal(protection.DocumentNamespaceUnqualified, mutation.StaleSnapshotError{})
		}
		bindings = append(bindings, binding)
		plans[invocationBindingKey{binding.Owner().Key(), binding.Destination()}] = plan
	}
	root, destination, err := target.layout.CaptureDestinationBounded(physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return document, target, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	capability, err := root.AcquireBounded(destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return document, target, err
	}
	metadata, err := storagecommit.ObserveRootedFilePermissionMetadata(ctx, capability)
	if err != nil {
		return document, target, err
	}
	document, err = ownership.NewProtectedDocument(bindings, metadata.UserID, metadata.GroupID, metadata.Mode)
	if err != nil {
		return document, target, err
	}
	for _, binding := range document.Bindings() {
		target.bindings = append(target.bindings, plans[invocationBindingKey{binding.Owner().Key(), binding.Destination()}])
	}
	return document, target, nil
}
