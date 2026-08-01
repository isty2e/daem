package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

// ProvisionalAcquirePromotion carries the exact journal facts established by a
// successful intent promotion. ActiveJournalAuthority remains available on an
// error when storage proved the journal directory stayed the same object.
type ProvisionalAcquirePromotion struct {
	recordFingerprint string
	activeAuthority   ActiveJournalAuthority
}

// RecordFingerprint returns the CAS baseline for a later promotion.
func (promotion ProvisionalAcquirePromotion) RecordFingerprint() string {
	return promotion.recordFingerprint
}

// ActiveJournalAuthority returns refreshed physical evidence after the record
// replacement attempt. The boolean is false when storage could not prove
// directory-object continuity.
func (promotion ProvisionalAcquirePromotion) ActiveJournalAuthority() (ActiveJournalAuthority, bool) {
	return promotion.activeAuthority, promotion.activeAuthority.valid()
}

// PromoteProvisionalAcquire atomically replaces one provisional ownership
// intent with its exact acquire transition and refreshes the active journal
// directory authority through the same open parent used for replacement.
func PromoteProvisionalAcquire(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	recordAuthority *rootedpath.EntryAuthority,
	directoryAuthority *rootedpath.EntryAuthority,
	activeAuthority ActiveJournalAuthority,
	expectedFingerprint string,
	intent outputownership.ProvisionalAcquireIntent,
	transition ownershipmutation.ClaimTransition,
	stateCodec durable.SnapshotCodec,
) (ProvisionalAcquirePromotion, error) {
	if ctx == nil {
		return ProvisionalAcquirePromotion{}, fmt.Errorf("recovery journal promotion context is required")
	}
	if filesystem == nil {
		return ProvisionalAcquirePromotion{}, fmt.Errorf("recovery journal promotion filesystem is required")
	}
	if recordAuthority == nil {
		return ProvisionalAcquirePromotion{}, fmt.Errorf("recovery journal record authority is required")
	}
	if directoryAuthority == nil {
		return ProvisionalAcquirePromotion{}, fmt.Errorf("active recovery journal authority is required")
	}
	if err := activeAuthority.Validate(); err != nil {
		return ProvisionalAcquirePromotion{}, err
	}
	if expectedFingerprint == "" {
		return ProvisionalAcquirePromotion{}, fmt.Errorf("recovery journal promotion fingerprint is required")
	}
	if stateCodec == nil {
		return ProvisionalAcquirePromotion{}, errRecoveryJournalStateCodecRequired
	}
	if err := intent.Validate(); err != nil {
		return ProvisionalAcquirePromotion{}, fmt.Errorf("recovery journal promotion intent: %w", err)
	}
	if err := transition.Validate(); err != nil {
		return ProvisionalAcquirePromotion{}, fmt.Errorf("recovery journal promotion transition: %w", err)
	}
	if transition.Kind() != ownershipmutation.TransitionAcquire {
		return ProvisionalAcquirePromotion{}, fmt.Errorf("recovery journal promotion requires an acquire transition")
	}
	derived, err := ownershipmutation.NewAcquireTransitionFromIntent(intent, transition.Address())
	if err != nil {
		return ProvisionalAcquirePromotion{}, fmt.Errorf("derive recovery journal promotion: %w", err)
	}
	if !derived.Equal(transition) {
		return ProvisionalAcquirePromotion{}, fmt.Errorf("recovery journal promotion transition differs from intent")
	}

	capability, snapshot, current, currentFingerprint, err := readPromotionRecord(
		ctx,
		filesystem,
		recordAuthority,
		stateCodec,
	)
	if err != nil {
		return ProvisionalAcquirePromotion{}, err
	}
	if currentFingerprint != expectedFingerprint {
		return ProvisionalAcquirePromotion{}, errors.Join(
			fmt.Errorf("recovery journal changed before ownership intent promotion"),
			capability.Close(),
		)
	}

	candidate, err := recoveryJournalAfterPromotion(current, intent, transition, stateCodec)
	if err != nil {
		return ProvisionalAcquirePromotion{}, errors.Join(err, capability.Close())
	}
	content, err := marshalRecoveryJournalWithStateContentForPromotion(candidate, stateCodec)
	if err != nil {
		return ProvisionalAcquirePromotion{}, errors.Join(err, capability.Close())
	}
	candidateFingerprint := recoveryJournalRecordFingerprint(content)
	outcome, refreshedParent, replaceErr := filesystem.ReplaceRootedFileAndRefreshParent(
		ctx,
		capability,
		content,
		recoveryJournalMode,
		snapshot.Identity(),
		activeAuthority.identity,
	)
	result := ProvisionalAcquirePromotion{}
	if refreshedParent != nil {
		refreshedAuthority, refreshErr := newActiveJournalAuthority(refreshedParent)
		if refreshErr != nil {
			replaceErr = errors.Join(replaceErr, refreshErr)
		} else {
			result.activeAuthority = refreshedAuthority
		}
	}
	if replaceErr != nil {
		return result, fmt.Errorf(
			"promote recovery journal ownership intent (%s): %w",
			commitOutcomeDetail(outcome),
			replaceErr,
		)
	}
	if !result.activeAuthority.valid() {
		return result, fmt.Errorf("promote recovery journal ownership intent: refreshed directory authority is unavailable")
	}
	if err := ValidateActiveJournalAuthority(
		ctx,
		filesystem,
		directoryAuthority,
		result.activeAuthority,
	); err != nil {
		return result, fmt.Errorf("verify promoted recovery journal directory: %w", err)
	}

	verificationCapability, verifiedSnapshot, verified, verifiedFingerprint, err := readPromotionRecord(
		ctx,
		filesystem,
		recordAuthority,
		stateCodec,
	)
	if err != nil {
		return result, fmt.Errorf("verify promoted recovery journal: %w", err)
	}
	closeErr := verificationCapability.Close()
	if verifiedFingerprint != candidateFingerprint ||
		!bytes.Equal(verifiedSnapshot.Content(), content) ||
		!samePromotedJournal(candidate, verified, stateCodec) {
		return result, errors.Join(
			fmt.Errorf("promoted recovery journal postcondition differs from committed candidate"),
			closeErr,
		)
	}
	if closeErr != nil {
		return result, closeErr
	}
	if err := ValidateActiveJournalAuthority(
		ctx,
		filesystem,
		directoryAuthority,
		result.activeAuthority,
	); err != nil {
		return result, fmt.Errorf("reverify promoted recovery journal directory: %w", err)
	}
	result.recordFingerprint = candidateFingerprint
	return result, nil
}

