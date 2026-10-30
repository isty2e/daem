package permissions

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalrecovery "github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess/executioncontext"
)

// Prepared contains frozen operation inputs, not retained roots or live grants.
// The parent Recover capability owns its single execution lifecycle.
type Prepared struct {
	paths      daempaths.Paths
	owner      stateauthority.Authority
	rows       []ownership.PendingProtection
	budget     rootedpath.PhysicalTraversalBudget
	disclosure Plan
}

func (prepared Prepared) Disclosure() Plan { return prepared.disclosure.Clone() }

func (prepared Prepared) RefusedResult(cause error) Result {
	result := prepared.unobservedResult()
	for index := range result.records {
		result.records[index].cause = cause
		for targetIndex := range result.records[index].targets {
			result.records[index].targets[targetIndex].state = TargetBlocked
			result.records[index].targets[targetIndex].cause = cause
		}
	}
	return result
}

func (prepared Prepared) unobservedResult() Result {
	result := Result{records: prepared.disclosure.Records()}
	for index := range result.records {
		result.records[index].settlement = SettlementUnknown
	}
	return result
}

func (prepared Prepared) WithBlocker(cause error) Prepared {
	prepared.disclosure.blocker = errors.Join(prepared.disclosure.blocker, cause)
	return prepared
}

func Prepare(ctx context.Context, paths daempaths.Paths, budget rootedpath.PhysicalTraversalBudget) (Prepared, bool, error) {
	registry, err := ownershipstore.ReadProtectionRegistryBounded(ctx, paths.OwnershipRegistryPath, journalrecovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return Prepared{}, false, err
	}
	if len(registry.PendingProtections()) == 0 {
		return Prepared{}, false, nil
	}
	owner, err := observeOwner(paths, budget)
	if err != nil {
		return Prepared{}, false, err
	}
	rows := registry.ProtectionsForOwner(owner.Key())
	if len(rows) == 0 {
		return Prepared{}, false, nil
	}
	prepared := Prepared{paths: paths, owner: owner, rows: rows, budget: budget}
	for _, row := range rows {
		record := Record{invocation: row.Invocation(), phase: row.Phase(), settlement: SettlementPending}
		processErr := requireQuiescence(ctx, row)
		for _, document := range row.Documents() {
			target := Target{destination: document.Bindings()[0].Destination(), state: TargetCheck, cause: processErr}
			if target.cause == nil {
				target = inspectTarget(ctx, prepared, registry, row, document)
			}
			if target.cause != nil {
				target.state = TargetBlocked
			}
			record.targets = append(record.targets, target)
		}
		prepared.disclosure.records = append(prepared.disclosure.records, record)
	}
	return prepared, true, nil
}

func observeOwner(paths daempaths.Paths, budget rootedpath.PhysicalTraversalBudget) (stateauthority.Authority, error) {
	authority, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(paths.StatefilePath, journalrecovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return stateauthority.Authority{}, err
	}
	return stateauthority.New(authority.Exact(), paths.ManifestPath)
}

func requireQuiescence(ctx context.Context, row ownership.PendingProtection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	binding, bound := row.Execution()
	if !bound {
		return nil
	}
	observation, err := executioncontext.ObserveExecutionQuiescence(ctx, binding)
	if err != nil {
		return err
	}
	if err := observation.Require(binding); err != nil {
		return fmt.Errorf("permission recovery quiescence: %w", err)
	}
	return nil
}
