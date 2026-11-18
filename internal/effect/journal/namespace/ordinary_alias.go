package namespace

import (
	"context"
	"fmt"

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
	return observeDocumentAliasCandidates(ctx, candidates, resolve, budget, false)
}

// ObserveReplacementAliasCandidates only promotes proved routes into selected
// groups. Forward admission checks unresolved retained routes when needed.
func ObserveReplacementAliasCandidates(
	ctx context.Context,
	candidates []aggregate.DocumentAddress,
	resolve func(output.Destination, rootedpath.PhysicalTraversalBudget) (string, error),
	budget rootedpath.DirectoryObservationBudget,
) (map[aggregate.DocumentAddress]mutation.DirectoryEntryAuthorityObservation, error) {
	return observeDocumentAliasCandidates(ctx, candidates, resolve, budget, true)
}

func observeDocumentAliasCandidates(
	ctx context.Context,
	candidates []aggregate.DocumentAddress,
	resolve func(output.Destination, rootedpath.PhysicalTraversalBudget) (string, error),
	budget rootedpath.DirectoryObservationBudget,
	deferUnresolved bool,
) (map[aggregate.DocumentAddress]mutation.DirectoryEntryAuthorityObservation, error) {
	if ctx == nil || budget == nil || resolve == nil {
		return nil, fmt.Errorf("ordinary document alias observation requires context, resolver and shared allocation")
	}
	if len(candidates) == 0 {
		return nil, ctx.Err()
	}
	allocation := &physicalObservationBudget{traversal: budget, work: budget}
	observe := mutation.NewFutureReferentAuthorityObserverBounded(physicalwork.MaximumPhysicalPathDepth, allocation)
	observations := make(map[aggregate.DocumentAddress]mutation.DirectoryEntryAuthorityObservation, len(candidates))
	var unresolved error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := resolve(candidate.AggregateRoot(), allocation)
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		var ordinary mutation.DirectoryEntryAuthorityObservation
		if err == nil {
			_, ordinary, err = observe(path)
		}
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			// Only a direct proof that traversal cannot reach an entry rules
			// out an alias. A limit, unknown identity or joined error does not.
			if ProvesUnreachableReferent(err) {
				continue
			}
			failure := NewUnverifiedDocumentRoute(candidate, err)
			if failure.Uncertainty() == ProtectionWorkLimit {
				return nil, failure
			}
			if !deferUnresolved && unresolved == nil {
				unresolved = failure
			}
			continue
		}
		observations[candidate] = ordinary
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if unresolved != nil {
		return nil, unresolved
	}
	return observations, nil
}

// ProvesUnreachableReferent excludes traversal that completed with proof of
// no reachable entry. A composite or incomplete observation is not proof.
func ProvesUnreachableReferent(err error) bool {
	failure, ok := err.(*rootedpath.Failure)
	return ok && (failure.Kind() == rootedpath.FailureSymlinkCycle || failure.Kind() == rootedpath.FailureAncestorNotDirectory)
}

// NewUnverifiedDocumentRoute reports the logical address and retains the
// observation error for bounded diagnostics.
func NewUnverifiedDocumentRoute(address aggregate.DocumentAddress, cause error) *ProtectionError {
	return &ProtectionError{kind: ProtectionUnverified, uncertainty: ProtectionDocumentUnavailable, address: address, host: address.Target(), documents: true, cause: cause}
}
