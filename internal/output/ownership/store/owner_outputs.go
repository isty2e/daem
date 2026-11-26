package store

import (
	"context"
	"errors"
	"fmt"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/output/ownership"
)

// ReleaseOwnerOutputs replaces only the recorded registry. The containing
// operation must hold the State and registry leases and revalidate its approval
// against the very registry read that supplies the expected commit identity.
func (store Store) ReleaseOwnerOutputs(
	ctx context.Context,
	expected ownership.OwnerOutputs,
	writer mutationfs.RootedEntryCommitter,
	revalidate func(context.Context, ownership.Registry) error,
) (_ mutationfs.CommitOutcome, returnErr error) {
	if store.root == nil || expected.Empty() || writer == nil || revalidate == nil {
		return mutationfs.CommitOutcome{}, fmt.Errorf("output ownership release requires a rooted registry, selected records, and revalidation")
	}
	current, identity, exists, capability, err := store.loadRecordedForUpdate(ctx)
	if err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	defer func() {
		if capability != nil {
			returnErr = errors.Join(returnErr, capability.Close())
		}
	}()
	if err := revalidate(ctx, current.registry); err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	if !exists {
		return mutationfs.CommitOutcome{}, ownership.ErrOwnerOutputsChanged
	}
	next, err := current.registry.WithoutSettledOwnerOutputs(expected)
	if err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	content, err := current.encodeSuccessor(ctx, next, nil, nil)
	if err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	if err := capability.AdmitPhysicalWork(0, 1, int64(len(content))); err != nil {
		return mutationfs.CommitOutcome{}, err
	}
	commitCapability := capability
	capability = nil
	return writer.ReplaceRootedFileWithOutcome(ctx, commitCapability, content, mutationfs.KeepFilePermissions, identity)
}
