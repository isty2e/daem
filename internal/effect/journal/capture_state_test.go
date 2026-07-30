package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestCaptureRecoveryStateBeforeUsesPreviousStateIdentity(t *testing.T) {
	sharedHash := testContentHashString("shared")
	previousState := resourceState(defaultRecoveryEntry(), sharedHash)
	action := pathMutationFromManagedState(t, previousState)
	action.Subject = mustTestManagedPathSubject("relocated", "instructions.project.claude")
	action.ConsumerTargets = []target.Target{target.TargetClaudeCode}
	action.Destination = outputtest.Parse(t, "CLAUDE.md")
	state := recoveryManagedStateMap(previousState)

	got, err := captureRecoveryStateBefore(action, state)
	if err != nil {
		t.Fatalf("captureRecoveryStateBefore returned error: %v", err)
	}
	if !got.Managed || got.ContentHash != sharedHash {
		t.Fatalf("state before = %#v, want managed previous state", got)
	}
}

func TestCaptureRecoveryStateBeforeAcceptsUnchangedIdentity(t *testing.T) {
	sharedHash := testContentHashString("shared")
	previousState := resourceState(defaultRecoveryEntry(), sharedHash)
	action := pathMutationFromManagedState(t, previousState)
	state := recoveryManagedStateMap(previousState)

	got, err := captureRecoveryStateBefore(action, state)
	if err != nil {
		t.Fatalf("captureRecoveryStateBefore returned error: %v", err)
	}
	if !got.Managed || got.ContentHash != sharedHash {
		t.Fatalf("state before = %#v, want managed unchanged state", got)
	}
}

func TestCaptureRecoveryStateBeforeRejectsMissingPreviousIdentity(t *testing.T) {
	sharedHash := testContentHashString("shared")
	previousState := resourceState(defaultRecoveryEntry(), sharedHash)
	action := pathMutationFromManagedState(t, previousState)
	postEntry := recoveryEntryFor(
		"relocated",
		"CLAUDE.md",
		sharedHash,
		sharedHash,
		"backup-0000",
	)
	postState := resourceState(postEntry, sharedHash)
	state := recoveryManagedStateMap(postState)

	_, err := captureRecoveryStateBefore(action, state)
	if err == nil || !strings.Contains(err.Error(), "missing state observation") {
		t.Fatalf("error = %v, want missing previous identity rejection", err)
	}
}

func TestCaptureRecoveryStateBeforeRejectsPreviousHashMismatch(t *testing.T) {
	previousState := resourceState(defaultRecoveryEntry(), testContentHashString("expected"))
	action := pathMutationFromManagedState(t, previousState)
	observed := resourceState(defaultRecoveryEntry(), testContentHashString("observed"))
	state := recoveryManagedStateMap(observed)

	_, err := captureRecoveryStateBefore(action, state)
	if err == nil || !strings.Contains(err.Error(), "does not match action previous state hash") {
		t.Fatalf("error = %v, want previous hash mismatch rejection", err)
	}
}

