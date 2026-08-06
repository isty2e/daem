package journal

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func capturedRemovalStates(
	candidates []capturedRemovalCandidate,
) (map[removalRelationKey][]recovery.RemovalState, error) {
	result := make(map[removalRelationKey][]recovery.RemovalState, len(candidates))
	for index, candidate := range candidates {
		before := candidate.before
		expected := candidate.expected
		if candidate.action.ContentPath != "" {
			before = aggregateWholeDocumentBeforeState(candidate.action, before)
			expected = aggregateWholeDocumentExpectedState(candidate.action, expected)
		}
		beforeState, err := recovery.NewBeforeRemovalState(before)
		if err != nil {
			return nil, fmt.Errorf("captured removal candidate[%d] before state: %w", index, err)
		}
		expectedState, err := recovery.NewExpectedRemovalState(expected)
		if err != nil {
			return nil, fmt.Errorf("captured removal candidate[%d] expected state: %w", index, err)
		}
		key := removalRelationKey{
			scope:       candidate.action.Scope,
			destination: candidate.action.Destination,
		}
		result[key] = appendUniqueRemovalStates(result[key], beforeState, expectedState)
	}
	return result, nil
}

func appendUniqueRemovalStates(
	states []recovery.RemovalState,
	additions ...recovery.RemovalState,
) []recovery.RemovalState {
	for _, addition := range additions {
		present := false
		for _, state := range states {
			if state.Equal(addition) {
				present = true
				break
			}
		}
		if !present {
			states = append(states, addition)
		}
	}
	return states
}

func validateRecoveryRemovalDemandStates(
	demands recovery.RemovalDemandSet,
	captured map[removalRelationKey][]recovery.RemovalState,
) error {
	for _, demand := range demands.Demands() {
		key := removalRelationKey{
			scope:       demand.Scope(),
			destination: demand.Destination(),
		}
		candidates := captured[key]
		for _, required := range demand.States() {
			matched := false
			for _, candidate := range candidates {
				if required.Equal(candidate) {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf(
					"removal demand %q state is not represented by captured transition evidence",
					demand.Destination(),
				)
			}
		}
	}
	return nil
}

// validateRecoveryRemovalCoverage proves that the durable intent set is the
// exact persisted form of the one canonical demand set constructed from the
// executable transition schedule. It deliberately does not inspect journal
// entry resource or content kinds.
func validateRecoveryRemovalCoverage(
	demands recovery.RemovalDemandSet,
	intents []recovery.RemovalIntent,
) error {
	actual := make([]recovery.RemovalDemand, 0, len(intents))
	for index, intent := range intents {
		demand, err := intent.Demand()
		if err != nil {
			return fmt.Errorf("removal intent[%d] demand: %w", index, err)
		}
		actual = append(actual, demand)
	}
	actualSet, err := recovery.NewRemovalDemandSet(actual)
	if err != nil {
		return fmt.Errorf("removal intent demand set: %w", err)
	}
	if !demands.Equal(actualSet) {
		return fmt.Errorf("removal intents do not exactly cover the executable removal-demand set")
	}
	return nil
}

// validateRecoveryRemovalIntents validates the complete persisted intent set
// without reconstructing reachability from journal entry kinds. The demand is
// already embedded in each immutable intent; this check protects relation and
// residue cardinality on reload.
func validateRecoveryRemovalIntents(intents []recovery.RemovalIntent) error {
	relations := make(map[removalRelationKey]struct{}, len(intents))
	residueNames := make(map[string]struct{}, len(intents))
	previousRelation := ""
	for index, intent := range intents {
		if err := intent.Validate(); err != nil {
			return fmt.Errorf("intent[%d]: %w", index, err)
		}
		key := removalRelationKey{scope: intent.Scope(), destination: intent.Destination()}
		relationOrder := string(intent.Scope()) + "\x00" + intent.Destination().String()
		if index > 0 && relationOrder <= previousRelation {
			return fmt.Errorf("intent[%d] rooted relation order is not canonical", index)
		}
		previousRelation = relationOrder
		if _, duplicate := relations[key]; duplicate {
			return fmt.Errorf("intent[%d] duplicates rooted relation %q", index, intent.Destination())
		}
		relations[key] = struct{}{}
		name := intent.Namespace().ResidueName().String()
		if _, duplicate := residueNames[name]; duplicate {
			return fmt.Errorf("intent[%d] duplicates residue name %q", index, name)
		}
		residueNames[name] = struct{}{}
	}
	return nil
}

// validatePersistedRemovalIntentRelations rejects orphaned durable intents on
// reload without reinterpreting entry resource kinds. Reachability itself was
// proved by the canonical demand set before publication.
func validatePersistedRemovalIntentRelations(
	entries []recoveryEntry,
	intents []recovery.RemovalIntent,
) error {
	relations := make(map[removalRelationKey]struct{}, len(entries))
	for index, entry := range entries {
		scope, err := target.ParseScope(entry.Scope)
		if err != nil {
			return fmt.Errorf("entry[%d] scope: %w", index, err)
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			return fmt.Errorf("entry[%d] destination: %w", index, err)
		}
		relations[removalRelationKey{scope: scope, destination: destination}] = struct{}{}
	}
	seen := make(map[removalRelationKey]struct{}, len(intents))
	for index, intent := range intents {
		key := removalRelationKey{scope: intent.Scope(), destination: intent.Destination()}
		if _, present := relations[key]; !present {
			return fmt.Errorf("intent[%d] %q has no journal entry relation", index, intent.Destination())
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("intent[%d] %q duplicates a journal entry relation", index, intent.Destination())
		}
		seen[key] = struct{}{}
	}
	return nil
}
