package recover

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
)

type failRetirementRecordAdvanceStore struct {
	mutationfs.Store
	failures int
}

func (filesystem *failRetirementRecordAdvanceStore) ReplaceRootedFileWithOutcome(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	content []byte,
	mode fs.FileMode,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	path, err := capability.Destination().LexicalPath()
	if err == nil && filepath.Base(path) == retirement.RecordFileName {
		filesystem.failures++
		outcome, outcomeErr := mutationfs.NewCommitOutcome(
			mutationfs.CommitOutcomeUncommitted,
			nil,
		)
		if outcomeErr != nil {
			panic(outcomeErr)
		}
		return outcome, errors.Join(
			errors.New("injected retirement record advancement failure"),
			capability.Close(),
		)
	}
	return filesystem.Store.ReplaceRootedFileWithOutcome(
		ctx,
		capability,
		content,
		mode,
		expected,
	)
}

func TestExecuteRejectsActiveJournalReplacement(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	prepared, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	replacementID := journal.OperationID(time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC))
	replacementDir := filepath.Join(filepath.Dir(fixture.operationDir), replacementID)
	if err := os.Rename(fixture.operationDir, replacementDir); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(replacementDir, "journal.json")
	content, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]json.RawMessage
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatal(err)
	}
	persisted["operation_id"], err = json.Marshal(replacementID)
	if err != nil {
		t.Fatal(err)
	}
	content, err = json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = Execute(context.Background(), prepared, ExecuteOptions{})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want StaleSnapshotError", err)
	}
	if hostContent, readErr := os.ReadFile(fixture.hostPath); readErr != nil {
		t.Fatal(readErr)
	} else if string(hostContent) != string(fixture.newContent) {
		t.Fatalf("host content = %q, want unchanged post-apply content", hostContent)
	}
	if _, statErr := os.Stat(replacementDir); statErr != nil {
		t.Fatalf("stale recovery removed replacement journal: %v", statErr)
	}
}

