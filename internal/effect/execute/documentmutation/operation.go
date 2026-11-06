// Package documentmutation publishes and restores canonical document evidence
// through the selected namespace, removal permission and work allocation.
package documentmutation

import (
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

type Operation struct {
	destinations       mutationnamespace.Namespace
	filesystem         mutationfs.Store
	removals           *removaloperation.Operation
	physicalWorkBudget *physicalwork.Budget
}

func New(destinations mutationnamespace.Namespace, filesystem mutationfs.Store, removals *removaloperation.Operation, budget *physicalwork.Budget) *Operation {
	return &Operation{destinations: destinations, filesystem: filesystem, removals: removals, physicalWorkBudget: budget}
}

type restorationOutcome struct {
	err             error
	commitAttempted bool
}

func (outcome restorationOutcome) Err() error { return outcome.err }

func attemptedRestoration(err error) restorationOutcome {
	return restorationOutcome{err: err, commitAttempted: true}
}
