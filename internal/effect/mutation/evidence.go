package mutation

import (
	"context"
	"fmt"
)

// StaleSnapshotError reports that authoritative evidence changed before an owned effect.
type StaleSnapshotError struct{}

func (StaleSnapshotError) Error() string {
	return "stale_snapshot: authoritative inputs changed; rerun the command from current state"
}

// StalePlanError reports that a freshly rebuilt plan no longer matches the
// exact operation disclosed to the operator.
type StalePlanError struct{}

func (StalePlanError) Error() string {
	return "stale_plan: the disclosed operation changed; review and confirm a new plan"
}

type revisionObservation struct {
	request  RevisionRequest
	revision SnapshotRevision
}

// RevisionSet is immutable evidence captured for one workflow-owned observation set.
type RevisionSet struct {
	observations []revisionObservation
	valid        bool
}

// CaptureRevisionSet captures every requested revision without granting mutation authority.
func CaptureRevisionSet(ctx context.Context, requests ...RevisionRequest) (RevisionSet, error) {
	if ctx == nil {
		return RevisionSet{}, fmt.Errorf("mutation revision-set context is required")
	}
	if len(requests) == 0 {
		return RevisionSet{}, fmt.Errorf("mutation revision-set requests are required")
	}

	observations, err := captureRevisionObservations(ctx, requests)
	if err != nil {
		return RevisionSet{}, err
	}
	for _, observation := range observations {
		if err := validateRevisionBaseline(observation.request, observation.revision); err != nil {
			return RevisionSet{}, err
		}
	}

	return RevisionSet{observations: observations, valid: true}, nil
}

// CaptureBoundedFileRevisionSet captures alias topology and content revisions
// for paths that are absent or resolve to regular files within maximumBytes.
// Existing directories and special files are rejected without traversal.
func CaptureBoundedFileRevisionSet(
	ctx context.Context,
	maximumBytes int64,
	paths ...string,
) (RevisionSet, error) {
	if maximumBytes <= 0 {
		return RevisionSet{}, fmt.Errorf(
			"bounded mutation revision maximum bytes must be positive",
		)
	}
	if len(paths) == 0 {
		return RevisionSet{}, fmt.Errorf(
			"bounded mutation revision paths are required",
		)
	}

	requests := make([]RevisionRequest, 0, len(paths)*2)
	for _, path := range paths {
		for _, effect := range []PathEffect{
			PathEffectDirectoryEntry,
			PathEffectReferent,
		} {
			requests = append(requests, RevisionRequest{
				Path:                    path,
				Effect:                  effect,
				maximumRegularFileBytes: maximumBytes,
				requireBoundedFile:      true,
			})
		}
	}
	return CaptureRevisionSet(ctx, requests...)
}

func captureRevisionObservations(
	ctx context.Context,
	requests []RevisionRequest,
) ([]revisionObservation, error) {
	fileCache := make(map[string]revisionFileCacheEntry)
	observations := make([]revisionObservation, 0, len(requests))
	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		revision, err := captureRevision(ctx, request, fileCache)
		if err != nil {
			return nil, err
		}
		observations = append(observations, revisionObservation{
			request:  request,
			revision: revision,
		})
	}
	return observations, nil
}

// MatchesCurrent reports whether every captured request still has the same revision.
func (set RevisionSet) MatchesCurrent(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("mutation revision-set context is required")
	}
	if !set.valid || len(set.observations) == 0 {
		return false, fmt.Errorf("mutation revision set is not initialized")
	}
	requests := make([]RevisionRequest, len(set.observations))
	for index, observation := range set.observations {
		requests[index] = observation.request
	}
	current, err := captureRevisionObservations(ctx, requests)
	if err != nil {
		return false, err
	}
	for index, observation := range set.observations {
		if !observation.revision.Equal(current[index].revision) {
			return false, nil
		}
	}
	return true, nil
}
