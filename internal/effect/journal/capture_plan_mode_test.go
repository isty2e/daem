package journal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	"github.com/isty2e/daem/internal/assurance/observe"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

func TestCaptureJournalRecordsCurrentPhysicalFileMode(t *testing.T) {
	root := t.TempDir()
	hostPath := filepath.Join(root, "AGENTS.md")
	content := []byte("shared instructions\n")
	if err := os.WriteFile(hostPath, content, 0o700); err != nil {
		t.Fatalf("write host path: %v", err)
	}
	contentHash := artifact.HashFileContentWithExecutable(content, true)
	previous := testAppliedState(target.TargetCodex, string(contentHash))
	mutation, err := journalrecord.NewManagedPathRecordMutation(
		previous.Subject(),
		previous.ConsumerTargets(),
		previous.Scope(),
		previous.Destination(),
		contentHash,
		contentHash,
		previous.ContentKind(),
		0o600,
		&previous,
	)
	if err != nil {
		t.Fatalf("NewManagedPathRecordMutation returned error: %v", err)
	}
	evidence, err := observe.NewManagedPathEvidence(
		previous.Subject(),
		previous.Destination(),
		true,
		contentHash,
		0o600,
	)
	if err != nil {
		t.Fatalf("NewManagedPathEvidence returned error: %v", err)
	}
	currentState := statefileFor(previous)
	paths := journalpublication.Paths{
		RecoveryDir:   filepath.Join(root, "recovery"),
		StatefilePath: filepath.Join(root, "state.json"),
		ManifestRoot:  root,
	}
	stateEncoder := &countingStateEncoder{}

	result, err := journalpublication.CaptureJournalWithOptions(
		context.Background(),
		paths,
		"physical-mode",
		time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		currentState,
		currentState,
		journalpublication.CaptureOptions{
			Filesystem:           journalTestFilesystem(),
			ManagedPathMutations: []journalrecord.ManagedPathMutation{mutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			Resolver:             func(output.Destination) (string, error) { return hostPath, nil },
			StateCodec:           stateEncoder,
		},
	)
	if err != nil {
		t.Fatalf("CaptureJournal returned error: %v", err)
	}
	if stateEncoder.calls != 2 {
		t.Fatalf("state encoder calls = %d, want one call per snapshot", stateEncoder.calls)
	}

	plan, err := LoadActivePlanForStateWithOptions(
		t.Context(),
		paths,
		currentState,
		PlanLoadOptions{
			Filesystem: journalTestFilesystem(),
			Resolver:   func(output.Destination) (string, error) { return hostPath, nil },
			StateCodec: testStateCodec(),
		},
	)
	if err != nil {
		t.Fatalf("load captured physical-mode recovery plan: %v", err)
	}
	actions := plan.GuardedActions()
	if len(actions) != 1 {
		t.Fatalf("guarded recovery actions = %d, want one captured path", len(actions))
	}
	mode := actions[0].BeforePathMode
	if mode == nil || mode.FileMode() != 0o700 {
		t.Fatalf("before path mode = %v, want 0700", mode)
	}
	snapshot, err := journalTestFilesystem().ReadRegularFileSnapshotUpTo(t.Context(), result.JournalPath, journalrecord.MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := journalrecord.DecodeOrdinarySnapshot(t.Context(), snapshot, result.JournalPath, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	ordinary, present := loaded.Ordinary()
	if !present {
		t.Fatal("ordinary capture decoded as another journal family")
	}
	basis, err := ordinary.RecoveryPlanningBasis(result.Directory, result.RecordFingerprint, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries := basis.AllEntries()
	if len(entries) != 1 {
		t.Fatalf("recorded recovery entries = %d, want one", len(entries))
	}
	backupPath := filepath.Join(result.Directory, filepath.FromSlash(entries[0].Backup().Path()))
	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("stat executable recovery backup: %v", err)
	}
	if backupInfo.Mode().Perm() != 0o700 {
		t.Fatalf("backup mode = %04o, want 0700", backupInfo.Mode().Perm())
	}
	backupHash, backupKind, err := access.HashPath(context.Background(), backupPath)
	if err != nil {
		t.Fatalf("hash executable recovery backup: %v", err)
	}
	if backupKind != artifact.ArtifactKindFile || backupHash != contentHash {
		t.Fatalf("backup identity = (%q, %q), want (%q, %q)", backupKind, backupHash, artifact.ArtifactKindFile, contentHash)
	}
}
