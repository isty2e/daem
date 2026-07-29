package reconcile

import "fmt"

// ReplayInstallRoute returns the same admitted carrier install route as an
// executable action. It is used only when a fresh dependent-artifact
// observation requires idempotent route replay despite an already-present
// host relation. The relation postcondition remains separate from the
// dependent artifact's postcondition.
func (action RelationAction) ReplayInstallRoute() (RelationAction, error) {
	if action.BlocksOrdinaryApply() {
		return RelationAction{}, fmt.Errorf("blocked carrier relation cannot replay its install route")
	}
	if !action.admission.AllowsHostRouteInvocation() {
		return RelationAction{}, fmt.Errorf("carrier relation does not admit host-route invocation")
	}
	if err := action.routeRequest.Validate(); err != nil {
		return RelationAction{}, fmt.Errorf("carrier install replay request: %w", err)
	}
	replayed := action
	replayed.kind = ActionCreate
	replayed.reason = ReasonNone
	replayed.execution = ExecutionHostRoute
	return replayed, nil
}
