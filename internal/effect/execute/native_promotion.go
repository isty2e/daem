package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func promoteNativeCapturedDocument(ctx context.Context, captured journal.CapturedNativeJournal, index int, authority *mutationAuthority, input NativeApplyInput, store ownershipmutation.RegistryStore) (journal.CapturedNativeJournal, error) {
	return captured.PromoteDocument(ctx, index, journal.NativePromotionOptions{
		NativeRecoveryOptions: journal.NativeRecoveryOptions{
			Filesystem: input.Filesystem, OperationAuthority: authority.recoveryJournal, Codecs: input.Codecs, Registry: store,
			State: func(ctx context.Context) (durable.Snapshot, error) {
				return readNativeCommittedState(ctx, authority, input)
			},
		},
		RecordAuthority: authority.recoveryJournalRecord, StateCodec: input.StateCodec,
	})
}

func reserveNativePromotedDocument(ctx context.Context, store ownershipmutation.RegistryStore, document journal.CapturedNativeDocument) error {
	if len(document.ProvisionalAcquires()) != 0 {
		return fmt.Errorf("native claim promotion is incomplete")
	}
	claims, exact := document.Claims()
	if !exact {
		return nil
	}
	change, err := claims.Preparation()
	if err != nil {
		return err
	}
	convergence, err := ownership.NewClaimConvergence(nil)
	if err != nil {
		return err
	}
	convergence, err = convergence.WithDocumentChanges([]ownership.DocumentClaimChange{change})
	if err != nil {
		return err
	}
	bindings, err := ownership.NewDocumentBindingConvergence(nil)
	if err != nil {
		return err
	}
	_, err = store.ConvergeRegistry(ctx, convergence, bindings)
	return err
}
