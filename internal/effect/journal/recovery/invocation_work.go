package recovery

import (
	"fmt"
	"slices"
)

// InvocationWork describes already compiled bounded passes, not filesystem
// targets or authority. Every component count is one bounded admission call.
type InvocationWork struct {
	paths      []int
	components int
	artifact   ArtifactWork
}

func NewInvocationWork(pathPasses []int, artifact ArtifactWork) (InvocationWork, error) {
	if artifact.entries < 0 || artifact.entries > MaximumPhysicalEntries || artifact.bytes < 0 || artifact.bytes > MaximumPhysicalBytes {
		return InvocationWork{}, fmt.Errorf("invocation artifact work exceeds operation limits")
	}
	total := 0
	for _, count := range pathPasses {
		if count < 0 || count > MaximumPhysicalPathDepth || count > maximumPhysicalPathComponentVisits-total {
			return InvocationWork{}, fmt.Errorf("invocation path work exceeds operation limits")
		}
		total += count
	}
	return InvocationWork{paths: slices.Clone(pathPasses), components: total, artifact: artifact}, nil
}

type invocationReservationLedger struct {
	transferring bool
}

type invocationWorkTransfer struct {
	ledger  *invocationReservationLedger
	forward InvocationWork
	safety  InvocationWork
	taken   bool
}

// InvocationWorkReservation copies share one transfer under the operation's
// single writer. Child consumption cannot spend its peer's prepaid credit.
type InvocationWorkReservation struct{ state *invocationWorkTransfer }

// ReserveInvocation and Take share the parent's single-writer ownership. Take
// closes admission while allowing already prepaid handles to transfer.
func (budget *PhysicalWorkBudget) ReserveInvocation(forward, safety InvocationWork) (InvocationWorkReservation, error) {
	if budget == nil {
		return InvocationWorkReservation{}, fmt.Errorf("physical work budget is required")
	}
	if budget.invocationReservations == nil {
		budget.invocationReservations = &invocationReservationLedger{}
	}
	ledger := budget.invocationReservations
	if ledger.transferring {
		return InvocationWorkReservation{}, fmt.Errorf("invocation admission is closed after transfer")
	}
	if forward.components > budget.pathComponentLimit-budget.pathComponents || safety.components > budget.pathComponentLimit-budget.pathComponents-forward.components {
		return InvocationWorkReservation{}, fmt.Errorf("invocation path work exceeds remaining operation capacity")
	}
	if forward.artifact.entries > budget.RemainingEntries() || safety.artifact.entries > budget.RemainingEntries()-forward.artifact.entries || forward.artifact.bytes > budget.RemainingBytes() || safety.artifact.bytes > budget.RemainingBytes()-forward.artifact.bytes {
		return InvocationWorkReservation{}, fmt.Errorf("invocation artifact work exceeds remaining operation capacity")
	}

	// Failed shared admission retains successful earlier charges, but publishes
	// no reservation. Rejection cannot replenish operation credit on a retry.
	for _, work := range []InvocationWork{forward, safety} {
		for _, count := range work.paths {
			if err := budget.AdmitPathComponents(count); err != nil {
				return InvocationWorkReservation{}, err
			}
		}
	}
	budget.entries += forward.artifact.entries + safety.artifact.entries
	budget.bytes += forward.artifact.bytes + safety.artifact.bytes
	return InvocationWorkReservation{state: &invocationWorkTransfer{ledger: ledger, forward: forward, safety: safety}}, nil
}

func (reservation InvocationWorkReservation) Take() (*PhysicalWorkBudget, *PhysicalWorkBudget, error) {
	state := reservation.state
	if state == nil || state.ledger == nil {
		return nil, nil, fmt.Errorf("invocation work reservation is unavailable")
	}
	if state.taken {
		return nil, nil, fmt.Errorf("invocation work was already transferred")
	}
	state.ledger.transferring = true
	state.taken = true
	allocation := func(work InvocationWork) *PhysicalWorkBudget {
		return &PhysicalWorkBudget{pathComponentLimit: work.components, entryLimit: work.artifact.entries, byteLimit: work.artifact.bytes}
	}
	return allocation(state.forward), allocation(state.safety), nil
}
