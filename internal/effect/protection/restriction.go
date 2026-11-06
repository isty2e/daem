package protection

import (
	"context"
	"errors"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
)

func (containment Containment) restrictTargets(ctx context.Context, store ownershipstore.Store, row ownership.PendingProtection, record *Record, eligibility map[string]error, beforeTarget func(context.Context) error) bool {
	// Settlement needs a fresh complete proof after the restriction pass.
	for pass := range 2 {
		record.cause = RequireQuiescence(ctx, row)
		if record.cause != nil {
			if pass == 0 {
				for index := range record.targets {
					record.targets[index].state = TargetBlocked
					record.targets[index].cause = record.cause
				}
			}
			return false
		}
		for index, document := range row.Documents() {
			target := Target{destination: record.targets[index].destination, state: TargetBlocked, cause: eligibility[document.PathAuthority().Key()]}
			if target.cause == nil && beforeTarget != nil {
				target.cause = beforeTarget(ctx)
			}
			if target.cause == nil {
				target = containment.restrictTarget(ctx, store, row, document)
			}
			if pass != 0 {
				target.expanded = target.expanded || record.targets[index].expanded
			}
			record.targets[index] = target
		}
		if !recordTargetsSecure(*record) {
			return false
		}
	}
	return true
}

func settleProtection(ctx context.Context, store ownershipmutation.ProtectionRegistryStore, owner stateauthority.Authority, row ownership.PendingProtection) (Settlement, error) {
	clear, err := ownership.NewClearProtection(row)
	if err == nil {
		_, err = store.TransitionProtection(ctx, owner, clear)
	}
	if err == nil {
		return SettlementCleared, nil
	}
	current, readErr := store.LoadPendingProtections(ctx)
	if readErr == nil {
		for _, pending := range current {
			if pending.Equal(row) {
				return SettlementPending, Refusal(PublicationFailed, err)
			}
		}
	}
	return SettlementUnknown, Refusal(PublicationFailed, errors.Join(err, readErr))
}

func recordTargetsSecure(record Record) bool {
	if record.cause != nil || len(record.targets) == 0 {
		return false
	}
	for _, target := range record.targets {
		if target.cause != nil || (target.state != TargetSecure && target.state != TargetAbsent) {
			return false
		}
	}
	return true
}

func (containment Containment) restrictTarget(ctx context.Context, registryStore ownershipstore.Store, row ownership.PendingProtection, document ownership.ProtectedDocument) (target Target) {
	target = Target{destination: document.Bindings()[0].Destination(), state: TargetBlocked}
	registry, err := registryStore.LoadProtectionRegistry(ctx)
	if err == nil {
		err = registry.RequireProtectedDocument(row, document)
	}
	if err != nil {
		target.cause = err
		return target
	}
	root, destination, err := containment.access.CaptureDocument(ctx, document, containment.budget)
	if err != nil {
		target.cause = err
		return target
	}
	defer func() {
		target.cause = errors.Join(target.cause, root.Close())
		if target.cause != nil {
			target.state = TargetBlocked
		}
	}()
	capability, err := root.AcquireBounded(destination, physicalwork.MaximumPhysicalPathDepth, containment.budget)
	if err != nil {
		target.cause = err
		return target
	}
	_, absenceErr := storagecommit.ConfirmRootedEntryAbsentWithOutcome(ctx, capability)
	if absenceErr == nil {
		target.state = TargetAbsent
		return target
	}
	capability, err = root.AcquireBounded(destination, physicalwork.MaximumPhysicalPathDepth, containment.budget)
	if err != nil {
		target.cause = errors.Join(absenceErr, err)
		return target
	}
	request, err := storagecommit.NewRootedPermissionRestriction(capability, document.Restriction)
	if err != nil {
		target.cause = errors.Join(err, capability.Close())
		return target
	}
	observation, err := storagecommit.RestrictRootedFilePermissions(ctx, request)
	target.expanded = observation.Expanded()
	target.cause = Refusal(MetadataUnqualified, err)
	if err == nil {
		target.state = TargetSecure
	}
	return target
}
