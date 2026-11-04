package execute

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func nativeRecoveryDestination(ctx context.Context, document journal.NativeRecoveryDocument, budget *physicalwork.Budget) (mutationDestination, error) {
	aliases := document.Namespace().Addresses()
	if len(aliases) == 0 {
		return mutationDestination{}, fmt.Errorf("native recovery document has no aliases")
	}
	alias := aliases[0]
	removal, present, err := document.Removal()
	if err != nil {
		return mutationDestination{}, err
	}
	if present {
		intent, err := removal.PhysicalIntent()
		if err != nil {
			return mutationDestination{}, err
		}
		found := false
		for _, candidate := range aliases {
			if candidate.Scope() == intent.Scope() && candidate.AggregateRoot() == intent.Destination() {
				alias = candidate
				found = true
				break
			}
		}
		if !found {
			return mutationDestination{}, fmt.Errorf("native physical removal has no actual alias")
		}
	}
	root, bound, err := document.Namespace().WriterBinding(ctx, budget)
	if err != nil {
		return mutationDestination{}, err
	}
	path, err := bound.LexicalPath()
	if err != nil {
		return mutationDestination{}, err
	}
	return mutationDestination{scope: alias.Scope(), logical: alias.AggregateRoot(), hostPath: path, root: root, destination: bound}, nil
}

func prepareNativeRecoveryRemovals(ctx context.Context, authority *mutationAuthority, operation *journal.NativeRecoveryOperation, ordinary []recoveryHostAction) error {
	physical, err := bindNativeRemovalIntents(ctx, authority, operation)
	if err != nil {
		return err
	}
	var reachable []recovery.RemovalDemand
	for _, demand := range physical.Demands() {
		for _, action := range ordinary {
			if demand.Scope() == action.Scope && demand.Destination().String() == action.Destination {
				reachable = append(reachable, demand)
				break
			}
		}
	}
	for _, document := range operation.Documents() {
		if !operation.InitialAssessment().RequiresRollback() || !document.Restoration().Changed() || document.Restoration().Document().Exists() {
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
		reachable = append(reachable, intent.Demand())
	}
	reachableDemands, err := recovery.NewRemovalDemandSet(reachable)
	if err != nil {
		return err
	}
	certificates, err := recoveryForwardRemovalCertificates(ctx, ordinary, physical, operation.Codecs())
	if err != nil {
		return err
	}
	return authority.prepareForwardRemovalReservationsFor(ctx, reachableDemands, certificates)
}