func TestExecuteReplansAndRestoresCurrentRecoveryPlan(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	planned, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	disclosed := planned.Disclosure()
	active, ok := journal.ActiveRecoveryPlan(disclosed)
	if !ok {
		t.Fatalf("authority kind = %q, want active journal", disclosed.AuthorityKind())
	}
	if active.Classification() != recovery.ClassificationNeedsRollback {
		t.Fatalf("classification = %q", active.Classification())
	}
	backupPaths := make(map[string]struct{})
	for _, action := range active.Actions() {
		if action.BackupPath != "" {
			backupPaths[filepath.Join(active.OperationDir(), action.BackupPath)] = struct{}{}
		}
	}
	if len(backupPaths) == 0 {
		t.Fatal("rollback fixture has no backup authority")
	}
	result, err := Execute(context.Background(), planned, ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Phase() != ExecutionPhaseCompleted ||
		result.OperationID() != active.OperationID() ||
		result.AuthorityRetained() {
		t.Fatalf("execution result = %#v, want retired active-journal authority", result)
	}
	content, err := os.ReadFile(fixture.hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(fixture.oldContent) {
		t.Fatalf("restored content = %q", content)
	}
	if _, err := os.Stat(fixture.operationDir); !os.IsNotExist(err) {
		t.Fatalf("operation journal stat error = %v, want absent", err)
	}
}

func TestActiveRecoveryAuthorityObservesRecoveryRootIdentity(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	planned, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	replaceRecoverTestTreeWithClone(t, paths.RecoveryDir)
	if _, err := Execute(t.Context(), planned, ExecuteOptions{}); err == nil {
		t.Fatal("Execute accepted a replacement recovery root")
	}
}

func TestExecuteReclassifiesPartialRetirementAsCleanupAuthority(t *testing.T) {
	fixture := prepareRecoveryFixture(t, false)
	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	filesystem := &failRetirementRecordAdvanceStore{Store: storagecommit.Adapter{}}

	result, err := Execute(t.Context(), prepared, ExecuteOptions{Filesystem: filesystem})
	var cleanupFailure *journal.CleanupFailure
	if !errors.As(err, &cleanupFailure) ||
		cleanupFailure.Action() != retirement.ActionFinalizeJournalCleanup {
		t.Fatalf("Execute error = %v, want fresh cleanup-authority failure", err)
	}
	if filesystem.failures != 1 {
		t.Fatalf("retirement record failures = %d, want 1", filesystem.failures)
	}
	if result.Phase() != ExecutionPhaseCleanupAuthorityRetained ||
		result.AuthorityKind() != journal.RecoveryAuthorityJournalCleanup {
		t.Fatalf("execution result = %#v, want fresh cleanup-only authority", result)
	}
	disclosure, ok := result.CurrentDisclosure()
	if !ok {
		t.Fatal("cleanup-only result has no current disclosure")
	}
	cleanup, ok := journal.JournalCleanupPlan(disclosure)
	if !ok || cleanup.Action() != retirement.ActionFinalizeJournalCleanup {
		t.Fatalf("current disclosure = %#v, want journal cleanup continuation", disclosure)
	}
	if _, statErr := os.Stat(fixture.operationDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("active journal stat error = %v, want renamed authority", statErr)
	}
}

func TestExecuteRejectsRecoveryDriftAndRetainsJournal(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	planned, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	dirty := []byte("neither before nor expected after\n")
	writeRecoverTestFile(t, fixture.hostPath, dirty)

	result, err := Execute(context.Background(), planned, ExecuteOptions{})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want StaleSnapshotError", err)
	}
	if result.Phase() != ExecutionPhaseActiveAuthorityRetained || !result.AuthorityRetained() {
		t.Fatalf("execution result = %#v, want retained recovery authority", result)
	}
	content, readErr := os.ReadFile(fixture.hostPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != string(dirty) {
		t.Fatalf("drifted content changed to %q", content)
	}
	if _, statErr := os.Stat(fixture.operationDir); statErr != nil {
		t.Fatalf("stale recovery removed journal: %v", statErr)
	}
}

func TestExecuteRejectsStatefileAfterAuthorityDrift(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	planned, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}

	journalPath := filepath.Join(fixture.operationDir, "journal.json")
	content, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]json.RawMessage
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatal(err)
	}
	after, err := statefile.Decode(persisted["statefile_after"])
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
		Subject:         after.ManagedPaths()[0].Subject(),
		Target:          target.TargetCodex,
		Scope:           target.ScopeProject,
		PlanIdentityKey: "delegate:test-authority-drift",
		ObservedAt:      time.Date(2026, time.July, 17, 0, 0, 0, 0, time.UTC),
		Status:          durableattempt.DelegateStatusSucceeded,
		Reason:          durableattempt.DelegateReasonNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err = after.WithDelegateAttempts([]durableattempt.DelegateAttempt{attempt})
	if err != nil {
		t.Fatal(err)
	}
	afterContent, err := statefile.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	persisted["statefile_after"] = afterContent
	content, err = json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	current, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if planned.lifecycle.planned.operationEvidence.Equal(current.lifecycle.planned.operationEvidence) {
		t.Fatal("statefile_after drift retained the disclosed recovery operation fingerprint")
	}

	_, err = Execute(context.Background(), planned, ExecuteOptions{})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want StaleSnapshotError", err)
	}
	if content, readErr := os.ReadFile(fixture.hostPath); readErr != nil {
		t.Fatal(readErr)
	} else if string(content) != string(fixture.newContent) {
		t.Fatalf("host content = %q, want unchanged post-apply content", content)
	}
	if _, statErr := os.Stat(fixture.operationDir); statErr != nil {
		t.Fatalf("stale recovery removed journal: %v", statErr)
	}
}

func TestActiveJournalRetirementRevalidatesGuardedHostPaths(t *testing.T) {
	fixture := prepareRecoveryFixture(t, false)
	planned, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	disclosed := planned.Disclosure()
	active, ok := journal.ActiveRecoveryPlan(disclosed)
	if !ok {
		t.Fatalf("authority kind = %q, want active journal", disclosed.AuthorityKind())
	}
	if active.Classification() != recovery.ClassificationCleanBefore {
		t.Fatalf("classification = %q", active.Classification())
	}
	writeRecoverTestFile(t, fixture.hostPath, fixture.newContent)

	_, err = Execute(context.Background(), planned, ExecuteOptions{})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want StaleSnapshotError", err)
	}
	if _, statErr := os.Stat(fixture.operationDir); statErr != nil {
		t.Fatalf("stale cleanup removed journal: %v", statErr)
	}
}

