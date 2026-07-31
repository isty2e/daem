package recover

import (
	"context"
	"encoding/json"
	"errors"
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
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
)

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

	err = Execute(context.Background(), prepared)
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
	if err := Execute(context.Background(), planned); err != nil {
		t.Fatal(err)
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

func TestExecuteRejectsRecoveryDriftAndRetainsJournal(t *testing.T) {
	fixture := prepareRecoveryFixture(t, true)
	planned, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	dirty := []byte("neither before nor expected after\n")
	writeRecoverTestFile(t, fixture.hostPath, dirty)

	err = Execute(context.Background(), planned)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want StaleSnapshotError", err)
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

	err = Execute(context.Background(), planned)
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

	err = Execute(context.Background(), planned)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("Execute error = %v, want StaleSnapshotError", err)
	}
	if _, statErr := os.Stat(fixture.operationDir); statErr != nil {
		t.Fatalf("stale cleanup removed journal: %v", statErr)
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
	for _, revision := range prepared.lifecycle.planned.authorityEvidence.revisions {
		if !strings.HasPrefix(revision.Path, fixture.paths.RecoveryDir+string(os.PathSeparator)) &&
			revision.Path != fixture.paths.RecoveryDir {
			t.Fatalf("cleanup authority includes non-recovery path %q", revision.Path)
		}
	}

	if err := Execute(t.Context(), prepared); err != nil {
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

	err = Execute(t.Context(), prepared)
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
	if err := Execute(t.Context(), first); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	err = Execute(t.Context(), second)
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
	err = Execute(ctx, prepared)
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

			err = Execute(t.Context(), prepared)
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
			results <- Execute(ctx, prepared)
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

			err = Execute(t.Context(), prepared)
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
	_, err := resolveRecoveryGuardedDestinations(paths, []recovery.Action{
		{
			Scope:       target.ScopeProject,
			Destination: ".agents/skills/review",
		},
		{
			Scope:       target.ScopeGlobal,
			Destination: "~/.agents/skills/review",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "aliases incompatible logical occupancies") {
		t.Fatalf("resolveRecoveryGuardedDestinations error = %v, want physical occupancy rejection", err)
	}
}

func TestResolveRecoveryGuardedDestinationsAllowsSharedAggregateDocument(t *testing.T) {
	root := t.TempDir()
	paths := daempaths.Paths{
		ManifestRoot: root,
		DataDir:      filepath.Join(root, ".daem", "data"),
	}
	destination := ".claude/settings.json"
	resolved, err := resolveRecoveryGuardedDestinations(paths, []recovery.Action{
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
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved destinations = %d, want 1 shared document", len(resolved))
	}
}
