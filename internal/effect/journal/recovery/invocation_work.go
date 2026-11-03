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

// Plus composes independently projected work under the same operation limits.
func (work InvocationWork) Plus(additional InvocationWork) (InvocationWork, error) {
	if additional.artifact.entries > MaximumPhysicalEntries-work.artifact.entries || additional.artifact.bytes > MaximumPhysicalBytes-work.artifact.bytes {
		return InvocationWork{}, fmt.Errorf("combined invocation artifact work exceeds operation limits")
	}
	artifact := ArtifactWork{entries: work.artifact.entries + additional.artifact.entries, bytes: work.artifact.bytes + additional.artifact.bytes}
	return NewInvocationWork(append(slices.Clone(work.paths), additional.paths...), artifact)
}

type invocationReservationLedger struct {
	transferring bool
}

type invocationWorkTransfer struct {
	ledger          *invocationReservationLedger
	attachment      *InvocationWork
	attachmentTaken bool
	forward         InvocationWork
	safety          InvocationWork
	taken           bool
}

// InvocationWorkReservation copies share one transfer per phase under the operation's
// single writer. Child consumption cannot spend its peer's prepaid credit.
type InvocationWorkReservation struct{ state *invocationWorkTransfer }

// Reservations and transfers share the parent's single-writer ownership. Transfer
// closes admission while allowing already prepaid handles to transfer.
func (budget *PhysicalWorkBudget) ReserveInvocation(forward, safety InvocationWork) (InvocationWorkReservation, error) {
	return budget.reserveInvocation(nil, forward, safety)
}

// ReserveInvocationWithAttachment prepays post-prefix observation separately
// from command and safety work. All scheduled invocations share admission.
func (budget *PhysicalWorkBudget) ReserveInvocationWithAttachment(attachment, forward, safety InvocationWork) (InvocationWorkReservation, error) {
	return budget.reserveInvocation(&attachment, forward, safety)
}

func (budget *PhysicalWorkBudget) reserveInvocation(attachment *InvocationWork, forward, safety InvocationWork) (InvocationWorkReservation, error) {
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
	work := []InvocationWork{forward, safety}
	if attachment != nil {
		work = append([]InvocationWork{*attachment}, work...)
	}
	var paths, entries int
	var bytes int64
	for _, phase := range work {
		if phase.components > budget.pathComponentLimit-budget.pathComponents-paths {
			return InvocationWorkReservation{}, fmt.Errorf("invocation path work exceeds remaining operation capacity")
		}
		if phase.artifact.entries > budget.RemainingEntries()-entries || phase.artifact.bytes > budget.RemainingBytes()-bytes {
			return InvocationWorkReservation{}, fmt.Errorf("invocation artifact work exceeds remaining operation capacity")
		}
		paths += phase.components
		entries += phase.artifact.entries
		bytes += phase.artifact.bytes
	}

	// Failed shared admission retains successful earlier charges, but publishes
	// no reservation. Rejection cannot replenish operation credit on a retry.
	for _, phase := range work {
		for _, count := range phase.paths {
			if err := budget.AdmitPathComponents(count); err != nil {
				return InvocationWorkReservation{}, err
			}
		}
	}
	budget.entries += entries
	budget.bytes += bytes
	return InvocationWorkReservation{state: &invocationWorkTransfer{ledger: ledger, attachment: attachment, forward: forward, safety: safety}}, nil
}

// TakeAttachment transfers only the prepaid observation phase. Copied handles
// share this transfer, while command and safety credit remain unavailable here.
func (reservation InvocationWorkReservation) TakeAttachment() (*PhysicalWorkBudget, error) {
	state := reservation.state
	if state == nil || state.ledger == nil || state.attachment == nil {
		return nil, fmt.Errorf("invocation attachment reservation is unavailable")
	}
	if state.attachmentTaken {
		return nil, fmt.Errorf("invocation attachment work was already transferred")
	}
	state.ledger.transferring = true
	state.attachmentTaken = true
	return invocationAllocation(*state.attachment), nil
}

func (reservation InvocationWorkReservation) Take() (*PhysicalWorkBudget, *PhysicalWorkBudget, error) {
	state := reservation.state
	if state == nil || state.ledger == nil {
		return nil, nil, fmt.Errorf("invocation work reservation is unavailable")
	}
	if state.taken {
		return nil, nil, fmt.Errorf("invocation work was already transferred")
	}
	if state.attachment != nil && !state.attachmentTaken {
		return nil, nil, fmt.Errorf("invocation attachment work has not transferred")
	}
	state.ledger.transferring = true
	state.taken = true
	return invocationAllocation(state.forward), invocationAllocation(state.safety), nil
}

func invocationAllocation(work InvocationWork) *PhysicalWorkBudget {
	return &PhysicalWorkBudget{pathComponentLimit: work.components, entryLimit: work.artifact.entries, byteLimit: work.artifact.bytes}
}
