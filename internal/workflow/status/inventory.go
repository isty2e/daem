package status

import (
	"slices"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type Inventory struct {
	Managed   []InventoryEntry
	Unmanaged []InventoryEntry
	Blocked   []InventoryEntry
}

type InventoryEntry struct {
	EntityID entity.ID
	Subject  topology.SubjectID
	Target   target.Target
	Targets  []target.Target
	Scope    target.Scope
	Path     string
	Hash     string
	Reason   reconcile.ActionReason
	Detail   string
}

func BuildInventory(file durable.Snapshot, planResult reconcile.Result, selection targetselection.Selection) Inventory {
	inventory := Inventory{
		Managed:   managedInventoryEntries(file, selection),
		Unmanaged: unmanagedInventoryEntries(planResult, selection),
		Blocked:   blockedOwnershipInventoryEntries(planResult, selection),
	}
	sortInventoryEntries(inventory.Managed)
	sortInventoryEntries(inventory.Unmanaged)
	sortInventoryEntries(inventory.Blocked)

	return inventory
}

func blockedOwnershipInventoryEntries(planResult reconcile.Result, selection targetselection.Selection) []InventoryEntry {
	entries := make([]InventoryEntry, 0)
	for _, decision := range planResult.ManagedPaths() {
		if !decision.IsBlocked() || !decision.Reason().IsOwnershipBlock() || !inventoryTargetsSelected(decision.ConsumerTargets(), selection) {
			continue
		}
		entries = append(entries, InventoryEntry{
			EntityID: inventoryEntityForSubject(decision.Subject()),
			Subject:  decision.Subject(), Targets: decision.ConsumerTargets(), Scope: decision.Scope(),
			Path: string(decision.Destination()), Reason: decision.Reason(), Detail: decision.Detail(),
		})
	}
	return entries
}

func managedInventoryEntries(file durable.Snapshot, selection targetselection.Selection) []InventoryEntry {
	paths := file.ManagedPaths()
	aggregates := file.ManagedAggregates()
	entries := make([]InventoryEntry, 0, len(paths)+len(aggregates))
	for _, state := range paths {
		consumers := state.ConsumerTargets()
		if !inventoryTargetsSelected(consumers, selection) {
			continue
		}
		entries = append(entries, InventoryEntry{
			EntityID: inventoryEntityForSubject(state.Subject()),
			Subject:  state.Subject(),
			Targets:  consumers,
			Scope:    state.Scope(),
			Path:     string(state.Destination()),
			Hash:     string(state.ContentHash()),
		})
	}
	for _, state := range aggregates {
		contribution := state.Contribution()
		if !selection.Includes(contribution.Target()) {
			continue
		}
		entries = append(entries, InventoryEntry{
			EntityID: inventoryEntityForSubject(state.Subject()),
			Subject:  state.Subject(),
			Targets:  []target.Target{contribution.Target()},
			Scope:    contribution.Scope(),
			Path:     contribution.AggregateRoot(),
		})
	}
	return entries
}

func unmanagedInventoryEntries(planResult reconcile.Result, selection targetselection.Selection) []InventoryEntry {
	entries := make([]InventoryEntry, 0)
	for _, decision := range planResult.ManagedPaths() {
		if !decision.IsBlocked() || decision.Reason() != reconcile.ReasonUnmanagedOutputExists ||
			!inventoryTargetsSelected(decision.ConsumerTargets(), selection) {
			continue
		}
		entries = append(entries, InventoryEntry{
			EntityID: inventoryEntityForSubject(decision.Subject()),
			Subject:  decision.Subject(), Targets: decision.ConsumerTargets(), Scope: decision.Scope(),
			Path: string(decision.Destination()), Hash: string(decision.LiveHash()), Reason: decision.Reason(), Detail: decision.Detail(),
		})
	}

	return entries
}

func inventoryEntityForSubject(subject topology.SubjectID) entity.ID {
	entityID, ok := topologyprojection.EntityID(subject)
	if !ok {
		return entity.ID{}
	}
	return entityID
}

func sortInventoryEntries(entries []InventoryEntry) {
	sort.SliceStable(entries, func(leftIndex int, rightIndex int) bool {
		left := entries[leftIndex]
		right := entries[rightIndex]
		if left.Target != right.Target {
			return left.Target < right.Target
		}
		if inventoryTargetKey(left.Targets) != inventoryTargetKey(right.Targets) {
			return inventoryTargetKey(left.Targets) < inventoryTargetKey(right.Targets)
		}
		if left.Scope != right.Scope {
			return left.Scope < right.Scope
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Subject != right.Subject {
			return topology.CompareSubjectID(left.Subject, right.Subject) < 0
		}
		return entity.Compare(left.EntityID, right.EntityID) < 0
	})
}

func inventoryTargetsSelected(values []target.Target, selection targetselection.Selection) bool {
	return slices.ContainsFunc(values, selection.Includes)
}

func inventoryTargetKey(values []target.Target) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, "\x00")
}
