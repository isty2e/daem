package permissions

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journalrecovery "github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/recoverygate"
)

func Execute(ctx context.Context, prepared Prepared) (result Result, returnErr error) {
	result = prepared.unobservedResult()
	if ctx == nil {
		return result, fmt.Errorf("permission recovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if prepared.disclosure.blocker != nil {
		return result, prepared.disclosure.blocker
	}
	if len(prepared.rows) == 0 || prepared.budget == nil {
		return result, fmt.Errorf("permission recovery is unavailable")
	}
	store, err := mutation.NewStore(prepared.paths.DataDir)
	if err != nil {
		return result, err
	}
	paths, err := prepared.paths.WithDataDir(store.DataDir())
	if err != nil {
		return result, err
	}
	eligibility := make(map[string]error)
	for _, row := range prepared.rows {
		for _, document := range row.Documents() {
			root, _, err := qualifyTarget(ctx, paths, prepared.owner, document, prepared.budget)
			if root != nil {
				err = errors.Join(err, root.Close())
			}
			eligibility[document.PathAuthority().Key()] = err
		}
	}
	domains, err := recoveryDomains(prepared, eligibility)
	if err != nil {
		return result, err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return result, err
	}
	defer func() { returnErr = errors.Join(returnErr, leases.Release()) }()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil || !matches {
		return result, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	if err := recoverygate.ObserveBounded(ctx, paths, prepared.budget); err != nil {
		return result, err
	}
	owner, err := observeOwner(paths, prepared.budget)
	if err != nil || !owner.Equal(prepared.owner) {
		return result, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(paths.OwnershipRegistryPath, journalrecovery.MaximumPhysicalPathDepth, prepared.budget)
	if err != nil {
		return result, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	registryStore, err := ownershipstore.NewRooted(root, destination, journalrecovery.MaximumPhysicalPathDepth, prepared.budget)
	if err != nil {
		return result, err
	}
	prepared.paths = paths
	for index, row := range prepared.rows {
		record := &result.records[index]
		current, err := registryStore.LoadProtectionRegistry(ctx)
		observed, present := current.Protection(row.Invocation())
		if err == nil && (!present || !observed.Equal(row)) {
			err = refusal(RegistryUnqualified, fmt.Errorf("prepared protection is no longer current"))
		}
		if err != nil {
			record.cause = err
			for targetIndex := range record.targets {
				record.targets[targetIndex].state = TargetBlocked
				record.targets[targetIndex].cause = err
			}
			continue
		}
		record.settlement = SettlementPending
		record.cause = requireQuiescence(ctx, row)
		for targetIndex, document := range row.Documents() {
			target := Target{destination: record.targets[targetIndex].destination, state: TargetBlocked, cause: errors.Join(record.cause, eligibility[document.PathAuthority().Key()])}
			if target.cause == nil {
				target = restrictTarget(ctx, prepared, registryStore, row, document)
			}
			record.targets[targetIndex] = target
		}
		if !recordTargetsSecure(*record) {
			continue
		}
		// Settlement requires a new complete proof, not cached success for earlier targets.
		record.cause = requireQuiescence(ctx, row)
		if record.cause != nil {
			continue
		}
		for targetIndex, document := range row.Documents() {
			target := restrictTarget(ctx, prepared, registryStore, row, document)
			target.expanded = target.expanded || record.targets[targetIndex].expanded
			record.targets[targetIndex] = target
		}
		if !recordTargetsSecure(*record) {
			continue
		}
		if matches, err := leases.DomainsMatchCurrent(ctx); err != nil || !matches {
			record.cause = errors.Join(mutation.StaleSnapshotError{}, err)
			continue
		}
		record.settlement, record.cause = settleProtection(ctx, registryStore, prepared.owner, row)
	}
	if !result.Complete() {
		return result, errors.Join(fmt.Errorf("permission recovery remains pending"), result.Failure())
	}
	return result, nil
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
				return SettlementPending, refusal(PublicationFailed, err)
			}
		}
	}
	return SettlementUnknown, refusal(PublicationFailed, errors.Join(err, readErr))
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

func restrictTarget(ctx context.Context, prepared Prepared, registryStore ownershipstore.Store, row ownership.PendingProtection, document ownership.ProtectedDocument) (target Target) {
	target = Target{destination: document.Bindings()[0].Destination(), state: TargetBlocked}
	registry, err := registryStore.LoadProtectionRegistry(ctx)
	if err == nil {
		err = registry.RequireProtectedDocument(row, document)
	}
	if err != nil {
		target.cause = err
		return target
	}
	root, destination, err := qualifyTarget(ctx, prepared.paths, prepared.owner, document, prepared.budget)
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
	capability, err := root.AcquireBounded(destination, journalrecovery.MaximumPhysicalPathDepth, prepared.budget)
	if err != nil {
		target.cause = err
		return target
	}
	_, absenceErr := storagecommit.ConfirmRootedEntryAbsentWithOutcome(ctx, capability)
	if absenceErr == nil {
		target.state = TargetAbsent
		return target
	}
	capability, err = root.AcquireBounded(destination, journalrecovery.MaximumPhysicalPathDepth, prepared.budget)
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
	target.cause = refusal(MetadataUnqualified, err)
	if err == nil {
		target.state = TargetSecure
	}
	return target
}

func recoveryDomains(prepared Prepared, eligibility map[string]error) ([]mutation.Domain, error) {
	var domains []mutation.Domain
	paths := []string{prepared.paths.OwnershipRegistryPath, prepared.paths.RecoveryDir, prepared.paths.StatefilePath}
	for _, row := range prepared.rows {
		for _, document := range row.Documents() {
			if eligibility[document.PathAuthority().Key()] == nil {
				paths = append(paths, document.PathAuthority().Key())
			}
		}
	}
	for _, path := range paths {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: effect})
			if err != nil {
				return nil, err
			}
			domains = append(domains, domain)
		}
	}
	return domains, nil
}
