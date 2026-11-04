package journal

import (
	"context"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func ordinaryBackups(ctx context.Context, journal nativeJournalSnapshots, directory string, active ActiveJournalAuthority, filesystem mutationfs.RootedReader, budget *physicalwork.Budget) ([]recovery.Entry, []recoveryBackupObservation, error) {
	entries, err := journal.ordinaryEntries(ctx)
	if err != nil {
		return nil, nil, err
	}
	views, err := journal.recordedEntries()
	if err != nil {
		return nil, nil, err
	}
	backups, err := recoveryBackupObservations(ctx, directory, active, views, filesystem, budget)
	if err != nil {
		return nil, nil, err
	}
	if err := recovery.ValidateBackups(entries, canonicalRecoveryBackupEvidence(backups)); err != nil {
		return nil, nil, err
	}
	return entries, backups, nil
}
