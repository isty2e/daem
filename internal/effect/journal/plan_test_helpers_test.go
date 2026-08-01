package journal

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func buildRecoveryPlan(
	operationID string,
	operationDir string,
	journal recoveryJournal,
	currentState durable.Snapshot,
	observations []recoveryPathObservation,
	backupObservations []recoveryBackupObservation,
	claimTransitions []ownershipmutation.ClaimTransition,
	registry ownership.Registry,
	stateEncoder durable.SnapshotEncoder,
) (recovery.Plan, error) {
	fingerprint, err := recoveryJournalAuthorityFingerprint(journal, stateEncoder)
	if err != nil {
		return recovery.Plan{}, err
	}
	return buildRecoveryPlanForEntries(
		operationID,
		operationDir,
		journal,
		journal.Entries,
		currentState,
		observations,
		backupObservations,
		claimTransitions,
		registry,
		fingerprint,
	)
}

func buildRecoveryPlanForEntries(
	operationID string,
	operationDir string,
	journal recoveryJournal,
	entries []recoveryEntry,
	currentState durable.Snapshot,
	observations []recoveryPathObservation,
	backupObservations []recoveryBackupObservation,
	claimTransitions []ownershipmutation.ClaimTransition,
	registry ownership.Registry,
	journalAuthorityFingerprint string,
) (recovery.Plan, error) {
	if journalAuthorityFingerprint == "" {
		return recovery.Plan{}, fmt.Errorf("recovery journal authority fingerprint is required")
	}
	intents, err := canonicalProvisionalAcquireIntents(journal.ProvisionalAcquires)
	if err != nil {
		return recovery.Plan{}, err
	}
	authority, err := canonicalRecoveryAuthority(
		journal,
		operationDir,
		claimTransitions,
		intents,
		journalAuthorityFingerprint,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	indexes, err := recoveryEntryIndexes(journal.Entries, entries)
	if err != nil {
		return recovery.Plan{}, err
	}
	selection, err := recovery.NewSelection(authority, indexes)
	if err != nil {
		return recovery.Plan{}, err
	}
	return recovery.Classify(
		authority,
		selection,
		currentState,
		canonicalRecoveryPathEvidence(observations),
		canonicalRecoveryBackupEvidence(backupObservations),
		registry,
	)
}

func recoveryEntryIndexes(entries []recoveryEntry, selected []recoveryEntry) ([]int, error) {
	selectedKeys := make(map[entrySelectionKey]struct{}, len(selected))
	for _, entry := range selected {
		key, err := entrySelectionKeyFromRecoveryEntry(entry)
		if err != nil {
			return nil, err
		}
		if _, duplicate := selectedKeys[key]; duplicate {
			return nil, fmt.Errorf("duplicate selected recovery entry for %q", entry.Path)
		}
		selectedKeys[key] = struct{}{}
	}

	indexes := make([]int, 0, len(selected))
	for index, entry := range entries {
		key, err := entrySelectionKeyFromRecoveryEntry(entry)
		if err != nil {
			return nil, err
		}
		if _, selected := selectedKeys[key]; !selected {
			continue
		}
		indexes = append(indexes, index)
		delete(selectedKeys, key)
	}
	if len(selectedKeys) != 0 {
		return nil, fmt.Errorf("selected recovery entries do not match the active journal")
	}
	return indexes, nil
}
