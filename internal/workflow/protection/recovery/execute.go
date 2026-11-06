package recovery

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/protection"
	"github.com/isty2e/daem/internal/effect/protection/namespace"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/recoverygate"
)

func Execute(ctx context.Context, prepared Prepared) (result protection.Result, returnErr error) {
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
	prepared.paths = paths
	resolution := namespace.NewResolution(paths, prepared.owner)
	eligibility := make(map[string]error)
	for _, row := range prepared.rows {
		for _, document := range row.Documents() {
			root, _, err := resolution.CaptureDocument(ctx, document, prepared.budget)
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
	owner, err := namespace.ObserveOwner(paths, prepared.budget)
	if err != nil || !owner.Equal(prepared.owner) {
		return result, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, prepared.budget)
	if err != nil {
		return result, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	registryStore, err := ownershipstore.NewRooted(root, destination, physicalwork.MaximumPhysicalPathDepth, prepared.budget)
	if err != nil {
		return result, err
	}
	containment := protection.NewContainment(resolution, prepared.budget)
	records := result.Records()
	for index, row := range prepared.rows {
		current, err := registryStore.LoadProtectionRegistry(ctx)
		observed, present := current.Protection(row.Invocation())
		if err == nil && (!present || !observed.Equal(row)) {
			err = protection.Refusal(protection.RegistryUnqualified, fmt.Errorf("prepared protection is no longer current"))
		}
		if err != nil {
			records[index] = records[index].Refused(err)
			continue
		}
		records[index] = containment.Settle(ctx, registryStore, row, records[index], eligibility, nil, func(ctx context.Context) error {
			if matches, err := leases.DomainsMatchCurrent(ctx); err != nil || !matches {
				return errors.Join(mutation.StaleSnapshotError{}, err)
			}
			return nil
		})
	}
	result = protection.NewResult(records)
	if !result.Complete() {
		return result, errors.Join(fmt.Errorf("permission recovery remains pending"), result.Failure())
	}
	return result, nil
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
