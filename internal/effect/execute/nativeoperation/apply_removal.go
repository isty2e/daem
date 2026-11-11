package nativeoperation

import (
	"context"
	"errors"

	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
)

func prepareNativeApplyRemovals(ctx context.Context, captured journalpublication.CapturedNativeJournal, ordinary nativeOrdinaryApply, work nativeApplyWork, authority *operation, input NativeApplyInput, store ownershipmutation.RegistryStore) (resultErr error) {
	operation, err := journal.OpenCapturedNativeRecovery(ctx, captured, input.StateCodec, input.StatefileCodec, authority.capturedRoot, input.Budget, journalqualification.NativeRecoveryOptions{
		Filesystem: input.Filesystem, OperationAuthority: authority.recoveryJournal, Registry: store, Codecs: input.Codecs,
		State: func(ctx context.Context) (durable.Snapshot, error) {
			return authority.projectStatefile.Read(ctx, input.StatefileCodec, input.Budget)
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
	certificates, err := hostmutation.ApplyRemovalCandidates(work.managed, work.ordinaryAggregates, input.Payloads, ordinary.demands)
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
			certificates = append(certificates, removaloperation.ConstantForwardCandidate(removaloperation.Relation{Scope: intent.Scope(), Destination: intent.Destination()}, state, 0, int64(len(expected.Content()))))
		}
	}
	return authority.removals.PrepareForwardFor(ctx, physical, certificates)
}
