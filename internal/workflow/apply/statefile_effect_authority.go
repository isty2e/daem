package apply

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

type statefileEffectPlan struct {
	validations int
	fileCommits int
}

func (plan statefileEffectPlan) empty() bool {
	return plan.validations == 0 && plan.fileCommits == 0
}

func (plan *statefileEffectPlan) add(validations int, fileCommits int) error {
	if plan == nil {
		return fmt.Errorf("statefile effect plan is required")
	}
	var err error
	plan.validations, err = checkedStatefileEffectCount(plan.validations, validations)
	if err != nil {
		return err
	}
	plan.fileCommits, err = checkedStatefileEffectCount(plan.fileCommits, fileCommits)
	return err
}

func checkedStatefileEffectCount(current int, added int) (int, error) {
	if current < 0 || added < 0 || current > int(^uint(0)>>1)-added {
		return 0, fmt.Errorf("statefile effect count overflows")
	}
	return current + added, nil
}

type boundStatefileEffectAuthority interface {
	Validate(context.Context) error
	Entry() *rootedpath.EntryAuthority
	Close() error
}

type statefileEffectReservation interface {
	Bind(context.Context) (boundStatefileEffectAuthority, error)
}

type transactionStatefileEffectReservation struct {
	reservation *transaction.StateDirDescendantReservation
}

func (reservation transactionStatefileEffectReservation) Bind(
	ctx context.Context,
) (boundStatefileEffectAuthority, error) {
	return reservation.reservation.Bind(ctx)
}

type reserveStatefileEffectAuthority func(
	string,
	statefileEffectPlan,
) (statefileEffectReservation, error)

type statefileEffectAuthority struct {
	mu                   sync.Mutex
	reservation          statefileEffectReservation
	bound                boundStatefileEffectAuthority
	remainingValidations int
	remainingFileCommits int
	closed               bool
}

func newStatefileEffectAuthority(
	statePath string,
	plan statefileEffectPlan,
	reserve reserveStatefileEffectAuthority,
) (*statefileEffectAuthority, error) {
	if plan.empty() {
		return nil, nil
	}
	if reserve == nil {
		return nil, fmt.Errorf("statefile effect reservation is required")
	}
	reservation, err := reserve(statePath, plan)
	if err != nil {
		return nil, err
	}
	return newStatefileEffectAuthorityFromReservation(plan, reservation)
}

func newStatefileEffectAuthorityFromReservation(
	plan statefileEffectPlan,
	reservation statefileEffectReservation,
) (*statefileEffectAuthority, error) {
	if plan.empty() {
		return nil, nil
	}
	if reservation == nil {
		return nil, fmt.Errorf("statefile effect reservation is unavailable")
	}
	return &statefileEffectAuthority{
		reservation:          reservation,
		remainingValidations: plan.validations,
		remainingFileCommits: plan.fileCommits,
	}, nil
}

func (authority *statefileEffectAuthority) Ensure(ctx context.Context) error {
	if authority == nil {
		return fmt.Errorf("statefile effect authority is required")
	}
	authority.mu.Lock()
	if authority.closed {
		authority.mu.Unlock()
		return fmt.Errorf("statefile effect authority is closed")
	}
	if authority.bound != nil {
		authority.mu.Unlock()
		return authority.Validate(ctx)
	}
	reservation := authority.reservation
	authority.mu.Unlock()
	if reservation == nil {
		return fmt.Errorf("statefile effect reservation is unavailable")
	}
	bound, err := reservation.Bind(ctx)
	if err != nil {
		return err
	}
	authority.mu.Lock()
	if authority.closed || authority.bound != nil {
		authority.mu.Unlock()
		return errors.Join(
			fmt.Errorf("statefile effect authority binding changed concurrently"),
			bound.Close(),
		)
	}
	authority.bound = bound
	authority.reservation = nil
	authority.mu.Unlock()
	return nil
}

func (authority *statefileEffectAuthority) Validate(ctx context.Context) error {
	if authority == nil {
		return fmt.Errorf("statefile effect authority is required")
	}
	authority.mu.Lock()
	if authority.closed || authority.bound == nil {
		authority.mu.Unlock()
		return fmt.Errorf("statefile effect authority is unbound")
	}
	if authority.remainingValidations == 0 {
		authority.mu.Unlock()
		return fmt.Errorf("statefile effect validation exceeded its reserved plan")
	}
	authority.remainingValidations--
	bound := authority.bound
	authority.mu.Unlock()
	return bound.Validate(ctx)
}

