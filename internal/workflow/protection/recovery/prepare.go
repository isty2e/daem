// Package recovery owns source-independent discovery and execution of pending
// permission protection. It never replays the protected command.
package recovery

import (
	"context"
	"errors"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/protection"
	"github.com/isty2e/daem/internal/effect/protection/namespace"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// Prepared contains frozen operation inputs, not retained roots or live grants.
// The containing operation owns its single execution lifecycle.
type Prepared struct {
	paths      daempaths.Paths
	owner      stateauthority.Authority
	rows       []ownership.PendingProtection
	budget     rootedpath.PhysicalTraversalBudget
	disclosure Plan
}

func (prepared Prepared) Disclosure() Plan { return prepared.disclosure.Clone() }

func (prepared Prepared) RefusedResult(cause error) protection.Result {
	result := prepared.unobservedResult()
	records := result.Records()
	for index := range records {
		records[index] = records[index].Refused(cause)
	}
	return protection.NewResult(records)
}

func (prepared Prepared) unobservedResult() protection.Result {
	records := prepared.disclosure.Records()
	for index := range records {
		records[index] = records[index].Unobserved()
	}
	return protection.NewResult(records)
}

func (prepared Prepared) WithBlocker(cause error) Prepared {
	prepared.disclosure.blocker = errors.Join(prepared.disclosure.blocker, cause)
	return prepared
}

func Prepare(ctx context.Context, paths daempaths.Paths, budget rootedpath.PhysicalTraversalBudget) (Prepared, bool, error) {
	registry, err := ownershipstore.ReadProtectionRegistryBounded(ctx, paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return Prepared{}, false, err
	}
	if len(registry.PendingProtections()) == 0 {
		return Prepared{}, false, nil
	}
	owner, err := namespace.ObserveOwner(paths, budget)
	if err != nil {
		return Prepared{}, false, err
	}
	rows := registry.ProtectionsForOwner(owner.Key())
	if len(rows) == 0 {
		return Prepared{}, false, nil
	}
	prepared := Prepared{paths: paths, owner: owner, rows: rows, budget: budget}
	containment := protection.NewContainment(namespace.NewResolution(paths, owner), budget)
	var records []protection.Record
	for _, row := range rows {
		records = append(records, containment.Assess(ctx, registry, row))
	}
	prepared.disclosure.records = protection.NewResult(records)
	return prepared, true, nil
}
