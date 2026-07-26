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

	observations := make([]revisionObservation, 0, len(requests))
	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return RevisionSet{}, err
		}
		revision, err := CaptureRevision(ctx, request)
		if err != nil {
			return RevisionSet{}, err
		}
		observations = append(observations, revisionObservation{
			request:  request,
			revision: revision,
		})
	}

	return RevisionSet{observations: observations, valid: true}, nil
}

// MatchesCurrent reports whether every captured request still has the same revision.
func (set RevisionSet) MatchesCurrent(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("mutation revision-set context is required")
	}
	if !set.valid || len(set.observations) == 0 {
		return false, fmt.Errorf("mutation revision set is not initialized")
	}
	for _, observation := range set.observations {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		current, err := CaptureRevision(ctx, observation.request)
		if err != nil {
			return false, err
		}
		if !observation.revision.Equal(current) {
			return false, nil
		}
	}
	return true, nil
}
