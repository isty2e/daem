package execute

import "context"

// visibilityEffectGate keeps authority validation adjacent to each namespace-
// visible effect without making effect owners depend on workflow leases.
type visibilityEffectGate struct {
	before func(context.Context) error
	after  func(context.Context) error
}

func (gate visibilityEffectGate) validateBefore(ctx context.Context) error {
	if gate.before == nil {
		return nil
	}
	return gate.before(ctx)
}

func (gate visibilityEffectGate) acceptAfter(ctx context.Context) error {
	if gate.after == nil {
		return nil
	}
	return gate.after(ctx)
}
