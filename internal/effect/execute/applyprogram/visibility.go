package applyprogram

import "context"

// VisibilityGate keeps authority validation adjacent to each namespace-
// visible effect without making effect owners depend on workflow leases.
type VisibilityGate struct {
	before   func(context.Context) error
	after    func(context.Context) error
	schedule *applyVisibilityExecution
}

func NewVisibilityGate(before, after func(context.Context) error) VisibilityGate {
	return VisibilityGate{before: before, after: after}
}

func (gate VisibilityGate) ValidateBefore(ctx context.Context) error {
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

func (gate VisibilityGate) ApplyEffect(action func() error) error {
	if gate.schedule != nil {
		return gate.schedule.apply(action)
	}
	return action()
}

func (gate VisibilityGate) Observe(suffix string, action func() error) error {
	if gate.schedule != nil {
		return gate.schedule.execution.Observe(
			gate.schedule.id+"/"+suffix,
			gate.schedule.validationFailure,
			action,
		)
	}
	return action()
}

func (gate VisibilityGate) AcceptAfter(ctx context.Context) error {
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
