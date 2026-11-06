package execute

import (
	"context"
	"fmt"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func promoteNativeCapturedDocument(ctx context.Context, captured journalpublication.CapturedNativeJournal, index int, authority *mutationAuthority, input NativeApplyInput, store ownershipmutation.RegistryStore) (journalpublication.CapturedNativeJournal, error) {
	return captured.PromoteDocument(ctx, index, journalpublication.NativePromotionOptions{
		NativeRecoveryOptions: journalqualification.NativeRecoveryOptions{
			Filesystem: input.Filesystem, OperationAuthority: authority.recoveryJournal, Codecs: input.Codecs, Registry: store,
			State: func(ctx context.Context) (durable.Snapshot, error) {
				return authority.projectStatefile.Read(ctx, input.StateCodec, input.Budget)
			},
		},
		RecordAuthority: authority.recoveryJournalRecord, StateCodec: input.StateCodec,
	})
}

func reserveNativePromotedDocument(ctx context.Context, store ownershipmutation.RegistryStore, document journalpublication.CapturedNativeDocument) error {
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
