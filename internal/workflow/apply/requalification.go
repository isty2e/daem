package apply

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/nativeoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/reconcile"
)

type applyRequalificationPhase uint8

const (
	requalificationAfterNativePrefix applyRequalificationPhase = iota + 1
	requalificationAfterHostRoutes
	requalificationAfterProviders
	requalificationAfterDelegates
)

func (phase applyRequalificationPhase) label() (string, error) {
	switch phase {
	case requalificationAfterNativePrefix:
		return "post-native-prefix replan", nil
	case requalificationAfterHostRoutes:
		return "post-host-route native suffix", nil
	case requalificationAfterProviders:
		return "post-native-provider replan", nil
	case requalificationAfterDelegates:
		return "post-native-delegate suffix", nil
	default:
		return "", fmt.Errorf("unknown apply requalification phase")
	}
}

type applyRequalificationInput struct {
	request                   CommandInput
	operationContext          reconcile.OperationContext
	current                   commandPlan
	currentAuthority          applyAuthorityEvidence
	originalOwnership         []observe.OwnershipObservation
	schedule                  applyForwardEffectSchedule
	guard                     applyExecutionGuard
	leases                    *mutation.LeaseSet
	validateBarrier           func(context.Context) error
	stateDirBoundaryValidated bool
}

type requalifiedApplyPlan struct {
	plan                      commandPlan
	authority                 applyAuthorityEvidence
	revisions                 mutation.RevisionSet
	stateDirBoundaryValidated bool
}

type requalifiedNativePrefix struct {
	continuation applyContinuationPlan
	registry     ownership.Registry
	scope        ownership.DocumentScope
}

type requalifiedNativeSuffix struct {
	input nativeoperation.NativeApplyInput
	plan  applyprogram.Plan
}

// requalifyApplyPlan owns the live-plan handoff. The phase callback proves the
// observed outcome and binds its already-reserved remainder, not new authority.
func requalifyApplyPlan[T any](
	ctx context.Context,
	input applyRequalificationInput,
	phase applyRequalificationPhase,
	qualify func(context.Context, *commandPlan) (T, error),
) (_ requalifiedApplyPlan, _ T, returnErr error) {
	var empty T
	label, err := phase.label()
	if err != nil {
		return requalifiedApplyPlan{}, empty, err
	}
	if err := input.guard.requireDeclarationsCurrent(ctx, label); err != nil {
		return requalifiedApplyPlan{}, empty, err
	}
	fresh, err := planReadinessAtPathsWithAssessor(ctx, input.request, input.operationContext, input.current.context.Paths, &input.current.barrier, input.validateBarrier, retainedReadiness)
	if err != nil {
		return requalifiedApplyPlan{}, empty, err
	}
	keep := false
	defer func() {
		if !keep {
			fresh.projectRoot = nil
			returnErr = errors.Join(returnErr, closeCommandPlan(&fresh))
		}
	}()
	fresh.projectRoot = input.current.projectRoot
	if input.schedule.managedSuffix != nil {
		fresh.assessment.Ownership = input.originalOwnership
	}
	remainder, err := qualify(ctx, &fresh)
	if err != nil {
		return requalifiedApplyPlan{}, empty, err
	}
	authority, err := buildApplyAuthorityEvidence(ctx, fresh)
	if err != nil {
		return requalifiedApplyPlan{}, empty, err
	}
	revisions, err := qualifyPostEffectAuthority(ctx, phase, input.currentAuthority, authority, input.leases, input.guard.planWasDisclosed)
	if err != nil {
		return requalifiedApplyPlan{}, empty, err
	}
	previous := input.current
	previous.projectRoot = nil
	if err := closeCommandPlan(&previous); err != nil {
		return requalifiedApplyPlan{}, empty, err
	}
	fresh.result.Reconciliation = input.current.result.Reconciliation
	keep = true
	return requalifiedApplyPlan{
		plan: fresh, authority: authority, revisions: revisions,
		stateDirBoundaryValidated: (phase == requalificationAfterHostRoutes || phase == requalificationAfterDelegates) && input.stateDirBoundaryValidated,
	}, remainder, nil
}

type requalificationLeases interface {
	CoversPhysicalAuthority(mutation.PhysicalAuthoritySet) (bool, error)
	MatchCurrentPhysicalRequests(context.Context, ...mutation.PhysicalAuthorityRequest) (bool, error)
}

func qualifyPostEffectAuthority(ctx context.Context, phase applyRequalificationPhase, previous, fresh applyAuthorityEvidence, leases requalificationLeases, disclosed bool) (mutation.RevisionSet, error) {
	if _, err := phase.label(); err != nil {
		return mutation.RevisionSet{}, err
	}
	// Prefix settlement and host commands replace identity facts. Their complete
	// observed state/Registry and reserved remainder are checked by the phase.
	// Provider installation must preserve all remaining stable authority facts.
	if phase == requalificationAfterProviders && !operationplan.FactsCover(previous.facts, fresh.facts) {
		return mutation.RevisionSet{}, staleApplyError(disclosed, nil)
	}
	if covered, err := leases.CoversPhysicalAuthority(fresh.documentWrites); err != nil || !covered {
		return mutation.RevisionSet{}, staleApplyError(disclosed, err)
	}
	if matches, err := leases.MatchCurrentPhysicalRequests(ctx, fresh.metadataRequests...); err != nil || !matches {
		return mutation.RevisionSet{}, staleApplyError(disclosed, err)
	}
	return mutation.CaptureRevisionSet(ctx, fresh.firstEffectRevisions...)
}
