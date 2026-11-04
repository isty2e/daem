package journal

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func selectedRecoveryEntryIndexes(entries []recoveryEntry, selected []EntrySelection) ([]int, error) {
	keys := make(map[entrySelectionKey]struct{}, len(selected))
	for _, selection := range selected {
		if !selection.initialized {
			return nil, fmt.Errorf("recovery entry selection is uninitialized")
		}
		key := selection.key
		if _, exists := keys[key]; exists {
			return nil, fmt.Errorf("duplicate selected recovery entry for %q", key.destination)
		}
		keys[key] = struct{}{}
	}

	result := make([]int, 0, len(selected))
	for index, entry := range entries {
		key, err := entrySelectionKeyFromRecoveryEntry(entry)
		if err != nil {
			return nil, err
		}
		if _, exists := keys[key]; !exists {
			continue
		}
		result = append(result, index)
		delete(keys, key)
	}
	if len(keys) != 0 {
		return nil, fmt.Errorf("selected recovery entries do not match the active journal")
	}
	return result, nil
}

func entrySelectionKeyFromRecoveryEntry(entry recoveryEntry) (entrySelectionKey, error) {
	agentTarget, _ := target.ParseTarget(entry.Target)
	scope, _ := target.ParseScope(entry.Scope)
	subject, err := entry.Subject.canonical()
	if err != nil {
		return entrySelectionKey{}, fmt.Errorf("recovery entry subject: %w", err)
	}
	destination, err := output.Parse(entry.Path)
	if err != nil {
		return entrySelectionKey{}, fmt.Errorf("recovery entry destination: %w", err)
	}
	return entrySelectionKey{
		subject:     subject,
		target:      agentTarget,
		consumers:   strings.Join(entry.Targets, "\x00"),
		scope:       scope,
		destination: destination,
		contentPath: output.ContentPath(entry.ContentPath),
	}, nil
}

// EntrySelections derives rollback-selection identities in host mutation
// order: managed paths first, followed by managed aggregate projections.
func EntrySelections(
	managed []ManagedPathMutation,
	aggregates []ManagedAggregateMutation,
) ([]EntrySelection, error) {
	result := make([]EntrySelection, 0, len(managed)+len(aggregates))
	seen := make(map[entrySelectionKey]struct{}, cap(result))
	appendSelection := func(key entrySelectionKey) error {
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate recovery entry selection for %q", key.destination)
		}
		seen[key] = struct{}{}
		result = append(result, EntrySelection{key: key, initialized: true})
		return nil
	}

	for index, mutation := range managed {
		if err := mutation.validate(); err != nil {
			return nil, fmt.Errorf("managed path journal mutation[%d]: %w", index, err)
		}
		facts := mutation.facts()
		if err := appendSelection(entrySelectionKey{
			subject:     facts.subject,
			consumers:   consumerTargetKey(facts.consumerTargets),
			scope:       facts.scope,
			destination: facts.destination,
		}); err != nil {
			return nil, err
		}
	}
	for index, mutation := range aggregates {
		if err := mutation.validate(); err != nil {
			return nil, fmt.Errorf("managed aggregate journal mutation[%d]: %w", index, err)
		}
		path, err := pathMutationFromAggregate(mutation)
		if err != nil {
			return nil, err
		}
		if err := appendSelection(entrySelectionKeyFromMutation(path)); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func entrySelectionKeyFromMutation(mutation pathMutation) entrySelectionKey {
	return entrySelectionKey{
		subject: mutation.Subject, target: mutation.Target,
		consumers: consumerTargetKey(mutation.ConsumerTargets), scope: mutation.Scope,
		destination: mutation.Destination, contentPath: mutation.ContentPath,
	}
}

func consumerTargetKey(values []target.Target) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, "\x00")
}

// EntrySelection identifies one validated capture mutation without exposing
// expected physical state or persisted journal syntax.
type EntrySelection struct {
	key         entrySelectionKey
	initialized bool
}

type entrySelectionKey struct {
	subject     topology.SubjectID
	target      target.Target
	consumers   string
	scope       target.Scope
	destination output.Destination
	contentPath output.ContentPath
}
