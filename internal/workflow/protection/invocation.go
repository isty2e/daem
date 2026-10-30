package protection

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	journalrecovery "github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/subprocess"
)

var ErrInvocationPermissionsExpanded = errors.New("protected file permissions expanded during invocation")

// InvocationInput borrows the containing operation's exact leases and complete
// prepaid work. Selection, approval, prefix/suffix and lease lifetime stay there.
type InvocationInput struct {
	Paths      daempaths.Paths
	Protection ownership.PendingProtection
	Leases     *mutation.LeaseSet
	Requests   []mutation.PhysicalAuthorityRequest
	Work       journalrecovery.InvocationWorkReservation
}

type InvocationResult struct {
	process    *subprocess.ProtectedCommandAttemptResult
	protection *Record
}

func (result InvocationResult) Process() (subprocess.ProtectedCommandAttemptResult, bool) {
	if result.process == nil {
		return subprocess.ProtectedCommandAttemptResult{}, false
	}
	return *result.process, true
}

func (result InvocationResult) Protection() (Record, bool) {
	if result.protection == nil {
		return Record{}, false
	}
	return *result.protection, true
}

// ExecuteInvocation owns Arm/Bind and immediate metadata settlement, not key
// correction or containing-operation completion. Correlation publication, when
// needed, runs after Arm and must use only the supplied forward allocation.
func ExecuteInvocation(ctx context.Context, input InvocationInput, executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, bind subprocess.WorkingDirectoryBinder, prepareCorrelation func(context.Context, *journalrecovery.PhysicalWorkBudget) error) (result InvocationResult, returnErr error) {
	if ctx == nil || input.Protection.Validate() != nil || input.Protection.Phase() != ownership.ProtectionArmed || input.Leases == nil {
		return result, fmt.Errorf("protected invocation requires an armed candidate and containing authority")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	input.Requests = slices.Clone(input.Requests)
	forward, safety, err := input.Work.Take()
	if err != nil {
		return result, err
	}
	controls := []string{input.Paths.StatefilePath, input.Paths.OwnershipRegistryPath, input.Paths.RecoveryDir}
	gate := func(ctx context.Context, budget *journalrecovery.PhysicalWorkBudget) error {
		matches, err := input.Leases.MatchCurrentExclusiveRequestsBounded(ctx, controls, input.Requests, journalrecovery.MaximumPhysicalPathDepth, budget)
		if err != nil || !matches {
			return errors.Join(mutation.StaleSnapshotError{}, err)
		}
		return nil
	}
	if err := gate(ctx, forward); err != nil {
		return result, err
	}
	if err := recoverygate.ObserveBounded(ctx, input.Paths, forward); err != nil {
		return result, err
	}
	prepared := Prepared{paths: input.Paths, owner: input.Protection.Owner(), budget: forward}
	owner, err := observeOwner(input.Paths, forward)
	if err != nil || !owner.Equal(prepared.owner) {
		return result, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	if err := prepared.freezeBindingContexts(ctx, input.Protection); err != nil {
		return result, err
	}
	if err := requireInvocationCoverage(ctx, input, controls, forward); err != nil {
		return result, err
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(input.Paths.OwnershipRegistryPath, journalrecovery.MaximumPhysicalPathDepth, forward)
	if err != nil {
		return result, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	forwardStore, err := ownershipstore.NewRooted(root, destination, journalrecovery.MaximumPhysicalPathDepth, forward)
	if err != nil {
		return result, err
	}
	safetyStore, err := ownershipstore.NewRooted(root, destination, journalrecovery.MaximumPhysicalPathDepth, safety)
	if err != nil {
		return result, err
	}
	current, err := forwardStore.LoadProtectionRegistry(ctx)
	if err != nil {
		return result, err
	}
	if _, exists := current.Protection(input.Protection.Invocation()); exists {
		return result, fmt.Errorf("protected invocation identity is already pending")
	}
	arm, err := ownership.NewArmProtection(input.Protection)
	if err != nil {
		return result, err
	}
	if _, _, err := current.ApplyProtection(prepared.owner, arm); err != nil {
		return result, err
	}
	if err := qualifyInvocationStart(ctx, prepared, input.Protection); err != nil {
		return result, err
	}
	if err := gate(ctx, forward); err != nil {
		return result, err
	}

	record := Record{invocation: input.Protection.Invocation(), phase: ownership.ProtectionArmed, settlement: SettlementUnknown}
	for _, document := range input.Protection.Documents() {
		record.targets = append(record.targets, Target{destination: document.Bindings()[0].Destination(), state: TargetBlocked})
	}
	result.protection = &record
	prepared.budget = safety
	finalized := false
	publicationAcknowledged := false
	finish := func(ctx context.Context, completion *subprocess.ProtectedCommandCompletion) error {
		if finalized {
			return fmt.Errorf("protected invocation was already finalized")
		}
		finalized = true
		var err error
		result.protection, err = finalizeInvocation(ctx, prepared, safetyStore, input.Protection, &record, completion, publicationAcknowledged, func(ctx context.Context) error { return gate(ctx, safety) })
		return err
	}
	defer func() {
		if !finalized {
			returnErr = errors.Join(returnErr, finish(context.WithoutCancel(ctx), nil))
		}
	}()
	if _, err := forwardStore.TransitionProtection(ctx, prepared.owner, arm); err != nil {
		return result, err
	}
	publicationAcknowledged = true
	if prepareCorrelation != nil {
		if err := prepareCorrelation(ctx, forward); err != nil {
			return result, err
		}
	}
	process, err := executor.ExecuteProtectedInWorkingDirectory(ctx, request, bind, input.Protection.Invocation(), func(ctx context.Context, binding executionidentity.Binding) error {
		if err := gate(ctx, forward); err != nil {
			return err
		}
		launch := prepared
		launch.budget = forward
		if err := qualifyInvocationStart(ctx, launch, input.Protection); err != nil {
			return err
		}
		transition, err := ownership.NewBindProtection(input.Protection, binding)
		if err != nil {
			return err
		}
		_, err = forwardStore.TransitionProtection(ctx, prepared.owner, transition)
		return err
	}, func(ctx context.Context, completion subprocess.ProtectedCommandCompletion) error {
		return finish(ctx, &completion)
	})
	result.process = &process
	return result, err
}

func qualifyInvocationStart(ctx context.Context, prepared Prepared, row ownership.PendingProtection) error {
	for _, document := range row.Documents() {
		observation, err := inspectPermissions(ctx, prepared, document, document.InitialRestriction)
		if err != nil || observation.ObservedAbsent() {
			return errors.Join(fmt.Errorf("protected document changed before invocation"), err)
		}
	}
	return nil
}

func requireInvocationCoverage(ctx context.Context, input InvocationInput, controls []string, budget *journalrecovery.PhysicalWorkBudget) error {
	covered := make(map[pathauthority.Exact]bool)
	for _, request := range input.Requests {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := rootedpath.ResolveReferentPathBounded(request.Path, journalrecovery.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		physical, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(path, journalrecovery.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		covered[physical.Exact()] = true
	}
	controlAuthorities := make([]pathauthority.Exact, 0, len(controls))
	for _, control := range controls {
		physical, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(control, journalrecovery.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return err
		}
		controlAuthorities = append(controlAuthorities, physical.Exact())
	}
	for _, document := range input.Protection.Documents() {
		if !covered[document.PathAuthority()] {
			return fmt.Errorf("protected document has no admitted physical request")
		}
		for _, control := range controlAuthorities {
			if document.PathAuthority().Contains(control) || control.Contains(document.PathAuthority()) {
				return fmt.Errorf("protected document overlaps a control artifact")
			}
		}
	}
	return nil
}

func finalizeInvocation(ctx context.Context, prepared Prepared, store ownershipstore.Store, armed ownership.PendingProtection, record *Record, completion *subprocess.ProtectedCommandCompletion, publicationAcknowledged bool, gate func(context.Context) error) (*Record, error) {
	current, err := store.LoadProtectionRegistry(ctx)
	row, present := current.Protection(armed.Invocation())
	if err == nil && !present && !publicationAcknowledged && completion == nil {
		return nil, nil
	}
	if err == nil && !present {
		err = fmt.Errorf("invocation protection is no longer present")
	}
	if err == nil && row.Phase() == ownership.ProtectionArmed {
		if !row.Equal(armed) || completion != nil && completion.ReleaseAttempted() {
			err = fmt.Errorf("unbound invocation protection changed")
		}
	}
	if err == nil && row.Phase() == ownership.ProtectionBound {
		if completion == nil {
			err = fmt.Errorf("bound invocation has no observed completion")
		} else if binding, observed := completion.Binding(); observed {
			expected, bindErr := armed.Bind(binding)
			if bindErr != nil || !row.Equal(expected) {
				err = errors.Join(fmt.Errorf("invocation protection binding changed"), bindErr)
			}
		} else {
			err = fmt.Errorf("bound invocation has no observed helper binding")
		}
	}
	if err != nil {
		record.cause = refusal(RegistryUnqualified, err)
		return record, record.cause
	}
	record.phase, record.settlement = row.Phase(), SettlementPending
	secure := restrictProtectionTargets(ctx, prepared, store, row, record, nil, gate)
	if secure {
		record.cause = requireQuiescence(ctx, row)
		if record.cause == nil {
			record.cause = gate(ctx)
		}
		if record.cause == nil {
			record.settlement, record.cause = settleProtection(ctx, store, prepared.owner, row)
		}
	}
	var failures []error
	failures = append(failures, record.cause)
	for _, target := range record.targets {
		failures = append(failures, target.cause)
		if target.expanded {
			failures = append(failures, ErrInvocationPermissionsExpanded)
		}
	}
	return record, errors.Join(failures...)
}
