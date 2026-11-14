package publication

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
)

// PromoteOrdinary publishes one exact ordinary acquire in the complete native16
// record. It retains all document groups, images and other pending intents.
func (captured CapturedNativeJournal) PromoteOrdinary(ctx context.Context, intent ownership.ProvisionalAcquireIntent, transition ownershipmutation.ClaimTransition, options NativePromotionOptions) (CapturedNativeJournal, error) {
	if err := captured.Validate(); err != nil {
		return CapturedNativeJournal{}, err
	}
	observed, err := captured.AssessForward(ctx, options.NativeRecoveryOptions)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	state, _ := observed.Facts().Persistence()
	if !state.Equal(captured.journal.Before()) || captured.publication.resolver == nil {
		return CapturedNativeJournal{}, fmt.Errorf("ordinary promotion requires observed Before and independent path selection")
	}
	entry, err := captured.journal.ProvisionalAcquireEntry(intent)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	matched, err := observed.Facts().OrdinaryMatchesExpected(entry)
	if err != nil || !matched {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("ordinary promotion requires a published expected path"), err)
	}
	path, err := captured.publication.resolver(intent.Destination())
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	actual, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, captured.budget)
	if err != nil || !actual.Exact().Equal(transition.Address().PathAuthority()) {
		return CapturedNativeJournal{}, errors.Join(fmt.Errorf("ordinary promotion path authority changed"), err)
	}
	captured.publication.references = append(slices.Clone(captured.publication.references), actual.Reference())
	candidate, err := captured.journal.PromoteProvisionalAcquire(intent, transition)
	if err != nil {
		return CapturedNativeJournal{}, err
	}
	return captured.replaceRecord(ctx, candidate, options)
}
