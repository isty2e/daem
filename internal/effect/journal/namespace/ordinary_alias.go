package namespace

import (
	"context"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// ObserveOrdinaryDocumentAliases collects potential aliases without granting
// authority or making an observation failure stand in for a missing file.
func ObserveOrdinaryDocumentAliases(
	ctx context.Context,
	candidates []aggregate.DocumentAddress,
	resolve func(output.Destination, rootedpath.PhysicalTraversalBudget) (string, error),
	budget rootedpath.DirectoryObservationBudget,
) (map[aggregate.DocumentAddress]mutation.DirectoryEntryAuthorityObservation, error) {
	if len(candidates) == 0 {
		return nil, ctx.Err()
	}
	allocation := &physicalObservationBudget{traversal: budget, work: budget}
	observe := mutation.NewFutureReferentAuthorityObserverBounded(physicalwork.MaximumPhysicalPathDepth, allocation)
	observations := make(map[aggregate.DocumentAddress]mutation.DirectoryEntryAuthorityObservation, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := resolve(candidate.AggregateRoot(), allocation)
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			return nil, NewUnverifiedDocumentRoute(candidate, err)
		}

		_, ordinary, err := observe(path)
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			// Only a direct proof that traversal cannot reach an entry rules
			// out an alias. A limit, unknown identity or joined error does not.
			if failure, ok := err.(*rootedpath.Failure); ok && (failure.Kind() == rootedpath.FailureSymlinkCycle || failure.Kind() == rootedpath.FailureAncestorNotDirectory) {
				continue
			}
			return nil, NewUnverifiedDocumentRoute(candidate, err)
		}
		observations[candidate] = ordinary
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return observations, nil
}

// NewUnverifiedDocumentRoute reports the logical address and retains the
// observation error for bounded diagnostics.
func NewUnverifiedDocumentRoute(address aggregate.DocumentAddress, cause error) *ProtectionError {
	return &ProtectionError{kind: ProtectionUnverified, uncertainty: ProtectionDocumentUnavailable, address: address, host: address.Target(), documents: true, cause: cause}
}