func (authority *statefileEffectAuthority) EntryForCommit() (*rootedpath.EntryAuthority, error) {
	if authority == nil {
		return nil, fmt.Errorf("statefile effect authority is required")
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed || authority.bound == nil {
		return nil, fmt.Errorf("statefile effect authority is unbound")
	}
	if authority.remainingFileCommits == 0 {
		return nil, fmt.Errorf("statefile commit exceeded its reserved plan")
	}
	entry := authority.bound.Entry()
	if entry == nil {
		return nil, fmt.Errorf("statefile effect entry authority is unavailable")
	}
	authority.remainingFileCommits--
	return entry, nil
}

func (authority *statefileEffectAuthority) Close() error {
	if authority == nil {
		return nil
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed {
		return nil
	}
	authority.closed = true
	if authority.bound == nil {
		authority.reservation = nil
		return nil
	}
	err := authority.bound.Close()
	authority.bound = nil
	authority.reservation = nil
	return err
}

func statefileEffectPlanFor(
	current durable.Snapshot,
	reconciliation reconcile.Result,
) (statefileEffectPlan, error) {
	host, err := hostRouteStatefileEffectPlan(current, reconciliation.Relations())
	if err != nil {
		return statefileEffectPlan{}, err
	}
	carrier, err := carrierRemovalStatefileEffectPlan(reconciliation.CarrierAbsences())
	if err != nil {
		return statefileEffectPlan{}, err
	}
	delegate, err := delegateStatefileEffectPlan(reconciliation.Delegates())
	if err != nil {
		return statefileEffectPlan{}, err
	}
	if err := host.add(carrier.validations, carrier.fileCommits); err != nil {
		return statefileEffectPlan{}, err
	}
	if err := host.add(delegate.validations, delegate.fileCommits); err != nil {
		return statefileEffectPlan{}, err
	}
	return host, nil
}

// hostRouteStatefileEffectPlan reserves the maximum branch envelope before
// host effects. Project routes need seven validations and four statefile
// commits; global routes add three validations around registry convergence.
func hostRouteStatefileEffectPlan(
	current durable.Snapshot,
	actions []reconcile.RelationAction,
) (statefileEffectPlan, error) {
	var plan statefileEffectPlan
	for _, action := range actions {
		switch {
		case action.InvokesHostRoute():
			validations := 7
			if action.Scope() == target.ScopeGlobal {
				validations = 10
			}
			if err := plan.add(validations, 4); err != nil {
				return statefileEffectPlan{}, err
			}
		case isGlobalCarrierPromotionCandidate(current, action):
			if err := plan.add(4, 1); err != nil {
				return statefileEffectPlan{}, err
			}
		}
	}
	return plan, nil
}

// carrierRemovalStatefileEffectPlan uses the global delegated-removal maximum:
// batch binding, pre/post command checks, attempt persistence, and three
// registry-retirement checks around at most three statefile commits.
func carrierRemovalStatefileEffectPlan(
	actions []carrierabsence.Action,
) (statefileEffectPlan, error) {
	var plan statefileEffectPlan
	for _, action := range actions {
		if !action.InvokesHostRoute() && !action.MutatesDirectProjection() &&
			!action.VerifiesPendingRemoval() {
			continue
		}
		if err := plan.add(8, 3); err != nil {
			return statefileEffectPlan{}, err
		}
	}
	return plan, nil
}

// delegateStatefileEffectPlan reserves one pre- and post-invocation validation
// per delegate, one batch binding check, and two final persistence checks.
func delegateStatefileEffectPlan(
	actions []reconcile.DelegateAction,
) (statefileEffectPlan, error) {
	if !delegateActionsRequireAttemptPersistence(actions) {
		return statefileEffectPlan{}, nil
	}
	validations, err := checkedStatefileEffectCount(len(actions), len(actions))
	if err != nil {
		return statefileEffectPlan{}, err
	}
	validations, err = checkedStatefileEffectCount(validations, 3)
	if err != nil {
		return statefileEffectPlan{}, err
	}
	return statefileEffectPlan{validations: validations, fileCommits: 1}, nil
}
