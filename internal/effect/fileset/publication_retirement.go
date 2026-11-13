package fileset

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func (plan *publicationPlan) evidenceDestination(name string) (rootedpath.Destination, error) {
	path := filepath.Join(filepath.Dir(plan.destination.Relative().Path()), name)
	relative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(path))
	if err != nil {
		return rootedpath.Destination{}, err
	}
	return plan.destination.Root().Bind(relative)
}

func (plan *publicationPlan) renameEvidence(ctx context.Context, source, destination string, expected storagecommit.EntryIdentity) error {
	selected, err := plan.evidenceDestination(source)
	if err != nil {
		return err
	}
	capability, err := plan.rootWitness.Acquire(selected)
	if err != nil {
		return err
	}
	request, err := storagecommit.NewRootedEntryRename(capability, destination, expected)
	if err != nil {
		_ = capability.Close()
		return err
	}
	_, err = storagecommit.CommitRootedEntryRename(ctx, request)
	return err
}

func (plan *publicationPlan) confirmEvidenceAbsent(ctx context.Context, name string) error {
	destination, err := plan.evidenceDestination(name)
	if err != nil {
		return err
	}
	capability, err := plan.rootWitness.Acquire(destination)
	if err != nil {
		return err
	}
	_, err = storagecommit.ConfirmRootedEntryAbsentWithOutcome(ctx, capability)
	return err
}

func (plan *publicationPlan) qualifyEvidenceAt(ctx context.Context, name, revision string, original storagecommit.EntryIdentity) (storagecommit.EntryIdentity, error) {
	destination, err := plan.evidenceDestination(name)
	if err != nil {
		return storagecommit.EntryIdentity{}, err
	}
	capability, err := plan.rootWitness.Acquire(destination)
	if err != nil {
		return storagecommit.EntryIdentity{}, err
	}
	defer capability.Close()
	identity, err := storagecommit.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return storagecommit.EntryIdentity{}, err
	}
	if !samePublicationDirectory(identity, original) {
		return storagecommit.EntryIdentity{}, fmt.Errorf("%w: evidence at %s is not the original directory", ErrRecoveryIntentMismatch, name)
	}
	path := filepath.Join(plan.root, name)
	view, err := readPublicationEvidenceAt(ctx, plan.root, path, mutationfs.MaximumPhysicalPathDepth, nil)
	if err != nil {
		return storagecommit.EntryIdentity{}, err
	}
	if view.markerRevision != revision || !view.intent.Equal(plan.intent) || !samePublicationDirectory(view.directoryIdentity, original) {
		return storagecommit.EntryIdentity{}, fmt.Errorf("%w: evidence at %s changed", ErrRecoveryIntentMismatch, name)
	}
	for index, target := range plan.intent.targets {
		if target.before.exists {
			if _, err := readPublicationEvidenceImage(ctx, filepath.Join(path, publicationBackupName(index)), "retired Before image", target.before.hash, target.before.size, mutationfs.MaximumPhysicalPathDepth, nil); err != nil {
				return storagecommit.EntryIdentity{}, err
			}
		}
	}
	fresh, err := storagecommit.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return storagecommit.EntryIdentity{}, err
	}
	if !samePublicationDirectory(fresh, original) {
		return storagecommit.EntryIdentity{}, fmt.Errorf("%w: evidence directory changed during qualification", ErrRecoveryIntentMismatch)
	}
	return fresh, nil
}

func (plan *publicationPlan) restoreActiveEvidence(ctx context.Context, name, revision string, original storagecommit.EntryIdentity) error {
	// original is the freshly qualified identity at the retired name.
	renameErr := plan.renameEvidence(ctx, name, transactionDirName, original)
	absenceErr := plan.confirmEvidenceAbsent(ctx, name)
	_, evidenceErr := plan.qualifyEvidenceAt(ctx, transactionDirName, revision, original)
	if absenceErr == nil && evidenceErr == nil {
		return nil
	}
	return errors.Join(renameErr, absenceErr, evidenceErr)
}

func (plan *publicationPlan) resolveRetirement(ctx context.Context, name string, original storagecommit.EntryIdentity, revision string, renameErr error) error {
	absenceErr := plan.confirmEvidenceAbsent(ctx, transactionDirName)
	retired, retiredErr := plan.qualifyEvidenceAt(ctx, name, revision, original)
	if absenceErr == nil && retiredErr == nil {
		return nil
	}
	if retiredErr == nil {
		if restoreErr := plan.restoreActiveEvidence(ctx, name, revision, retired); restoreErr == nil {
			return errors.Join(renameErr, absenceErr, fmt.Errorf("retirement was not established; original active evidence restored for recovery"))
		} else {
			return errors.Join(renameErr, absenceErr, restoreErr, fmt.Errorf("retirement remains uncertain at active and retired name %s; evidence preserved", name))
		}
	}
	_, activeErr := plan.qualifyEvidenceAt(ctx, transactionDirName, revision, original)
	if activeErr == nil {
		return errors.Join(renameErr, fmt.Errorf("retirement did not complete; original active evidence remains"))
	}
	return errors.Join(renameErr, absenceErr, retiredErr, activeErr, fmt.Errorf("retirement outcome is unproven at active and retired name %s; preserve evidence", name))
}