func readPromotionRecord(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	stateCodec durable.SnapshotCodec,
) (
	rootedpath.CommitCapability,
	mutationfs.RegularFileSnapshot,
	recoveryJournal,
	string,
	error,
) {
	capability, err := authority.Acquire()
	if err != nil {
		return nil, mutationfs.RegularFileSnapshot{}, recoveryJournal{}, "", err
	}
	content, mode, identity, err := filesystem.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		maximumRecoveryJournalBytes,
	)
	if err != nil {
		return nil, mutationfs.RegularFileSnapshot{}, recoveryJournal{}, "", errors.Join(err, capability.Close())
	}
	snapshot, err := mutationfs.NewRegularFileSnapshot(content, mode, identity)
	if err != nil {
		return nil, mutationfs.RegularFileSnapshot{}, recoveryJournal{}, "", errors.Join(err, capability.Close())
	}
	current, err := decodeRecoveryJournalSnapshot(snapshot, recoveryJournalFileName, stateCodec)
	if err != nil {
		return nil, mutationfs.RegularFileSnapshot{}, recoveryJournal{}, "", errors.Join(err, capability.Close())
	}
	return capability, snapshot, current, recoveryJournalRecordFingerprint(content), nil
}

func recoveryJournalAfterPromotion(
	current recoveryJournal,
	intent outputownership.ProvisionalAcquireIntent,
	transition ownershipmutation.ClaimTransition,
	stateCodec durable.SnapshotCodec,
) (recoveryJournal, error) {
	if current.OperationID != intent.OperationID() {
		return recoveryJournal{}, fmt.Errorf("recovery journal operation differs from ownership intent")
	}
	intents, err := canonicalProvisionalAcquireIntents(current.ProvisionalAcquires)
	if err != nil {
		return recoveryJournal{}, err
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
		return recoveryJournal{}, fmt.Errorf(
			"recovery journal contains %d matching provisional ownership intents, want 1",
			matched,
		)
	}
	entryIndex, err := promotedIntentEntryIndex(current.Entries, intent)
	if err != nil {
		return recoveryJournal{}, err
	}

	transitions, err := canonicalClaimTransitions(current.ClaimTransitions)
	if err != nil {
		return recoveryJournal{}, err
	}
	transitions = append(transitions, transition)
	persistedTransitions, err := recoveryClaimTransitions(transitions)
	if err != nil {
		return recoveryJournal{}, err
	}
	persistedIntents, err := recoveryProvisionalAcquireIntents(remaining)
	if err != nil {
		return recoveryJournal{}, err
	}
	candidate := current
	candidate.Entries = append([]recoveryEntry(nil), current.Entries...)
	candidate.Entries[entryIndex].OwnershipPathAuthority = persistedPathAuthority(
		transition.Address().PathAuthority(),
	)
	candidate.ClaimTransitions = persistedTransitions
	candidate.ProvisionalAcquires = persistedIntents
	if err := validateRecoveryJournal(candidate, stateCodec); err != nil {
		return recoveryJournal{}, fmt.Errorf("validate promoted recovery journal: %w", err)
	}
	return candidate, nil
}

func promotedIntentEntryIndex(
	entries []recoveryEntry,
	intent outputownership.ProvisionalAcquireIntent,
) (int, error) {
	matchedIndex := -1
	for index, entry := range entries {
		if entry.Path != intent.Destination().String() ||
			entry.ContentPath != string(intent.ContentPath()) {
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
