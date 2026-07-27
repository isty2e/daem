package apply

import (
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type managedPathFingerprintFacts struct {
	Kind          reconcile.ManagedPathDecisionKind
	Subject       topology.SubjectID
	Consumers     []string
	Scope         string
	Destination   string
	DesiredHash   string
	LiveHash      string
	ContentKind   realization.PathProjectionContentKind
	PlacementMode realization.PathProjectionMode
	Reason        reconcile.ActionReason
	Detail        string
	Previous      *managedPathPreviousFingerprintFacts
}

type managedPathPreviousFingerprintFacts struct {
	Subject     topology.SubjectID
	Consumers   []string
	Scope       string
	Destination string
	ContentHash string
	ContentKind realization.PathProjectionContentKind
}

func managedPathFingerprintRows(decisions []reconcile.ManagedPathDecision) []managedPathFingerprintFacts {
	rows := make([]managedPathFingerprintFacts, 0, len(decisions))
	for _, decision := range decisions {
		row := managedPathFingerprintFacts{
			Kind: decision.Kind(), Subject: decision.Subject(), Consumers: targetValues(decision.ConsumerTargets()),
			Scope: string(decision.Scope()), Destination: decision.Destination().String(),
			DesiredHash: string(decision.DesiredHash()), LiveHash: string(decision.LiveHash()),
			ContentKind: decision.ContentKind(), PlacementMode: decision.PlacementMode(),
			Reason: decision.Reason(), Detail: decision.Detail(),
		}
		if previous, present := decision.PreviousState(); present {
			row.Previous = &managedPathPreviousFingerprintFacts{
				Subject: previous.Subject(), Consumers: targetValues(previous.ConsumerTargets()),
				Scope: string(previous.Scope()), Destination: previous.Destination().String(),
				ContentHash: string(previous.ContentHash()), ContentKind: previous.ContentKind(),
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func targetValues(values []target.Target) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}
