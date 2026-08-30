package recoverygate

import "context"

type barrierPhase string

const (
	barrierPhasePreCreate  barrierPhase = "pre_create"
	barrierPhasePostCreate barrierPhase = "post_create"
	barrierPhasePostAccept barrierPhase = "post_accept"
)

// barrierFaultPlan is a test-only injection seam for first-incarnation
// visibility. Production callers pass the zero value.
type barrierFaultPlan struct {
	failures map[barrierPhase]error
	actions  map[barrierPhase]func()
}

func (plan barrierFaultPlan) check(ctx context.Context, current barrierPhase) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if action := plan.actions[current]; action != nil {
		action()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return plan.failures[current]
}
