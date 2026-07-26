//go:build darwin || linux

package commit

import (
	"context"
)

// PrepareCommitParent creates and persists missing parent directories for a
// later file or same-parent prepared-tree commit.
func PrepareCommitParent(ctx context.Context, path string) error {
	return prepareCommitParentWithFaults(ctx, path, faultPlan{})
}

func prepareCommitParentWithFaults(ctx context.Context, path string, faults faultPlan) error {
	if err := validateCommitPath(path); err != nil {
		return newFailure(failureUncommitted, phaseValidate, path, err)
	}
	if err := faults.check(ctx, phaseValidate); err != nil {
		return newFailure(failureUncommitted, phaseValidate, path, err)
	}
	if err := faults.check(ctx, phaseCreateAncestors); err != nil {
		return newFailure(failureUncommitted, phaseCreateAncestors, path, err)
	}
	anchor, err := openCommitParent(path, nil, true)
	if anchor != nil {
		defer anchor.close()
	}
	if err != nil {
		return failFileBeforeVisibility(path, phaseCreateAncestors, err, anchor, "", EntryIdentity{}, faults)
	}
	if err := anchor.verifyChain(); err != nil {
		return failFileBeforeVisibility(path, phaseValidate, err, anchor, "", EntryIdentity{}, faults)
	}
	if err := faults.check(ctx, phaseSyncAncestors); err != nil {
		return failFileBeforeVisibility(path, phaseSyncAncestors, err, anchor, "", EntryIdentity{}, faults)
	}
	if err := syncCreatedAncestors(anchor); err != nil {
		return failFileBeforeVisibility(path, phaseSyncAncestors, err, anchor, "", EntryIdentity{}, faults)
	}
	if err := anchor.verifyChain(); err != nil {
		return newFailure(failureIndeterminateCommit, phaseVerifyEntry, path, err)
	}
	return nil
}
