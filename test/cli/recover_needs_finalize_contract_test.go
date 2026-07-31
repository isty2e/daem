package cli_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunRecoverDryRunReportsNeedsFinalizeInHumanAndJSON(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	paths := captureCLIRecoveryNeedsFinalizeJournal(t, manifestPath)

	var humanStdout bytes.Buffer
	var humanStderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"recover", "--manifest", manifestPath, "--dry-run"},
		&humanStdout,
		&humanStderr,
	)
	if exitCode != 0 {
		t.Fatalf("human exitCode = %d, stderr = %q", exitCode, humanStderr.String())
	}
	if humanStderr.Len() != 0 {
		t.Fatalf("human stderr = %q, want empty", humanStderr.String())
	}
	for _, want := range []string{
		"recover: needs_finalize\n",
		"finalize_claims reason=needs_finalize\n",
	} {
		if !strings.Contains(humanStdout.String(), want) {
			t.Fatalf("human stdout = %q, want %q", humanStdout.String(), want)
		}
	}

	var jsonStdout bytes.Buffer
	var jsonStderr bytes.Buffer
	exitCode = testkit.RunVerboseCLI(
		[]string{"recover", "--manifest", manifestPath, "--dry-run", "--json"},
		&jsonStdout,
		&jsonStderr,
	)
	if exitCode != 0 {
		t.Fatalf("JSON exitCode = %d, stderr = %q", exitCode, jsonStderr.String())
	}
	if jsonStderr.Len() != 0 {
		t.Fatalf("JSON stderr = %q, want empty", jsonStderr.String())
	}
	payload := decodeRecoverJSONTestPayload(t, jsonStdout.Bytes())
	if payload.SchemaVersion != 4 ||
		payload.Command != "recover" ||
		payload.Mode != "dry-run" ||
		payload.AuthorityKind != "active_journal" {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.Classification != "needs_finalize" || payload.HasErrors {
		t.Fatalf("payload = %#v, want non-error needs_finalize", payload)
	}
	if payload.ActionCount != 1 || len(payload.Actions) != 1 {
		t.Fatalf("actions = %#v, want one action", payload.Actions)
	}
	if action := payload.Actions[0]; action.Kind != "finalize_claims" || action.Reason != "needs_finalize" {
		t.Fatalf("action = %#v, want finalize_claims/needs_finalize", action)
	}

	assertCLIRecoveryJournalActive(t, paths.RecoveryDir)
}

func captureCLIRecoveryNeedsFinalizeJournal(t *testing.T, manifestPath string) daempaths.Paths {
	t.Helper()

	root := filepath.Dir(manifestPath)
	home := filepath.Join(root, "home with spaces")
	testkit.SetDefaultRootEnv(t, root)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))

	const desiredContent = "managed global instructions\n"
	testkit.WriteFile(t, root, "desired.md", desiredContent)
	desiredHash := testkit.HashPath(t, filepath.Join(root, "desired.md"))

	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("daempaths.Resolve returned error: %v", err)
	}
	operationID := "20260621T120000.000000000Z-apply"
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	currentState := durable.EmptySnapshot()
	nextState := testkit.Snapshot(
		t,
		testkit.InstructionPathState(t, "global", []string{"codex"}, "global", "~/.codex/AGENTS.md", desiredHash),
	)
	desired := singleCLIManagedPath(t, nextState)
	managedMutation, err := journal.NewManagedPathCreateMutation(
		desired.Subject(),
		[]target.Target{target.TargetCodex},
		target.ScopeGlobal,
		destination,
		artifact.ContentHash(desiredHash),
		realization.PathProjectionFile,
		0o600,
		nil,
	)
	if err != nil {
		t.Fatalf("NewManagedPathCreateMutation returned error: %v", err)
	}
	managedEvidence, err := observe.NewManagedPathEvidence(desired.Subject(), destination, false, "", 0)
	if err != nil {
		t.Fatalf("NewManagedPathEvidence returned error: %v", err)
	}

	resolver := hostpath.NewResolverWithManagedDataRoot(paths.ManifestRoot, paths.DataDir)
	hostPath, err := resolver.Resolve(destination)
	if err != nil {
		t.Fatalf("Resolve host destination returned error: %v", err)
	}
	address, err := ownership.NewManagedAddress(testkit.MustObservedPathAuthority(t, hostPath), "")
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	owner, err := stateauthority.New(testkit.MustObservedPathAuthority(t, paths.StatefilePath), paths.ManifestPath)
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
	}
	transition, err := ownershipmutation.NewAcquireTransition(address, owner, operationID)
	if err != nil {
		t.Fatalf("NewAcquireTransition returned error: %v", err)
	}

	_, err = journal.CaptureJournalWithOptions(
		context.Background(),
		recoveryJournalPaths(paths),
		operationID,
		time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC),
		currentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:           testFilesystem(),
			ClaimTransitions:     []ownershipmutation.ClaimTransition{transition},
			ManagedPathMutations: []journal.ManagedPathMutation{managedMutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{managedEvidence},
			Resolver:             resolver.Resolve,
			StateCodec:           statefile.Codec{},
		},
	)
	if err != nil {
		t.Fatalf("journal.CaptureJournalWithOptions returned error: %v", err)
	}

	testkit.WriteFile(t, home, ".codex/AGENTS.md", desiredContent)
	testkit.WriteStatefile(t, paths.StatefilePath, nextState)
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatalf("ownershipstore.New returned error: %v", err)
	}
	if _, err := store.Apply(context.Background(), address, transition.Before(), transition.Prepared()); err != nil {
		t.Fatalf("reserve ownership claim returned error: %v", err)
	}

	return paths
}
