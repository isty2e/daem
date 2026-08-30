package apply

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/recoverygate"
)

type statefileEffectPlan struct {
	validations int
	fileCommits int
}

func (plan statefileEffectPlan) empty() bool {
	return plan.validations == 0 && plan.fileCommits == 0
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
	reservation *recoverygate.StateDirDescendantReservation
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
