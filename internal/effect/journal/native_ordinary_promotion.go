package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

// PromoteOrdinary publishes one exact ordinary acquire in the complete native16
// record. It retains all document groups, images and other pending intents.
func (captured CapturedNativeJournal) PromoteOrdinary(ctx context.Context, intent ownership.ProvisionalAcquireIntent, transition ownershipmutation.ClaimTransition, options NativePromotionOptions) (CapturedNativeJournal, error) {
	if err := captured.Validate(); err != nil {
		return CapturedNativeJournal{}, err
	}
	observed, err := captured.AssessRecovery(ctx, options.NativeRecoveryOptions)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	if !observed.assessment.state.Equal(captured.journal.before) || captured.publication.resolver == nil {
		return CapturedNativeJournal{}, fmt.Errorf("ordinary promotion requires observed Before and independent path selection")
	}
	index, err := promotedIntentEntryIndex(captured.journal.record.Entries, intent)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	entry, err := canonicalRecoveryEntry(captured.journal.record.Entries[index])
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	ordinary := observed.assessment.ordinary
	matched, err := recovery.AssessEntries([]recovery.Entry{entry}, canonicalRecoveryPathEvidence(ordinary.paths), canonicalRecoveryBackupEvidence(ordinary.backups))
	if err != nil || !matched.MatchesExpected() {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("ordinary promotion requires a published expected path"), err)
	}
	path, err := captured.publication.resolver(intent.Destination())
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	actual, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(path, recovery.MaximumPhysicalPathDepth, captured.budget)
	if err != nil || !actual.Exact().Equal(transition.Address().PathAuthority()) {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("ordinary promotion path authority changed"), err)
	}
	material := ordinaryCaptureMaterial{
		OperationID: captured.capture.OperationID, Entries: captured.journal.record.Entries,
		ClaimTransitions: captured.journal.record.ClaimTransitions, ProvisionalAcquires: captured.journal.record.ProvisionalAcquires,
	}
	promoted, err := material.promoteOwnership(intent, transition)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	candidate := captured.journal.record
	candidate.Entries, candidate.ClaimTransitions, candidate.ProvisionalAcquires = promoted.Entries, promoted.ClaimTransitions, promoted.ProvisionalAcquires
	return captured.replaceRecord(ctx, candidate, options)
}