func TestCaptureJournalMissingPreviousIdentityLeavesNoActiveJournalOrHostEffect(t *testing.T) {
	root := t.TempDir()
	hostPath := filepath.Join(root, "CLAUDE.md")
	content := []byte("shared instructions\n")
	if err := os.WriteFile(hostPath, content, 0o600); err != nil {
		t.Fatalf("write host path: %v", err)
	}
	contentHash := artifact.HashFileContent(content)
	previous := resourceState(defaultRecoveryEntry(), string(contentHash))
	nextEntry := recoveryEntryFor(
		"shared",
		"CLAUDE.md",
		string(contentHash),
		string(contentHash),
		"backup-0000",
	)
	next := resourceState(nextEntry, string(contentHash))
	mutation, err := NewManagedPathRecordMutation(
		next.Subject(),
		next.ConsumerTargets(),
		next.Scope(),
		next.Destination(),
		next.ContentHash(),
		next.ContentHash(),
		next.ContentKind(),
		0o600,
		&previous,
	)
	if err != nil {
		t.Fatalf("NewManagedPathRecordMutation returned error: %v", err)
	}
	evidence, err := observe.NewManagedPathEvidence(
		next.Subject(),
		next.Destination(),
		true,
		next.ContentHash(),
		0o600,
	)
	if err != nil {
		t.Fatalf("NewManagedPathEvidence returned error: %v", err)
	}
	currentState := statefileFor(next)
	paths := Paths{
		RecoveryDir:   filepath.Join(root, "recovery"),
		StatefilePath: filepath.Join(root, "state.json"),
		ManifestRoot:  root,
	}

	_, err = CaptureJournalWithOptions(
		context.Background(),
		paths,
		"missing-previous",
		time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		currentState,
		currentState,
		CaptureOptions{
			Filesystem:           journalTestFilesystem(),
			ManagedPathMutations: []ManagedPathMutation{mutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			Resolver:             func(output.Destination) (string, error) { return hostPath, nil },
			StateCodec:           testStateCodec(),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "missing state observation") {
		t.Fatalf("CaptureJournal error = %v, want missing previous identity", err)
	}
	inventory, inventoryErr := loadRecoveryRootInventory(
		t.Context(),
		paths.RecoveryDir,
		inventoryOptions{
			Filesystem: journalTestFilesystem(),
			StateCodec: testStateCodec(),
		},
	)
	if inventoryErr != nil {
		t.Fatalf("loadRecoveryRootInventory returned error: %v", inventoryErr)
	}
	if inventory.decision.State() != retirement.StateClean {
		t.Fatalf("recovery inventory state = %q, want clean", inventory.decision.State())
	}
	got, readErr := os.ReadFile(hostPath)
	if readErr != nil {
		t.Fatalf("read host path: %v", readErr)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("host content = %q, want unchanged %q", got, content)
	}
}

func TestCaptureJournalRecordsCurrentPhysicalFileMode(t *testing.T) {
	root := t.TempDir()
	hostPath := filepath.Join(root, "AGENTS.md")
	content := []byte("shared instructions\n")
	if err := os.WriteFile(hostPath, content, 0o700); err != nil {
		t.Fatalf("write host path: %v", err)
	}
	contentHash := artifact.HashFileContentWithExecutable(content, true)
	previous := testAppliedState(target.TargetCodex, string(contentHash))
	mutation, err := NewManagedPathRecordMutation(
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
	paths := Paths{
		RecoveryDir:   filepath.Join(root, "recovery"),
		StatefilePath: filepath.Join(root, "state.json"),
		ManifestRoot:  root,
	}
	stateEncoder := &countingStateEncoder{}

	result, err := CaptureJournalWithOptions(
		context.Background(),
		paths,
		"physical-mode",
		time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		currentState,
		currentState,
		CaptureOptions{
			Filesystem:           journalTestFilesystem(),
			ManagedPathMutations: []ManagedPathMutation{mutation},
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

	captured, err := loadRecoveryJournal(
		context.Background(),
		journalTestFilesystem(),
		result.JournalPath,
		testStateCodec(),
	)
	if err != nil {
		t.Fatalf("loadRecoveryJournal returned error: %v", err)
	}
	mode := captured.Entries[0].Before.PathMode
	if mode == nil || mode.FileMode() != 0o700 {
		t.Fatalf("before path mode = %v, want 0700", mode)
	}
	backupPath := filepath.Join(result.Directory, filepath.FromSlash(captured.Entries[0].Before.BackupPath))
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

func TestCaptureJournalWithOptionsRejectsMissingResolverBeforeFilesystemWork(t *testing.T) {
	recoveryDir := filepath.Join(t.TempDir(), "recovery")
	_, err := CaptureJournalWithOptions(
		context.Background(),
		Paths{RecoveryDir: recoveryDir},
		"missing-resolver",
		time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		durable.EmptySnapshot(),
		durable.EmptySnapshot(),
		CaptureOptions{StateCodec: testStateCodec()},
	)
	if err == nil || !strings.Contains(err.Error(), "destination resolver is required") {
		t.Fatalf("error = %v, want missing resolver", err)
	}
	if _, statErr := os.Lstat(recoveryDir); !os.IsNotExist(statErr) {
		t.Fatalf("missing resolver created recovery storage: %v", statErr)
	}
}

func TestCaptureJournalWithOptionsRejectsMissingStateCodecBeforeFilesystemWork(t *testing.T) {
	recoveryDir := filepath.Join(t.TempDir(), "recovery")
	_, err := CaptureJournalWithOptions(
		context.Background(),
		Paths{RecoveryDir: recoveryDir},
		"missing-codec",
		time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		durable.EmptySnapshot(),
		durable.EmptySnapshot(),
		CaptureOptions{
			Resolver: func(output.Destination) (string, error) { return "", nil },
		},
	)
	if err == nil || !strings.Contains(err.Error(), "state codec is required") {
		t.Fatalf("error = %v, want missing state codec", err)
	}
	if _, statErr := os.Lstat(recoveryDir); !os.IsNotExist(statErr) {
		t.Fatalf("missing codec created recovery storage: %v", statErr)
	}
}

func TestCaptureJournalWithOptionsRejectsMissingFilesystemBeforeFilesystemWork(t *testing.T) {
	recoveryDir := filepath.Join(t.TempDir(), "recovery")
	_, err := CaptureJournalWithOptions(
		context.Background(),
		Paths{RecoveryDir: recoveryDir},
		"missing-filesystem",
		time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		durable.EmptySnapshot(),
		durable.EmptySnapshot(),
		CaptureOptions{
			Resolver:   func(output.Destination) (string, error) { return "", nil },
			StateCodec: testStateCodec(),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "filesystem is required") {
		t.Fatalf("error = %v, want missing filesystem", err)
	}
	if _, statErr := os.Lstat(recoveryDir); !os.IsNotExist(statErr) {
		t.Fatalf("missing filesystem created recovery storage: %v", statErr)
	}
}

func TestCaptureJournalWithOptionsRejectsStateEncodingFailureBeforeFilesystemWork(t *testing.T) {
	recoveryDir := filepath.Join(t.TempDir(), "recovery")
	codecErr := fmt.Errorf("injected state encoding failure")
	_, err := CaptureJournalWithOptions(
		context.Background(),
		Paths{RecoveryDir: recoveryDir},
		"encoding-failure",
		time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		durable.EmptySnapshot(),
		durable.EmptySnapshot(),
		CaptureOptions{
			Filesystem: journalTestFilesystem(),
			Resolver:   func(output.Destination) (string, error) { return "", nil },
			StateCodec: failingStateCodec{encodeErr: codecErr},
		},
	)
	if !errors.Is(err, codecErr) {
		t.Fatalf("error = %v, want state encoding failure", err)
	}
	if _, statErr := os.Lstat(recoveryDir); !os.IsNotExist(statErr) {
		t.Fatalf("encoding failure created recovery storage: %v", statErr)
	}
}

func TestCaptureJournalWithOptionsRejectsInvalidStateEncodingBeforeFilesystemWork(t *testing.T) {
	recoveryDir := filepath.Join(t.TempDir(), "recovery")
	_, err := CaptureJournalWithOptions(
		context.Background(),
		Paths{RecoveryDir: recoveryDir},
		"invalid-encoding",
		time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		durable.EmptySnapshot(),
		durable.EmptySnapshot(),
		CaptureOptions{
			Filesystem: journalTestFilesystem(),
			Resolver:   func(output.Destination) (string, error) { return "", nil },
			StateCodec: invalidStateEncoder{},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "statefile_before is not valid JSON") {
		t.Fatalf("error = %v, want invalid state encoding", err)
	}
	if _, statErr := os.Lstat(recoveryDir); !os.IsNotExist(statErr) {
		t.Fatalf("invalid encoding created recovery storage: %v", statErr)
	}
}

func TestCaptureJournalWithOptionsRejectsAfterStateEncodingFailureBeforeFilesystemWork(t *testing.T) {
	recoveryDir := filepath.Join(t.TempDir(), "recovery")
	codecErr := fmt.Errorf("injected statefile_after encoding failure")
	encoder := &failAtCallStateEncoder{failureAt: 2, err: codecErr}
	_, err := CaptureJournalWithOptions(
		context.Background(),
		Paths{RecoveryDir: recoveryDir},
		"after-encoding-failure",
		time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		durable.EmptySnapshot(),
		durable.EmptySnapshot(),
		CaptureOptions{
			Filesystem: journalTestFilesystem(),
			Resolver:   func(output.Destination) (string, error) { return "", nil },
			StateCodec: encoder,
		},
	)
	if !errors.Is(err, codecErr) || !strings.Contains(err.Error(), "statefile_after") {
		t.Fatalf("error = %v, want statefile_after encoding failure", err)
	}
	if encoder.calls != 2 {
		t.Fatalf("state encoder calls = %d, want failure on second snapshot", encoder.calls)
	}
	if _, statErr := os.Lstat(recoveryDir); !os.IsNotExist(statErr) {
		t.Fatalf("statefile_after encoding failure created recovery storage: %v", statErr)
	}
}

func pathMutationFromManagedState(t *testing.T, previous durable.ManagedPathState) pathMutation {
	t.Helper()
	return pathMutation{
		Kind:            pathMutationRecord,
		Subject:         previous.Subject(),
		ConsumerTargets: previous.ConsumerTargets(),
		Scope:           previous.Scope(),
		Destination:     previous.Destination(),
		DesiredHash:     previous.ContentHash(),
		LiveHash:        previous.ContentHash(),
		LivePathHash:    previous.ContentHash(),
		ContentKind:     previous.ContentKind(),
		PreviousState:   previousPathStateFromManaged(previous),
	}
}

func recoveryManagedStateMap(states ...durable.ManagedPathState) map[recoveryStateKey]recoveryManagedState {
	result := make(map[recoveryStateKey]recoveryManagedState, len(states))
	for _, state := range states {
		result[recoveryStateKeyForManagedPath(state)] = recoveryManagedState{
			identity:    recoveryStateIdentityFromManagedPath(state),
			contentHash: string(state.ContentHash()),
		}
	}
	return result
}