func TestActiveRecoveryAuthorityCoversCompleteRemovalContinuation(t *testing.T) {
	fixture := prepareRemovalRecoveryFixture(t)
	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	active, ok := journal.ActiveRecoveryPlan(prepared.Disclosure())
	if !ok {
		t.Fatal("active recovery plan is unavailable")
	}
	intents := active.RemovalIntents()
	if len(intents) == 0 {
		t.Fatal("fixture has no removal continuation authority")
	}
	for _, action := range active.GuardedActions() {
		if action.Kind != recovery.ActionKindNoOp {
			t.Fatalf("guarded action = %#v, want semantically complete removal-only recovery", action)
		}
	}
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	evidence := prepared.lifecycle.planned.authorityEvidence
	resolver := destinationResolver(paths)
	var leasedRemovalPaths []string
	var oneParentPath string
	for _, intent := range intents {
		destinationPath, err := resolver.Resolve(intent.Destination())
		if err != nil {
			t.Fatal(err)
		}
		residuePath, cleanupPath, err := journal.RemovalNamespacePaths(intent.Namespace())
		if err != nil {
			t.Fatal(err)
		}
		if len(leasedRemovalPaths) == 0 {
			leasedRemovalPaths = []string{destinationPath, residuePath, cleanupPath}
		}
		parentPath := filepath.Dir(residuePath)
		oneParentPath = parentPath
	}

	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	leases, err := store.Acquire(t.Context(), evidence.domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer leases.Release()
	for _, path := range leasedRemovalPaths {
		competitor, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
			Path: path, Access: mutation.AccessExclusive,
			Effect: mutation.PathEffectDirectoryEntry,
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		_, acquireErr := store.Acquire(ctx, competitor)
		cancel()
		if !errors.Is(acquireErr, context.DeadlineExceeded) {
			t.Fatalf("overlapping removal lease %q error = %v, want deadline", path, acquireErr)
		}
	}
	parentCompetitor, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
		Path: oneParentPath, Access: mutation.AccessExclusive,
		Effect: mutation.PathEffectDirectoryEntry,
	})
	if err != nil {
		t.Fatal(err)
	}
	parentCtx, parentCancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer parentCancel()
	_, err = store.Acquire(parentCtx, parentCompetitor)
	if err == nil || (!errors.Is(err, context.DeadlineExceeded) &&
		!strings.Contains(err.Error(), "contains the lease store")) {
		t.Fatalf("overlapping removal-parent lease error = %v, want exclusion", err)
	}
}

func TestCleanupOnlyRecoveryUsesOnlyRetirementAuthority(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhasePrepared, true)
	writeRecoverTestFile(t, fixture.paths.StatefilePath, []byte("invalid statefile\n"))
	writeRecoverTestFile(
		t,
		fixture.paths.OwnershipRegistryPath,
		[]byte("invalid ownership registry\n"),
	)
	hostBefore := readRecoverTestFile(t, fixture.hostPath)
	stateBefore := readRecoverTestFile(t, fixture.paths.StatefilePath)
	ownershipBefore := readRecoverTestFile(t, fixture.paths.OwnershipRegistryPath)

	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatalf("Plan cleanup-only recovery: %v", err)
	}
	disclosed := prepared.Disclosure()
	if disclosed.AuthorityKind() != journal.RecoveryAuthorityJournalCleanup {
		t.Fatalf(
			"authority kind = %q, want %q",
			disclosed.AuthorityKind(),
			journal.RecoveryAuthorityJournalCleanup,
		)
	}
	cleanup, ok := journal.JournalCleanupPlan(disclosed)
	if !ok {
		t.Fatal("cleanup-only plan is unavailable")
	}
	if cleanup.Classification() != retirement.ClassificationRetainedCleanupResidue ||
		cleanup.Action() != retirement.ActionFinalizeJournalCleanup {
		t.Fatalf(
			"cleanup plan = classification %q action %q",
			cleanup.Classification(),
			cleanup.Action(),
		)
	}
	if _, err := Execute(t.Context(), prepared, ExecuteOptions{}); err != nil {
		t.Fatalf("Execute cleanup-only recovery: %v", err)
	}
	assertRecoverPathAbsent(t, fixture.controlDir)
	assertRecoverPathAbsent(t, fixture.residueDir)
	assertRecoverPathAbsent(t, fixture.garbageDir)
	if got := readRecoverTestFile(t, fixture.hostPath); string(got) != string(hostBefore) {
		t.Fatalf("host content changed to %q, want %q", got, hostBefore)
	}
	if got := readRecoverTestFile(t, fixture.paths.StatefilePath); string(got) != string(stateBefore) {
		t.Fatalf("statefile changed to %q, want %q", got, stateBefore)
	}
	if got := readRecoverTestFile(t, fixture.paths.OwnershipRegistryPath); string(got) != string(ownershipBefore) {
		t.Fatalf("ownership registry changed to %q, want %q", got, ownershipBefore)
	}
}

