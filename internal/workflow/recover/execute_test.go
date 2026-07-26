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
	if disclosed.Classification() != recovery.ClassificationNeedsRollback {
		t.Fatalf("classification = %q", disclosed.Classification())
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

func TestCleanupOnlyRecoveryRevalidatesHiddenGuardedPaths(t *testing.T) {
	fixture := prepareRecoveryFixture(t, false)
	planned, err := Plan(context.Background(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	disclosed := planned.Disclosure()
	if disclosed.Classification() != recovery.ClassificationCleanBefore {
		t.Fatalf("classification = %q", disclosed.Classification())
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
