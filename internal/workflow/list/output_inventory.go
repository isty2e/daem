package listworkflow

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// OutputInventory is one canonical, deterministic view of managed output
// authority and conflicting live occupancy.
type OutputInventory struct {
	managed   []OutputInventoryEntry
	unmanaged []OutputInventoryEntry
	blocked   []OutputInventoryEntry
}

// OutputInventoryEntry identifies one logical subject at one physical output.
// Aggregate entries intentionally carry no whole-document hash because daem
// owns only their selected contribution.
type OutputInventoryEntry struct {
	entityID    entity.ID
	subject     topology.SubjectID
	targets     []target.Target
	scope       target.Scope
	path        string
	contentPath output.ContentPath
	hash        string
	reason      reconcile.ActionReason
	detail      string
}

func (inventory OutputInventory) Managed() []OutputInventoryEntry {
	return cloneOutputInventoryEntries(inventory.managed)
}

func (inventory OutputInventory) Unmanaged() []OutputInventoryEntry {
	return cloneOutputInventoryEntries(inventory.unmanaged)
}

func (inventory OutputInventory) Blocked() []OutputInventoryEntry {
	return cloneOutputInventoryEntries(inventory.blocked)
}

func (entry OutputInventoryEntry) EntityID() entity.ID         { return entry.entityID }
func (entry OutputInventoryEntry) Subject() topology.SubjectID { return entry.subject }
func (entry OutputInventoryEntry) Targets() []target.Target {
	return append([]target.Target(nil), entry.targets...)
}
func (entry OutputInventoryEntry) Scope() target.Scope             { return entry.scope }
func (entry OutputInventoryEntry) Path() string                    { return entry.path }
func (entry OutputInventoryEntry) ContentPath() output.ContentPath { return entry.contentPath }
func (entry OutputInventoryEntry) Hash() string                    { return entry.hash }
func (entry OutputInventoryEntry) Reason() reconcile.ActionReason  { return entry.reason }
func (entry OutputInventoryEntry) Detail() string                  { return entry.detail }

func buildOutputInventory(
	file durable.Snapshot,
	managedPaths []reconcile.ManagedPathDecision,
	aggregates []reconcile.AggregateDecision,
	selection targetselection.Selection,
) (OutputInventory, error) {
	managed := managedOutputInventoryEntries(file, selection)
	unmanaged := append(
		unmanagedPathInventoryEntries(managedPaths, selection),
		aggregateInventoryEntries(aggregates, selection, func(decision reconcile.AggregateSubjectDecision) bool {
			return decision.IsBlocked() && decision.Reason() == reconcile.ReasonUnmanagedOutputExists
		})...,
	)
	blocked := append(
		blockedPathInventoryEntries(managedPaths, selection),
		aggregateInventoryEntries(aggregates, selection, func(decision reconcile.AggregateSubjectDecision) bool {
			return decision.IsBlocked() && decision.Reason().IsOwnershipBlock()
		})...,
	)

	var err error
	if managed, err = canonicalOutputInventoryEntries("managed", managed); err != nil {
		return OutputInventory{}, err
	}
	if unmanaged, err = canonicalOutputInventoryEntries("unmanaged", unmanaged); err != nil {
		return OutputInventory{}, err
	}
	if blocked, err = canonicalOutputInventoryEntries("blocked", blocked); err != nil {
		return OutputInventory{}, err
	}
	return OutputInventory{managed: managed, unmanaged: unmanaged, blocked: blocked}, nil
}

func managedOutputInventoryEntries(
	file durable.Snapshot,
	selection targetselection.Selection,
) []OutputInventoryEntry {
	paths := file.ManagedPaths()
	aggregates := file.ManagedAggregates()
	entries := make([]OutputInventoryEntry, 0, len(paths)+len(aggregates))
	for _, state := range paths {
		consumers := state.ConsumerTargets()
		if !outputInventoryTargetsSelected(consumers, selection) {
			continue
		}
		entries = append(entries, OutputInventoryEntry{
			entityID: outputInventoryEntityForSubject(state.Subject()),
			subject:  state.Subject(),
			targets:  consumers,
			scope:    state.Scope(),
			path:     state.Destination().String(),
			hash:     string(state.ContentHash()),
		})
	}
	for _, state := range aggregates {
		contribution := state.Contribution()
		if !selection.Includes(contribution.Target()) {
			continue
		}
		entries = append(entries, OutputInventoryEntry{
			entityID: outputInventoryEntityForSubject(state.Subject()),
			subject:  state.Subject(),
			targets:  []target.Target{contribution.Target()},
			scope:    contribution.Scope(),
			path:     contribution.AggregateRoot().String(),
		})
	}
	return entries
}

func unmanagedPathInventoryEntries(
	decisions []reconcile.ManagedPathDecision,
	selection targetselection.Selection,
) []OutputInventoryEntry {
	entries := make([]OutputInventoryEntry, 0)
	for _, decision := range decisions {
		if !decision.IsBlocked() || decision.Reason() != reconcile.ReasonUnmanagedOutputExists ||
			!outputInventoryTargetsSelected(decision.ConsumerTargets(), selection) {
			continue
		}
		entries = append(entries, pathInventoryEntry(decision))
	}
	return entries
}

