package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func prepareNativeCapturedEffects(ctx context.Context, captured journal.CapturedNativeJournal, input NativeApplyInput) (retiring *journal.NativeJournalRetirement, preparation ownership.ClaimConvergence, bindings ownership.DocumentBindingConvergence, resultErr error) {
	retiring, err := journal.PrepareNativeJournalRetirement(ctx, captured, input.Filesystem, input.StateCodec)
	if err != nil {
		return nil, preparation, bindings, err
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
	preparation, err = ownership.NewClaimConvergence(nil)
	if err != nil {
		return retiring, preparation, bindings, err
	}
	for _, document := range captured.Documents() {
		claims, exact := document.Claims()
		if len(document.ProvisionalAcquires()) != 0 {
			return retiring, preparation, bindings, fmt.Errorf("native claims require exact captured admission")
		}
		if !exact {
			continue
		}
		change, err := claims.Preparation()
		if err != nil {
			return retiring, preparation, bindings, err
		}
		preparation, err = preparation.WithDocumentChanges([]ownership.DocumentClaimChange{change})
		if err != nil {
			return retiring, preparation, bindings, err
		}
	}
	return retiring, preparation, bindings, nil
}

func observeNativeRuntime(ctx context.Context, captured journal.CapturedNativeJournal, authority *mutationAuthority, input NativeApplyInput, store ownershipmutation.RegistryReader) (journal.NativeRecoveryAssessment, error) {
	return captured.AssessRecovery(ctx, journal.NativeRecoveryOptions{
		Filesystem: input.Filesystem, OperationAuthority: authority.recoveryJournal,
		State: func(ctx context.Context) (durable.Snapshot, error) {
			return readNativeCommittedState(ctx, authority, input)
		},
		Registry: store, Codecs: input.Codecs,
	})
}
