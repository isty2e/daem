package transaction

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// FinalizationPair binds both declared referents and their revisions under the
// caller's leases. Persisted record paths never select the pair.
type FinalizationPair struct {
	requests  []mutation.RevisionRequest
	referents [2]string
	revisions mutation.RevisionSet
}

// CaptureFinalizationPair binds the selected declaration entries and referents.
func CaptureFinalizationPair(ctx context.Context, manifestPath, lockfilePath string) (FinalizationPair, error) {
	requests, err := mutation.BoundedFileRevisionRequests(declarationartifact.MaximumBytes, manifestPath, lockfilePath)
	if err != nil {
		return FinalizationPair{}, err
	}
	revisions, err := mutation.CaptureRevisionSet(ctx, requests...)
	if err != nil {
		return FinalizationPair{}, err
	}
	pair := FinalizationPair{revisions: revisions}
	for _, request := range requests {
		if request.Effect != mutation.PathEffectReferent {
			continue
		}
		index := len(pair.requests)
		pair.requests = append(pair.requests, request)
		pair.referents[index], _, err = revisions.CapturedReferent(request)
		if err != nil {
			return FinalizationPair{}, err
		}
	}
	return pair, nil
}

// RequireSameSelection accepts a fresh plan for the same referent locations;
// it does not carry publication authority or an old content revision forward.
func (pair FinalizationPair) RequireSameSelection(revisions mutation.RevisionSet) error {
	if len(pair.requests) != len(pair.referents) {
		return fmt.Errorf("declaration pair was not captured")
	}
	for index, request := range pair.requests {
		path, _, err := revisions.CapturedReferent(request)
		if err != nil {
			return err
		}
		if path != pair.referents[index] {
			return mutation.StaleSnapshotError{}
		}
	}
	return nil
}

// RetireSettled requires the manifest to be a replacement and the default
// lockfile to be written or retained. Controls are independent workspace paths.
func (pair FinalizationPair) RetireSettled(ctx context.Context, stateDir string, controls []string, budget rootedpath.PhysicalTraversalBudget, revalidate func(context.Context) error) (bool, error) {
	if len(pair.requests) != len(pair.referents) || revalidate == nil {
		return false, fmt.Errorf("retirement requires a captured pair and revalidation")
	}
	validate := func(ctx context.Context) error {
		if err := revalidate(ctx); err != nil {
			return err
		}
		matches, err := pair.revisions.MatchesCurrent(ctx)
		if err != nil {
			return err
		}
		if !matches {
			return mutation.StaleSnapshotError{}
		}
		return pair.rejectControlOverlap(controls, budget)
	}
	if err := pair.rejectControlOverlap(controls, budget); err != nil {
		return false, err
	}
	return fileset.FinalizeCommittedPair(ctx, stateDir, pair.referents[0], pair.referents[1], validate)
}

func (pair FinalizationPair) rejectControlOverlap(controls []string, budget rootedpath.PhysicalTraversalBudget) error {
	depth := mutationfs.MaximumPhysicalPathDepth
	var destinations [2]mutation.DirectoryEntryAuthorityObservation
	for index, path := range pair.referents {
		var err error
		destinations[index], err = mutation.ObserveDirectoryEntryAuthorityBounded(path, depth, budget)
		if err != nil {
			return err
		}
	}
	for _, path := range controls {
		if path == "" {
			continue
		}
		entry, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, depth, budget)
		if err != nil {
			return err
		}
		_, referent, err := mutation.ObserveFutureReferentAuthorityBounded(path, depth, budget)
		if err != nil {
			return err
		}
		for _, destination := range destinations {
			if destination.MayOverlap(entry) || destination.MayOverlap(referent) {
				return fileset.ErrDifferentFinalizationPair
			}
		}
	}
	return nil
}
