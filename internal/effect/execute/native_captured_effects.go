package execute

import (
	"context"
	"errors"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func prepareNativeCapturedEffects(ctx context.Context, captured journal.CapturedNativeJournal, input NativeApplyInput) (retiring *journal.NativeJournalRetirement, preparation ownership.ClaimConvergence, bindings ownership.DocumentBindingConvergence, resultErr error) {
	var err error
	if !captured.NeedsPromotion() {
		retiring, err = journal.PrepareNativeJournalRetirement(ctx, captured, input.Filesystem, input.StateCodec)
		if err != nil {
			return nil, preparation, bindings, err
		}
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, retiring.Close())
			retiring = nil
		}
	}()
	bindings, err = ownership.NewDocumentBindingConvergence(nil)
	if err != nil {
		return retiring, preparation, bindings, err
	}
	preparation, err = captured.PreparationClaims(ctx)
	return retiring, preparation, bindings, err
}

func observeNativeRuntime(ctx context.Context, captured journal.CapturedNativeJournal, authority *mutationAuthority, input NativeApplyInput, store ownershipmutation.RegistryReader) (journalqualification.NativeRecoveryAssessment, error) {
	return captured.AssessRecovery(ctx, journal.NativeRecoveryOptions{
		Filesystem: input.Filesystem, OperationAuthority: authority.recoveryJournal,
		State: func(ctx context.Context) (durable.Snapshot, error) {
			return readNativeCommittedState(ctx, authority, input)
		},
		Registry: store, Codecs: input.Codecs,
	})
}
