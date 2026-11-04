package record

import (
	"fmt"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

type (
	recoveryPathObservation   = recovery.PathEvidence
	recoveryBackupObservation = recovery.BackupEvidence
)

func mustBuildRecoveryPlan(
	t *testing.T,
	journal recoveryJournal,
	currentState durable.Snapshot,
	observations []recoveryPathObservation,
	backupObservations []recoveryBackupObservation,
) recovery.Plan {
	t.Helper()

	plan, err := buildRecoveryPlan(testOperationID, testOperationDir, journal, currentState, observations, backupObservations, nil, ownership.EmptyRegistry(), testStateCodec())
	if err != nil {
		t.Fatalf("buildRecoveryPlan returned error: %v", err)
	}

	return plan
}

func beforePathObservation(entry recoveryEntry) recoveryPathObservation {
	if !entry.Before.Existed {
		return recoveryPathObservation{
			Path:        entry.Path,
			ContentPath: entry.ContentPath,
			Exists:      false,
			PathExisted: entry.Before.PathExisted,
			PathMode:    entry.Before.PathMode,
		}
	}

	return recoveryPathObservation{
		Path:        entry.Path,
		ContentPath: entry.ContentPath,
		Exists:      true,
		PathExisted: entry.Before.PathExisted,
		PathMode:    entry.Before.PathMode,
		Kind:        entry.Before.Kind,
		ContentHash: entry.Before.ContentHash,
		LinkTarget:  entry.Before.LinkTarget,
	}
}

func afterPathObservation(entry recoveryEntry) recoveryPathObservation {
	if !entry.ExpectedAfter.Existed {
		return recoveryPathObservation{
			Path:        entry.Path,
			ContentPath: entry.ContentPath,
			Exists:      false,
			PathExisted: entry.ExpectedAfter.PathExisted,
			PathMode:    entry.ExpectedAfter.PathMode,
		}
	}

	return recoveryPathObservation{
		Path:        entry.Path,
		ContentPath: entry.ContentPath,
		Exists:      true,
		PathExisted: entry.ExpectedAfter.PathExisted,
		PathMode:    entry.ExpectedAfter.PathMode,
		Kind:        entry.ExpectedAfter.Kind,
		ContentHash: entry.ExpectedAfter.ContentHash,
		LinkTarget:  entry.ExpectedAfter.LinkTarget,
	}
}

func dirtyPathObservation(entry recoveryEntry) recoveryPathObservation {
	return recoveryPathObservation{
		Path:        entry.Path,
		Exists:      true,
		PathMode:    testRecoveryPermissionMode(0o600),
		Kind:        recovery.PathKindFile,
		ContentHash: testDirtyHash,
	}
}

func matchingBackupObservation(entry recoveryEntry) recoveryBackupObservation {
	return recoveryBackupObservation{
		BackupPath:  entry.Before.BackupPath,
		Exists:      true,
		Kind:        entry.Before.Kind,
		ContentHash: entry.Before.ContentHash,
	}
}

func requireClassification(t *testing.T, plan recovery.Plan, want recovery.Classification) {
	t.Helper()

	if plan.Classification() != want {
		t.Fatalf("recovery.Classification = %q, want %q", plan.Classification(), want)
	}
}

func requireHealthyPlan(t *testing.T, plan recovery.Plan) {
	t.Helper()

	if plan.Blocked() {
		t.Fatalf("Blocked() = true, want false")
	}
	if plan.HasErrors() {
		t.Fatalf("HasErrors() = true, want false")
	}
}

func requireOnlyAction(t *testing.T, plan recovery.Plan, kind recovery.ActionKind, reason string) recovery.Action {
	t.Helper()

	actions := plan.Actions()
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want exactly one action", actions)
	}
	if actions[0].Kind != kind || actions[0].Reason != reason {
		t.Fatalf("action = %#v, want kind %q reason %q", actions[0], kind, reason)
	}

	return actions[0]
}

func requireAction(t *testing.T, plan recovery.Plan, kind recovery.ActionKind, reason string) recovery.Action {
	t.Helper()

	for _, action := range plan.Actions() {
		if action.Kind == kind && action.Reason == reason {
			return action
		}
	}
	t.Fatalf("actions = %#v, want kind %q reason %q", plan.Actions(), kind, reason)
	return recovery.Action{}
}

func requireErrorAction(t *testing.T, plan recovery.Plan, reason string, detail string) recovery.Action {
	t.Helper()

	action := requireAction(t, plan, recovery.ActionKindError, reason)
	if action.Detail != detail {
		t.Fatalf("error detail = %q, want %q", action.Detail, detail)
	}
	if !plan.Blocked() {
		t.Fatalf("Blocked() = false, want true")
	}
	if !plan.HasErrors() {
		t.Fatalf("HasErrors() = false, want true")
	}

	return action
}

func requireEntryFacts(t *testing.T, action recovery.Action, entry recoveryEntry) {
	t.Helper()

	wantSubject, err := entry.Subject.canonical()
	if err != nil {
		t.Fatalf("entry subject = %#v, error = %v, want canonical subject", entry.Subject, err)
	}
	gotSubject, hasSubject := action.SubjectID()
	if !hasSubject || gotSubject != wantSubject {
		t.Fatalf("action identity = subject %#v/%t, want subject %q", gotSubject, hasSubject, wantSubject)
	}
	if action.Target != target.Target(entry.Target) {
		t.Fatalf("Target = %q, want %q", action.Target, entry.Target)
	}
	wantConsumers := make([]target.Target, 0, len(entry.Targets))
	for _, value := range entry.Targets {
		wantConsumers = append(wantConsumers, target.Target(value))
	}
	if !slices.Equal(action.ConsumerTargets, wantConsumers) {
		t.Fatalf("ConsumerTargets = %v, want %v", action.ConsumerTargets, wantConsumers)
	}
	if action.Scope != target.Scope(entry.Scope) {
		t.Fatalf("Scope = %q, want %q", action.Scope, entry.Scope)
	}
	if action.Destination != entry.Path {
		t.Fatalf("Destination = %q, want %q", action.Destination, entry.Path)
	}
}

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
		observations,
		backupObservations,
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
