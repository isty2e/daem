package journal

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func removalDemandSetFromEntries(
	entries []recoveryEntry,
) (recovery.RemovalDemandSet, error) {
	statesByRelation := make(map[removalRelationKey][]recovery.RemovalState)
	for index, entry := range entries {
		if entry.RemovalTransition == nil {
			continue
		}
		scope, err := target.ParseScope(entry.Scope)
		if err != nil {
			return recovery.RemovalDemandSet{}, fmt.Errorf(
				"entry[%d] removal transition scope: %w",
				index,
				err,
			)
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			return recovery.RemovalDemandSet{}, fmt.Errorf(
				"entry[%d] removal transition destination: %w",
				index,
				err,
			)
		}
		states, err := canonicalRemovalTransition(*entry.RemovalTransition)
		if err != nil {
			return recovery.RemovalDemandSet{}, fmt.Errorf(
				"entry[%d] removal transition: %w",
				index,
				err,
			)
		}
		key := removalRelationKey{scope: scope, destination: destination}
		statesByRelation[key] = appendUniqueRemovalStates(statesByRelation[key], states...)
	}

	demands := make([]recovery.RemovalDemand, 0, len(statesByRelation))
	for relation, states := range statesByRelation {
		if len(states) == 0 {
			continue
		}
		demand, err := recovery.NewRemovalDemand(
			relation.scope,
			relation.destination,
			states,
		)
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		demands = append(demands, demand)
	}
	return recovery.NewRemovalDemandSet(demands)
}

func canonicalRemovalTransition(
	transition recoveryRemovalTransition,
) ([]recovery.RemovalState, error) {
	states := make([]recovery.RemovalState, 0, 2)
	if transition.Before != nil {
		before := transition.Before.canonical()
		if !before.Existed {
			return nil, fmt.Errorf("before removal state must exist")
		}
		if err := validateRemovalStateContentHash(before.ContentHash, "before.content_hash"); err != nil {
			return nil, err
		}
		state, err := recovery.NewBeforeRemovalState(before)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	if transition.ExpectedAfter != nil {
		expected := transition.ExpectedAfter.canonical()
		if !expected.Existed {
			return nil, fmt.Errorf("expected-after removal state must exist")
		}
		if err := validateRemovalStateContentHash(expected.ContentHash, "expected_after.content_hash"); err != nil {
			return nil, err
		}
		state, err := recovery.NewExpectedRemovalState(expected)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	if len(states) == 0 {
		return nil, fmt.Errorf("removal transition requires before or expected-after state")
	}
	return states, nil
}

func validateRemovalStateContentHash(value string, context string) error {
	if value == "" {
		return nil
	}
	return validateRecoveryContentHash(value, context)
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
		if err := intent.Validate(); err != nil {
			return fmt.Errorf("removal intent[%d]: %w", index, err)
		}
		actual = append(actual, intent.Demand())
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
	if len(intents) > recovery.MaximumRemovalIntents {
		return fmt.Errorf(
			"removal intent count %d exceeds operation maximum %d",
			len(intents),
			recovery.MaximumRemovalIntents,
		)
	}
	relations := make(map[removalRelationKey]struct{}, len(intents))
	namespaceNames := make(map[string]struct{}, len(intents)*2)
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
		for _, name := range []string{
			intent.Namespace().Names().Residue(),
			intent.Namespace().Names().Cleanup(),
		} {
			if _, duplicate := namespaceNames[name]; duplicate {
				return fmt.Errorf("intent[%d] duplicates removal namespace name %q", index, name)
			}
			namespaceNames[name] = struct{}{}
		}
	}
	return nil
}

// validatePersistedRemovalIntentCoverage independently reconstructs the
// executable demand set from removal transition facts and requires the durable
// intent set to be its exact namespace-authority projection.
func validatePersistedRemovalIntentCoverage(
	entries []recoveryEntry,
	intents []recovery.RemovalIntent,
) error {
	demands, err := removalDemandSetFromEntries(entries)
	if err != nil {
		return err
	}
	return validateRecoveryRemovalCoverage(demands, intents)
}
