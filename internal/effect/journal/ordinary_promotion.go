package journal

import (
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

type ordinaryOwnershipFacts struct {
	OperationID         string
	Entries             []recoveryEntry
	ClaimTransitions    []recoveryClaimTransition
	ProvisionalAcquires []recoveryProvisionalAcquireIntent
}

func (material ordinaryOwnershipFacts) promoteOwnership(intent outputownership.ProvisionalAcquireIntent, transition ownershipmutation.ClaimTransition) (ordinaryOwnershipFacts, error) {
	if err := errors.Join(intent.Validate(), transition.Validate()); err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	derived, err := ownershipmutation.NewAcquireTransitionFromIntent(intent, transition.Address())
	if err != nil || !derived.Equal(transition) {
		return ordinaryOwnershipFacts{}, errors.Join(fmt.Errorf("ownership promotion differs from its intent"), err)
	}
	if material.OperationID != intent.OperationID() {
		return ordinaryOwnershipFacts{}, fmt.Errorf("recovery journal operation differs from ownership intent")
	}
	intents, err := canonicalProvisionalAcquireIntents(material.ProvisionalAcquires)
	if err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	remaining := make([]outputownership.ProvisionalAcquireIntent, 0, len(intents))
	matched := 0
	for _, candidate := range intents {
		if candidate.Equal(intent) {
			matched++
			continue
		}
		remaining = append(remaining, candidate)
	}
	if matched != 1 {
		return ordinaryOwnershipFacts{}, fmt.Errorf("recovery journal contains %d matching provisional ownership intents, want 1", matched)
	}
	entryIndex, err := promotedIntentEntryIndex(material.Entries, intent)
	if err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	transitions, err := canonicalClaimTransitions(material.ClaimTransitions)
	if err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	transitions = append(transitions, transition)
	persistedTransitions, err := recoveryClaimTransitions(transitions)
	if err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	persistedIntents, err := recoveryProvisionalAcquireIntents(remaining)
	if err != nil {
		return ordinaryOwnershipFacts{}, err
	}
	candidate := material
	candidate.Entries = slices.Clone(material.Entries)
	candidate.Entries[entryIndex].OwnershipPathAuthority = persistedPathAuthority(transition.Address().PathAuthority())
	candidate.ClaimTransitions, candidate.ProvisionalAcquires = persistedTransitions, persistedIntents
	return candidate, nil
}

func recoveryJournalAfterPromotion(
	current recoveryJournal,
	intent outputownership.ProvisionalAcquireIntent,
	transition ownershipmutation.ClaimTransition,
	stateCodec durable.SnapshotCodec,
) (recoveryJournal, error) {
	material := ordinaryOwnershipFacts{
		OperationID: current.OperationID, Entries: current.Entries,
		ClaimTransitions: current.ClaimTransitions, ProvisionalAcquires: current.ProvisionalAcquires,
	}
	promoted, err := material.promoteOwnership(intent, transition)
	if err != nil {
		return recoveryJournal{}, err
	}
	candidate := current
	candidate.Entries, candidate.ClaimTransitions, candidate.ProvisionalAcquires = promoted.Entries, promoted.ClaimTransitions, promoted.ProvisionalAcquires
	if err := validateRecoveryJournal(candidate, stateCodec); err != nil {
		return recoveryJournal{}, fmt.Errorf("validate promoted recovery journal: %w", err)
	}
	return candidate, nil
}

func promotedIntentEntryIndex(
	entries []recoveryEntry,
	intent outputownership.ProvisionalAcquireIntent,
) (int, error) {
	contentPath, err := intent.LegacyContentPath()
	if err != nil {
		return -1, err
	}
	matchedIndex := -1
	for index, entry := range entries {
		if entry.Path != intent.Destination().String() ||
			entry.ContentPath != string(contentPath) {
			continue
		}
		if matchedIndex >= 0 {
			return -1, fmt.Errorf("recovery journal contains multiple entries for promoted ownership intent")
		}
		allowed, err := recoveryEntryAllowsTransition(entry, ownershipmutation.TransitionAcquire)
		if err != nil {
			return -1, err
		}
		if !allowed {
			return -1, fmt.Errorf("recovery journal entry does not admit ownership acquisition")
		}
		if entry.OwnershipPathAuthority != nil {
			return -1, fmt.Errorf("provisional ownership intent entry already carries exact path authority")
		}
		matchedIndex = index
	}
	if matchedIndex < 0 {
		return -1, fmt.Errorf("recovery journal contains no entry for promoted ownership intent")
	}
	return matchedIndex, nil
}

func marshalRecoveryJournalWithStateContentForPromotion(
	journal recoveryJournal,
	stateCodec durable.SnapshotCodec,
) ([]byte, error) {
	before, after, err := encodeRecoveryJournalSnapshots(
		journal.StatefileBefore,
		journal.StatefileAfter,
		stateCodec,
	)
	if err != nil {
		return nil, err
	}
	return marshalRecoveryJournalWithStateContent(journal, before, after)
}

func samePromotedJournal(
	expected recoveryJournal,
	actual recoveryJournal,
	stateCodec durable.SnapshotCodec,
) bool {
	expectedFingerprint, expectedErr := recoveryJournalAuthorityFingerprint(expected, stateCodec)
	actualFingerprint, actualErr := recoveryJournalAuthorityFingerprint(actual, stateCodec)
	return expectedErr == nil && actualErr == nil && expectedFingerprint == actualFingerprint
}
