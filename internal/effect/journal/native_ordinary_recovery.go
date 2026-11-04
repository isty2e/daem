package journal

import (
	"context"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func (journal nativeJournalSnapshots) ordinaryBackups(ctx context.Context, directory string, active ActiveJournalAuthority, filesystem mutationfs.RootedReader, budget *physicalwork.Budget) ([]recovery.Entry, []recoveryBackupObservation, error) {
	entries := make([]recovery.Entry, 0, len(journal.record.Entries))
	for _, record := range journal.record.Entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		entry, err := canonicalRecoveryEntry(record)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, entry)
	}
	backups, err := recoveryBackupObservations(ctx, directory, active, journal.record.Entries, filesystem, budget)
	if err != nil {
		return nil, nil, err
	}
	if err := recovery.ValidateBackups(entries, canonicalRecoveryBackupEvidence(backups)); err != nil {
		return nil, nil, err
	}
	return entries, backups, nil
}
