package execute

import (
	"context"
	"errors"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
)

func prepareNativeApplyRemovals(ctx context.Context, captured journal.CapturedNativeJournal, ordinary nativeOrdinaryApply, work nativeApplyWork, authority *mutationAuthority, input NativeApplyInput, store ownershipmutation.RegistryStore) (resultErr error) {
	operation, err := captured.OpenRecovery(ctx, input.StateCodec, journal.NativeRecoveryOptions{
		Filesystem: input.Filesystem, OperationAuthority: authority.recoveryJournal, Registry: store, Codecs: input.Codecs,
		State: func(ctx context.Context) (durable.Snapshot, error) {
			return readNativeCommittedState(ctx, authority, input)
		},
	})
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, operation.Close()) }()
	physical, err := bindNativeRemovalIntents(ctx, authority, operation)
	if err != nil {
		return err
	}
	certificates, err := applyForwardRemovalCertificates(work.managed, work.ordinaryAggregates, input.Payloads, ordinary.demands)
	if err != nil {
		return err
	}
	for _, document := range captured.Documents() {
		before, _ := document.Original()
		expected, _ := document.Expected()
		if before.Exists() || !expected.Exists() {
			continue
		}
		removal, present, err := document.Removal()
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		intent, err := removal.PhysicalIntent()
		if err != nil {
			return err
		}
		for _, state := range intent.Demand().States() {
			certificates = append(certificates, constantForwardRemovalCertificate(removalRelationKey{scope: intent.Scope(), destination: intent.Destination()}, state, 0, int64(len(expected.Content()))))
		}
	}
	return authority.prepareForwardRemovalReservationsFor(ctx, physical, certificates)
}
