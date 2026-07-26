package projection

import (
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type managedPathDecision struct {
	input reconcile.ManagedPathDecisionInput
}

func newManagedPathDecision(
	input reconcile.ManagedPathDecisionInput,
	kind reconcile.ManagedPathDecisionKind,
	reason reconcile.ActionReason,
	detail string,
) managedPathDecision {
	input.Kind = kind
	input.Reason = reason
	input.Detail = detail
	return managedPathDecision{input: input}
}

func newManagedPathCreate(input reconcile.ManagedPathDecisionInput, reason reconcile.ActionReason) managedPathDecision {
	return newManagedPathDecision(input, reconcile.ManagedPathCreate, reason, "")
}

func newManagedPathReplace(input reconcile.ManagedPathDecisionInput, reason reconcile.ActionReason, detail string) managedPathDecision {
	return newManagedPathDecision(input, reconcile.ManagedPathReplace, reason, detail)
}

func newManagedPathRemove(input reconcile.ManagedPathDecisionInput, reason reconcile.ActionReason) managedPathDecision {
	return newManagedPathDecision(input, reconcile.ManagedPathRemove, reason, "")
}

func newManagedPathRecord(input reconcile.ManagedPathDecisionInput, reason reconcile.ActionReason, detail string) managedPathDecision {
	return newManagedPathDecision(input, reconcile.ManagedPathRecord, reason, detail)
}

func newManagedPathNoOp(input reconcile.ManagedPathDecisionInput, reason reconcile.ActionReason) managedPathDecision {
	return newManagedPathDecision(input, reconcile.ManagedPathNoOp, reason, "")
}

func newManagedPathBlocked(input reconcile.ManagedPathDecisionInput, reason reconcile.ActionReason, detail string) managedPathDecision {
	return newManagedPathDecision(input, reconcile.ManagedPathBlocked, reason, detail)
}

func (decision managedPathDecision) IsBlocked() bool {
	return decision.input.Kind == reconcile.ManagedPathBlocked
}

func (decision managedPathDecision) Kind() reconcile.ManagedPathDecisionKind {
	return decision.input.Kind
}
func (decision managedPathDecision) Reason() reconcile.ActionReason { return decision.input.Reason }
func (decision managedPathDecision) Detail() string                 { return decision.input.Detail }
func (decision managedPathDecision) DesiredFileMode() os.FileMode {
	return decision.input.DesiredFileMode
}

func (decision managedPathDecision) InvolvesScope(scope target.Scope) bool {
	if decision.input.Scope == scope {
		return true
	}
	return decision.input.Previous != nil && decision.input.Previous.Scope() == scope
}

func (decision managedPathDecision) canonical() (reconcile.ManagedPathDecision, error) {
	return reconcile.NewManagedPathDecision(decision.input)
}

func canonicalManagedPathDecisions(values []managedPathDecision) ([]reconcile.ManagedPathDecision, error) {
	result := make([]reconcile.ManagedPathDecision, 0, len(values))
	for index, value := range values {
		decision, err := value.canonical()
		if err != nil {
			return nil, fmt.Errorf("managed path decision[%d]: %w", index, err)
		}
		result = append(result, decision)
	}
	return result, nil
}

func compareManagedPathDecisions(left managedPathDecision, right managedPathDecision) int {
	if subject := topology.CompareSubjectID(left.input.Subject, right.input.Subject); subject != 0 {
		return subject
	}
	if left.input.Destination < right.input.Destination {
		return -1
	}
	if left.input.Destination > right.input.Destination {
		return 1
	}
	if left.input.Kind < right.input.Kind {
		return -1
	}
	if left.input.Kind > right.input.Kind {
		return 1
	}
	return 0
}