func TestCleanupOnlyRecoveryRejectsStaleRecordBeforeEffects(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhasePrepared, true)
	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	finalizing, err := fixture.record.Finalizing()
	if err != nil {
		t.Fatal(err)
	}
	writeRetirementRecord(t, fixture.recordPath, finalizing)

	_, err = Execute(t.Context(), prepared, ExecuteOptions{})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want StaleSnapshotError", err)
	}
	assertRecoverPathPresent(t, fixture.controlDir)
	assertRecoverPathPresent(t, fixture.residueDir)
	assertRecoverPathAbsent(t, fixture.garbageDir)
}

func TestCleanupOnlyRecoveryRejectsDuplicatePreparedPlan(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhaseFinalizing, true)
	first, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(t.Context(), first, ExecuteOptions{}); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	_, err = Execute(t.Context(), second, ExecuteOptions{})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("second Execute error = %v, want StaleSnapshotError", err)
	}
	assertRecoverPathAbsent(t, fixture.controlDir)
	assertRecoverPathAbsent(t, fixture.residueDir)
	assertRecoverPathAbsent(t, fixture.garbageDir)
}

func TestCleanupOnlyRecoveryHonorsCancellationBeforeEffects(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhaseFinalizing, true)
	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Execute(ctx, prepared, ExecuteOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context.Canceled", err)
	}
	var failure *journal.CleanupFailure
	if !errors.As(err, &failure) ||
		failure.Action() != retirement.ActionFinalizeJournalCleanup ||
		failure.Phase() != journal.CleanupFailurePhaseExecution {
		t.Fatalf("Execute error = %v, want typed cleanup cancellation", err)
	}
	const want = "journal cleanup failed: phase=execution action=finalize_journal_cleanup"
	if err.Error() != want {
		t.Fatalf("Execute error = %q, want %q", err, want)
	}
	assertRecoverPathPresent(t, fixture.controlDir)
	assertRecoverPathPresent(t, fixture.residueDir)
	assertRecoverPathAbsent(t, fixture.garbageDir)
}

func TestCleanupOnlyRecoveryRejectsPhysicalAndNamespaceDrift(t *testing.T) {
	tests := []struct {
		name  string
		drift func(*testing.T, cleanupRecoveryFixture)
	}{
		{
			name: "control identity replacement",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				replaceRecoverTestTreeWithClone(t, fixture.controlDir)
			},
		},
		{
			name: "record identity replacement with equal bytes",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				content := readRecoverTestFile(t, fixture.recordPath)
				replacement := fixture.recordPath + ".replacement"
				writeRecoverTestFile(t, replacement, content)
				if err := os.Rename(replacement, fixture.recordPath); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "residue identity replacement",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				replaceRecoverTestTreeWithClone(t, fixture.residueDir)
			},
		},
		{
			name: "recovery root identity replacement",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				replaceRecoverTestTreeWithClone(t, fixture.paths.RecoveryDir)
			},
		},
		{
			name: "admitted record temporary appears",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				writeRecoverTestFile(
					t,
					filepath.Join(fixture.controlDir, ".daem-tmp-edge-hunt"),
					[]byte("interrupted replacement\n"),
				)
			},
		},
		{
			name: "unrelated hidden root entry appears",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				path := filepath.Join(fixture.paths.RecoveryDir, ".unrelated-edge-hunt")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareCleanupRecoveryFixture(
				t,
				retirement.PhaseFinalizing,
				true,
			)
			prepared, err := Plan(t.Context(), fixture.input)
			if err != nil {
				t.Fatal(err)
			}
			test.drift(t, fixture)

			_, err = Execute(t.Context(), prepared, ExecuteOptions{})
			var stale mutation.StaleSnapshotError
			if !errors.As(err, &stale) {
				t.Fatalf("Execute error = %v, want StaleSnapshotError", err)
			}
			assertRecoverPathPresent(t, fixture.controlDir)
			assertRecoverPathPresent(t, fixture.residueDir)
			assertRecoverPathAbsent(t, fixture.garbageDir)
		})
	}
}

