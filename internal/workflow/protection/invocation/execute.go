package invocation

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/protection"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
)

var ErrInvocationPermissionsExpanded = errors.New("protected file permissions expanded during invocation")

// InvocationInput borrows the containing operation's exact leases and complete
// prepaid work. Selection, approval, prefix/suffix and lease lifetime stay there.
type InvocationInput struct {
	Paths         daempaths.Paths
	Protection    ownership.PendingProtection
	Leases        *mutation.LeaseSet
	Requests      []mutation.PhysicalAuthorityRequest
	qualification *invocationQualification
}

type InvocationResult struct {
	process    *subprocess.ProtectedCommandAttemptResult
	protection *protection.Record
}

func (result InvocationResult) Process() (subprocess.ProtectedCommandAttemptResult, bool) {
	if result.process == nil {
		return subprocess.ProtectedCommandAttemptResult{}, false
	}
	return *result.process, true
}

func (result InvocationResult) Protection() (protection.Record, bool) {
	if result.protection == nil {
		return protection.Record{}, false
	}
	return *result.protection, true
}

// ExecuteInvocation owns Arm/Bind and immediate metadata settlement, not key
// correction or containing-operation completion. Correlation publication, when
// needed, runs after Arm and must use only the supplied forward allocation.
func ExecuteInvocation(ctx context.Context, input InvocationInput, executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, bind subprocess.WorkingDirectoryBinder, prepareCorrelation func(context.Context, *physicalwork.Budget) error) (result InvocationResult, returnErr error) {
	if ctx == nil || !input.matchesPreparation() {
		return result, fmt.Errorf("protected invocation requires an armed candidate and containing authority")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	forward, safety, err := input.qualification.work.Take()
	if err != nil {
		return result, err
	}
	gate := func(ctx context.Context, budget *physicalwork.Budget) error {
		matches, err := input.qualification.authority.MatchCurrent(ctx, budget)
		if err != nil || !matches {
			return errors.Join(mutation.StaleSnapshotError{}, err)
		}
		return nil
	}
	if err := gate(ctx, forward); err != nil {
		return result, err
	}
	owner := input.Protection.Owner()
	forwardStore, root, err := input.qualification.registry.OpenBounded(ctx, forward)
	if err != nil {
		return result, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	safetyStore, safetyRoot, err := input.qualification.registry.OpenBounded(ctx, safety)
	if err != nil {
		return result, err
	}
	defer func() { returnErr = errors.Join(returnErr, safetyRoot.Close()) }()
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
	if _, _, err := current.ApplyProtection(owner, arm); err != nil {
		return result, err
	}
	if err := qualifyInvocationStart(ctx, input.qualification, input.Protection, forward); err != nil {
		return result, err
	}
	if err := gate(ctx, forward); err != nil {
		return result, err
	}

	record := protection.Unobserved(input.Protection)
	result.protection = &record
	containment := protection.NewContainment(input.qualification, safety)
	finalized := false
	publicationAcknowledged := false
	finish := func(ctx context.Context, completion *subprocess.ProtectedCommandCompletion) error {
		if finalized {
			return fmt.Errorf("protected invocation was already finalized")
		}
		finalized = true
		var err error
		result.protection, err = finalizeInvocation(ctx, containment, safetyStore, input.Protection, record, completion, publicationAcknowledged, func(ctx context.Context) error { return gate(ctx, safety) })
		return err
	}
	defer func() {
		if !finalized {
			returnErr = errors.Join(returnErr, finish(context.WithoutCancel(ctx), nil))
		}
	}()
	if _, err := forwardStore.TransitionProtection(ctx, owner, arm); err != nil {
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
		if err := qualifyInvocationStart(ctx, input.qualification, input.Protection, forward); err != nil {
			return err
		}
		transition, err := ownership.NewBindProtection(input.Protection, binding)
		if err != nil {
			return err
		}
		_, err = forwardStore.TransitionProtection(ctx, owner, transition)
		return err
	}, func(ctx context.Context, completion subprocess.ProtectedCommandCompletion) error {
		return finish(ctx, &completion)
	})
	result.process = &process
	return result, err
}

func qualifyInvocationStart(ctx context.Context, qualified *invocationQualification, row ownership.PendingProtection, budget *physicalwork.Budget) error {
	containment := protection.NewContainment(qualified, budget)
	for _, document := range row.Documents() {
		observation, err := containment.InspectDocument(ctx, document, document.InitialRestriction)
		if err != nil || observation.ObservedAbsent() {
			return errors.Join(fmt.Errorf("protected document changed before invocation"), err)
		}
	}
	return nil
}

func finalizeInvocation(ctx context.Context, containment protection.Containment, store ownershipstore.Store, armed ownership.PendingProtection, record protection.Record, completion *subprocess.ProtectedCommandCompletion, publicationAcknowledged bool, gate func(context.Context) error) (*protection.Record, error) {
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
		record = record.WithFailure(protection.Refusal(protection.RegistryUnqualified, err))
		return &record, record.Cause()
	}
	record = containment.Settle(ctx, store, row, record, nil, gate, func(ctx context.Context) error {
		if err := protection.RequireQuiescence(ctx, row); err != nil {
			return err
		}
		return gate(ctx)
	})
	var failures []error
	failures = append(failures, record.Cause())
	for _, target := range record.Targets() {
		failures = append(failures, target.Cause())
		if target.Expanded() {
			failures = append(failures, ErrInvocationPermissionsExpanded)
		}
	}
	return &record, errors.Join(failures...)
}
