package execute

import "context"

// visibilityEffectGate keeps authority validation adjacent to each namespace-
// visible effect without making effect owners depend on workflow leases.
type visibilityEffectGate struct {
	before   func(context.Context) error
	after    func(context.Context) error
	schedule *applyVisibilityExecution
}

func (gate visibilityEffectGate) validateBefore(ctx context.Context) error {
	action := func() error {
		if gate.before == nil {
			return nil
		}
		return gate.before(ctx)
	}
	if gate.schedule != nil {
		return gate.schedule.validate(action)
	}
	return action()
}

func (gate visibilityEffectGate) applyEffect(action func() error) error {
	if gate.schedule != nil {
		return gate.schedule.apply(action)
	}
	return action()
}

func (gate visibilityEffectGate) observe(suffix string, action func() error) error {
	if gate.schedule != nil {
		return gate.schedule.execution.runObservation(gate.schedule.id+"/"+suffix, action)
	}
	return action()
}

func (gate visibilityEffectGate) acceptAfter(ctx context.Context) error {
	action := func() error {
		if gate.after == nil {
			return nil
		}
		return gate.after(ctx)
	}
	if gate.schedule != nil {
		return gate.schedule.accept(action)
	}
	return action()
}