func blockedPathInventoryEntries(
	decisions []reconcile.ManagedPathDecision,
	selection targetselection.Selection,
) []OutputInventoryEntry {
	entries := make([]OutputInventoryEntry, 0)
	for _, decision := range decisions {
		if !decision.IsBlocked() || !decision.Reason().IsOwnershipBlock() ||
			!outputInventoryTargetsSelected(decision.ConsumerTargets(), selection) {
			continue
		}
		entries = append(entries, pathInventoryEntry(decision))
	}
	return entries
}

func pathInventoryEntry(decision reconcile.ManagedPathDecision) OutputInventoryEntry {
	return OutputInventoryEntry{
		entityID: outputInventoryEntityForSubject(decision.Subject()),
		subject:  decision.Subject(),
		targets:  decision.ConsumerTargets(),
		scope:    decision.Scope(),
		path:     decision.Destination().String(),
		hash:     string(decision.LiveHash()),
		reason:   decision.Reason(),
		detail:   decision.Detail(),
	}
}

func aggregateInventoryEntries(
	decisions []reconcile.AggregateDecision,
	selection targetselection.Selection,
	include func(reconcile.AggregateSubjectDecision) bool,
) []OutputInventoryEntry {
	entries := make([]OutputInventoryEntry, 0)
	for _, decision := range decisions {
		for _, projection := range decision.Projections() {
			for _, subject := range projection.SubjectDecisions() {
				if !selection.Includes(subject.Target()) || !include(subject) {
					continue
				}
				entries = append(entries, OutputInventoryEntry{
					entityID:    outputInventoryEntityForSubject(subject.Subject()),
					subject:     subject.Subject(),
					targets:     []target.Target{subject.Target()},
					scope:       subject.Scope(),
					path:        subject.Destination().String(),
					contentPath: subject.ContentPath(),
					reason:      subject.Reason(),
					detail:      subject.Detail(),
				})
			}
		}
	}
	return entries
}

func canonicalOutputInventoryEntries(
	category string,
	entries []OutputInventoryEntry,
) ([]OutputInventoryEntry, error) {
	result := cloneOutputInventoryEntries(entries)
	for index := range result {
		if err := result[index].validate(); err != nil {
			return nil, fmt.Errorf("%s output inventory entry[%d]: %w", category, index, err)
		}
	}
	sort.Slice(result, func(left int, right int) bool {
		return compareOutputInventoryEntries(result[left], result[right]) < 0
	})
	canonical := result[:0]
	for _, entry := range result {
		if len(canonical) == 0 || outputInventoryIdentityKey(canonical[len(canonical)-1]) != outputInventoryIdentityKey(entry) {
			canonical = append(canonical, entry)
			continue
		}
		if !outputInventoryEntriesEqual(canonical[len(canonical)-1], entry) {
			return nil, fmt.Errorf(
				"%s output inventory has conflicting duplicate subject %q at %q",
				category,
				entry.subject,
				entry.path,
			)
		}
	}
	return canonical, nil
}

func (entry OutputInventoryEntry) validate() error {
	if err := entry.subject.Validate(); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	if _, err := target.ParseScope(string(entry.scope)); err != nil {
		return fmt.Errorf("scope: %w", err)
	}
	if entry.path == "" {
		return fmt.Errorf("path is required")
	}
	if err := entry.contentPath.Validate(); err != nil {
		return fmt.Errorf("content path: %w", err)
	}
	if len(entry.targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}
	seen := make(map[target.Target]struct{}, len(entry.targets))
	for index, value := range entry.targets {
		parsed, err := target.ParseTarget(string(value))
		if err != nil {
			return fmt.Errorf("target[%d]: %w", index, err)
		}
		if _, duplicate := seen[parsed]; duplicate {
			return fmt.Errorf("duplicate target %q", parsed)
		}
		seen[parsed] = struct{}{}
	}
	return nil
}

func compareOutputInventoryEntries(left OutputInventoryEntry, right OutputInventoryEntry) int {
	leftKey := outputInventoryIdentityKey(left)
	rightKey := outputInventoryIdentityKey(right)
	switch {
	case leftKey < rightKey:
		return -1
	case leftKey > rightKey:
		return 1
	default:
		return 0
	}
}

func outputInventoryIdentityKey(entry OutputInventoryEntry) string {
	return outputInventoryTargetKey(entry.targets) + "\x00" + string(entry.scope) + "\x00" +
		entry.path + "\x00" + string(entry.contentPath) + "\x00" + entry.subject.String() + "\x00" +
		string(entry.entityID.Kind()) + "\x00" + entry.entityID.Name()
}

func outputInventoryEntriesEqual(left OutputInventoryEntry, right OutputInventoryEntry) bool {
	return left.entityID == right.entityID && left.subject == right.subject &&
		slices.Equal(left.targets, right.targets) && left.scope == right.scope &&
		left.path == right.path && left.contentPath == right.contentPath &&
		left.hash == right.hash && left.reason == right.reason &&
		left.detail == right.detail
}

func outputInventoryEntityForSubject(subject topology.SubjectID) entity.ID {
	entityID, ok := topologyprojection.EntityID(subject)
	if !ok {
		return entity.ID{}
	}
	return entityID
}

func outputInventoryTargetsSelected(values []target.Target, selection targetselection.Selection) bool {
	return slices.ContainsFunc(values, selection.Includes)
}

func outputInventoryTargetKey(values []target.Target) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, "\x00")
}

func cloneOutputInventoryEntries(values []OutputInventoryEntry) []OutputInventoryEntry {
	result := make([]OutputInventoryEntry, len(values))
	for index, value := range values {
		result[index] = value
		result[index].targets = append([]target.Target(nil), value.targets...)
	}
	return result
}
