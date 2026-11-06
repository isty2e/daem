// Package hostmutation owns rooted ordinary host publication, retained
// preconditions and restoration sources. The containing transaction owns leases
// and semantic ordering.
package hostmutation

import (
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

type Operation struct {
	destinations mutationnamespace.Namespace
	filesystem   mutationfs.Store
	removals     *removaloperation.Operation
}

func New(destinations mutationnamespace.Namespace, filesystem mutationfs.Store, removals *removaloperation.Operation) *Operation {
	return &Operation{destinations: destinations, filesystem: filesystem, removals: removals}
}