func TestCleanupOnlyRecoverySerializesIndependentPreparedPlans(t *testing.T) {
	fixture := prepareCleanupRecoveryFixture(t, retirement.PhaseFinalizing, true)
	first, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, prepared := range []*PreparedRecovery{first, second} {
		go func(prepared *PreparedRecovery) {
			<-start
			_, err := Execute(ctx, prepared, ExecuteOptions{})
			results <- err
		}(prepared)
	}
	close(start)

	successes := 0
	staleFailures := 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		var stale mutation.StaleSnapshotError
		if errors.As(err, &stale) {
			staleFailures++
			continue
		}
		t.Fatalf("concurrent Execute error = %v, want success or stale snapshot", err)
	}
	if successes != 1 || staleFailures != 1 {
		t.Fatalf(
			"concurrent results = successes %d stale %d, want 1 and 1",
			successes,
			staleFailures,
		)
	}
	assertRecoverPathAbsent(t, fixture.controlDir)
	assertRecoverPathAbsent(t, fixture.residueDir)
	assertRecoverPathAbsent(t, fixture.garbageDir)
}

func TestCleanupOnlyRecoveryRejectsNewlyBlockedEvidenceBeforeEffects(t *testing.T) {
	tests := []struct {
		name  string
		drift func(*testing.T, cleanupRecoveryFixture)
	}{
		{
			name: "control permissions widen",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				if err := os.Chmod(fixture.controlDir, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "record permissions widen",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				if err := os.Chmod(fixture.recordPath, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "record becomes malformed",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				writeRecoverTestFile(t, fixture.recordPath, []byte("{\"version\":1}\n"))
			},
		},
		{
			name: "record becomes cross-paired",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				record, err := retirement.NewRecord(
					"cross-paired-operation",
					fixture.record.Identity().JournalAuthorityFingerprint(),
					retirement.PhaseFinalizing,
				)
				if err != nil {
					t.Fatal(err)
				}
				writeRetirementRecord(t, fixture.recordPath, record)
			},
		},
		{
			name: "residue becomes symlink",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				held := filepath.Join(t.TempDir(), filepath.Base(fixture.residueDir))
				if err := os.Rename(fixture.residueDir, held); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(held, fixture.residueDir); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "second control appears",
			drift: func(t *testing.T, fixture cleanupRecoveryFixture) {
				record, err := retirement.NewRecord(
					"second-control-operation",
					fixture.record.Identity().JournalAuthorityFingerprint(),
					retirement.PhaseFinalizing,
				)
				if err != nil {
					t.Fatal(err)
				}
				control := filepath.Join(
					fixture.paths.RecoveryDir,
					record.Identity().ControlName(),
				)
				if err := os.Mkdir(control, retirement.DirectoryMode); err != nil {
					t.Fatal(err)
				}
				writeRetirementRecord(
					t,
					filepath.Join(control, retirement.RecordFileName),
					record,
				)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareCleanupRecoveryFixture(
				t,
				retirement.PhaseFinalizing,
				true,
			)
			prepared, err := Plan(t.Context(), fixture.input)
			if err != nil {
				t.Fatal(err)
			}
			test.drift(t, fixture)

			_, err = Execute(t.Context(), prepared, ExecuteOptions{})
			var stale mutation.StaleSnapshotError
			if !errors.As(err, &stale) {
				t.Fatalf("Execute error = %v, want StaleSnapshotError", err)
			}
			assertRecoverPathPresent(t, fixture.controlDir)
			assertRecoverPathPresent(t, fixture.residueDir)
			assertRecoverPathAbsent(t, fixture.garbageDir)
		})
	}
}

func TestResolveRecoveryGuardedDestinationsRejectsPhysicalAliases(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	paths := daempaths.Paths{
		ManifestRoot: root,
		DataDir:      filepath.Join(root, ".daem", "data"),
	}
	_, _, err := resolveRecoveryAuthorityPaths(paths, []recovery.Action{
		{
			Scope:       target.ScopeProject,
			Destination: ".agents/skills/review",
		},
		{
			Scope:       target.ScopeGlobal,
			Destination: "~/.agents/skills/review",
		},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "aliases incompatible logical occupancies") {
		t.Fatalf("resolveRecoveryAuthorityPaths error = %v, want physical occupancy rejection", err)
	}
}

func TestResolveRecoveryGuardedDestinationsAllowsSharedAggregateDocument(t *testing.T) {
	root := t.TempDir()
	paths := daempaths.Paths{
		ManifestRoot: root,
		DataDir:      filepath.Join(root, ".daem", "data"),
	}
	destination := ".claude/settings.json"
	resolved, _, err := resolveRecoveryAuthorityPaths(paths, []recovery.Action{
		{
			Scope:       target.ScopeProject,
			Destination: destination,
			ContentPath: "/hooks/PreToolUse",
		},
		{
			Scope:       target.ScopeProject,
			Destination: destination,
			ContentPath: "/hooks/PostToolUse",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved destinations = %d, want 1 shared document", len(resolved))
	}
}
