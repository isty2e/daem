package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	recoverworkflow "github.com/isty2e/daem/internal/workflow/recover"
)

func TestRunRecoverActiveDryRunJSONPreservesSchemaFourFacts(t *testing.T) {
	fixture := writeRecoverConfirmationFixture(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions(
		[]string{"recover", "--dry-run", "--json", "--manifest", fixture.manifestPath},
		RunOptions{Stdout: &stdout, Stderr: &stderr},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf(
			"exitCode = %d stdout = %q stderr = %q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode recovery JSON: %v", err)
	}
	if payload["schema_version"] != float64(4) ||
		payload["authority_kind"] != "active_journal" ||
		payload["operation_dir"] != fixture.operationDir ||
		payload["classification"] != "needs_rollback" {
		t.Fatalf("active recovery JSON = %#v", payload)
	}
	actions, ok := payload["actions"].([]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("active recovery actions = %#v", payload["actions"])
	}
	action, ok := actions[0].(map[string]any)
	if !ok {
		t.Fatalf("active recovery action = %#v, want object", actions[0])
	}
	for _, key := range []string{
		"kind",
		"resource_id",
		"resource",
		"subject",
		"targets",
		"scope",
		"destination",
		"backup_path",
		"backup_hash",
		"backup_kind",
	} {
		if _, present := action[key]; !present {
			t.Fatalf("active recovery action omitted populated %q: %#v", key, action)
		}
	}
}

func TestRecoverActiveFailureProjectionPreservesDetailedCause(t *testing.T) {
	fixture := writeRecoverConfirmationFixture(t)
	prepared, err := recoverworkflow.Plan(t.Context(), recoverworkflow.PlanInput{
		ManifestPath: fixture.manifestPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	disclosure := prepared.Disclosure()
	cause := fmt.Errorf("active recovery destination %s changed", fixture.hostPath)
	if projected := clipresent.RecoverResultError(disclosure, cause); projected != cause {
		t.Fatalf("active recovery error was replaced: %v", projected)
	}

	var output bytes.Buffer
	if err := clipresent.PrintRecoverResultJSON(
		&output,
		"write",
		disclosure,
		cause,
	); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	errorsValue, ok := payload["errors"].([]any)
	if !ok || len(errorsValue) != 1 || errorsValue[0] != cause.Error() {
		t.Fatalf("active recovery JSON errors = %#v, want %q", payload["errors"], cause)
	}
}

func TestRunRecoverCleanupDryRunJSONUsesSchemaFourCleanupShape(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhasePrepared, true)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions(
		[]string{"recover", "--dry-run", "--json", "--manifest", fixture.manifestPath},
		RunOptions{Stdout: &stdout, Stderr: &stderr},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf(
			"exitCode = %d stdout = %q stderr = %q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode recovery JSON: %v", err)
	}
	assertJSONKeys(t, payload, []string{
		"action_count",
		"actions",
		"authority_kind",
		"classification",
		"command",
		"has_errors",
		"mode",
		"operation_id",
		"schema_version",
	})
	if payload["schema_version"] != float64(4) ||
		payload["command"] != "recover" ||
		payload["mode"] != "dry-run" ||
		payload["authority_kind"] != "journal_cleanup" ||
		payload["operation_id"] != fixture.operationID ||
		payload["classification"] != "retained_cleanup_residue" ||
		payload["action_count"] != float64(1) ||
		payload["has_errors"] != false {
		t.Fatalf("cleanup JSON = %#v", payload)
	}
	actions, ok := payload["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("cleanup actions = %#v, want one action", payload["actions"])
	}
	action, ok := actions[0].(map[string]any)
	if !ok {
		t.Fatalf("cleanup action = %#v, want object", actions[0])
	}
	assertJSONKeys(t, action, []string{"kind"})
	if action["kind"] != "finalize_journal_cleanup" {
		t.Fatalf("cleanup action = %#v", action)
	}
	if strings.Contains(stdout.String(), ".daem-journal-") ||
		strings.Contains(stdout.String(), fixture.paths.RecoveryDir) {
		t.Fatalf("cleanup JSON exposed private retirement path: %s", stdout.String())
	}
	fixture.assertCleanupPresent(t)
}

func TestRunRecoverCleanupHumanOutputIsExactPathNeutralAndEffectFree(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, false)
	want := "recover: retained_cleanup_residue\n" +
		"operation: " + fixture.operationID + "\n" +
		"finalize_journal_cleanup\n" +
		"journal cleanup only; host, statefile, and ownership data are unchanged\n"

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "default"},
		{name: "verbose", args: []string{"--verbose"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			args := []string{"recover", "--dry-run", "--manifest", fixture.manifestPath}
			args = append(args, test.args...)

			exitCode := RunWithOptions(
				args,
				RunOptions{Stdout: &stdout, Stderr: &stderr},
			)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf(
					"exitCode = %d stdout = %q stderr = %q",
					exitCode,
					stdout.String(),
					stderr.String(),
				)
			}
			if stdout.String() != want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), want)
			}
			if strings.Contains(stdout.String(), ".daem-journal-") ||
				strings.Contains(stdout.String(), fixture.paths.RecoveryDir) {
				t.Fatalf("human cleanup output exposed private retirement path: %q", stdout.String())
			}
		})
	}
	fixture.assertCleanupPresent(t)
}

func TestRecoverCleanupFailureProjectionIsSharedAndPathNeutral(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, false)
	prepared, err := recoverworkflow.Plan(t.Context(), recoverworkflow.PlanInput{
		ManifestPath: fixture.manifestPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	disclosure := prepared.Disclosure()
	cleanup, ok := journal.JournalCleanupPlan(disclosure)
	if !ok {
		t.Fatalf("authority kind = %q, want journal cleanup", disclosure.AuthorityKind())
	}
	cause := fmt.Errorf("remove %s: permission denied", fixture.garbageDir)
	resultErr := journal.WrapCleanupFailure(cleanup.Action(), cause)
	projected := clipresent.RecoverResultError(disclosure, resultErr)
	const want = "journal cleanup failed: phase=execution action=finalize_journal_cleanup"
	if projected.Error() != want || humanDiagnosticError(projected) != want {
		t.Fatalf(
			"projected error = %q human = %q, want %q",
			projected,
			humanDiagnosticError(projected),
			want,
		)
	}

	var output bytes.Buffer
	if err := clipresent.PrintRecoverResultJSON(
		&output,
		"write",
		disclosure,
		resultErr,
	); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	assertJSONKeys(t, payload, []string{
		"action_count",
		"actions",
		"authority_kind",
		"classification",
		"command",
		"errors",
		"has_errors",
		"mode",
		"operation_id",
		"schema_version",
	})
	errorsValue, ok := payload["errors"].([]any)
	if !ok || len(errorsValue) != 1 || errorsValue[0] != want {
		t.Fatalf("cleanup JSON errors = %#v, want %q", payload["errors"], want)
	}
	if strings.Contains(output.String(), fixture.paths.RecoveryDir) ||
		strings.Contains(output.String(), "permission denied") {
		t.Fatalf("cleanup JSON exposed private cause: %s", output.String())
	}
}

func TestRunRecoverCleanupInteractiveDeclineRetainsArtifacts(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, true)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions(
		[]string{"recover", "--manifest", fixture.manifestPath},
		interactiveRunOptions(strings.NewReader("no\n"), &stdout, &stderr),
	)
	if exitCode != 1 ||
		stderr.String() != "Proceed with recover? [y/N]: \nrecover canceled\n" {
		t.Fatalf(
			"exitCode = %d stdout = %q stderr = %q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	fixture.assertCleanupPresent(t)
}

func TestRunRecoverCleanupYesFinalizesOnlyRetirementArtifacts(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, true)
	hostBefore := readCLIRecoveryFile(t, fixture.hostPath)
	stateBefore := readCLIRecoveryFile(t, fixture.paths.StatefilePath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions(
		[]string{"recover", "--yes", "--json", "--manifest", fixture.manifestPath},
		RunOptions{Stdout: &stdout, Stderr: &stderr},
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf(
			"exitCode = %d stdout = %q stderr = %q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	assertCleanupWriteJSON(
		t,
		stdout.Bytes(),
		fixture,
		false,
		"",
	)
	fixture.assertCleanupAbsent(t)
	if got := readCLIRecoveryFile(t, fixture.hostPath); !bytes.Equal(got, hostBefore) {
		t.Fatalf("host content changed to %q, want %q", got, hostBefore)
	}
	if got := readCLIRecoveryFile(t, fixture.paths.StatefilePath); !bytes.Equal(got, stateBefore) {
		t.Fatalf("statefile changed to %q, want %q", got, stateBefore)
	}
}

func TestRunRecoverCleanupExecutionFailureIsPathNeutral(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, true)
	filesystem := &failNthCleanupStore{
		Store:  storagecommit.Adapter{},
		failOn: 1,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions(
		[]string{"recover", "--yes", "--json", "--manifest", fixture.manifestPath},
		RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			RecoverExecuteOptions: recoverworkflow.ExecuteOptions{
				Filesystem: filesystem,
			},
		},
	)
	const want = "journal cleanup failed: phase=execution action=finalize_journal_cleanup"
	if exitCode != 1 || stderr.Len() != 0 {
		t.Fatalf(
			"exitCode = %d stdout = %q stderr = %q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	if filesystem.calls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", filesystem.calls)
	}
	assertCleanupWriteJSON(t, stdout.Bytes(), fixture, true, want)
	assertNoCleanupPrivateCause(t, stdout.String(), fixture)
	for _, path := range []string{fixture.controlDir, fixture.residueDir} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("recoverable cleanup path %q missing: %v", path, err)
		}
	}
	if _, err := os.Lstat(fixture.garbageDir); !os.IsNotExist(err) {
		t.Fatalf("garbage-collection path exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunRecoverCleanupGarbageCollectionFailureIsPathNeutral(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, true)
	filesystem := &failNthCleanupStore{
		Store:  storagecommit.Adapter{},
		failOn: 2,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions(
		[]string{"recover", "--yes", "--manifest", fixture.manifestPath},
		RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			RecoverExecuteOptions: recoverworkflow.ExecuteOptions{
				Filesystem: filesystem,
			},
		},
	)
	wantPlan := "recover: retained_cleanup_residue\n" +
		"operation: " + fixture.operationID + "\n" +
		"finalize_journal_cleanup\n" +
		"journal cleanup only; host, statefile, and ownership data are unchanged\n"
	const wantError = "recover failed: journal cleanup incomplete: phase=garbage_collection action=finalize_journal_cleanup; semantic retirement is committed and no recovery action remains\n"
	if exitCode != 1 || stdout.String() != wantPlan || stderr.String() != wantError {
		t.Fatalf(
			"exitCode = %d stdout = %q stderr = %q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	if filesystem.calls != 2 {
		t.Fatalf("cleanup calls = %d, want 2", filesystem.calls)
	}
	assertNoCleanupPrivateCause(t, stdout.String()+stderr.String(), fixture)
	if _, err := os.Lstat(fixture.garbageDir); err != nil {
		t.Fatalf("garbage-collection residue missing: %v", err)
	}
	for _, path := range []string{fixture.controlDir, fixture.residueDir} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("retired cleanup path %q exists or stat failed unexpectedly: %v", path, err)
		}
	}
}

func TestRunRecoverBlocksPre10JournalTombstoneBeforeEffects(t *testing.T) {
	fixture := writeRecoverConfirmationFixture(t)
	paths, err := daempaths.Resolve(fixture.manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(
		paths.RecoveryDir,
		".daem-tombstone-"+strings.Repeat("a", 32),
	)
	if err := os.Rename(fixture.operationDir, legacyDir); err != nil {
		t.Fatal(err)
	}
	hostBefore := readCLIRecoveryFile(t, fixture.hostPath)
	stateBefore := readCLIRecoveryFile(t, paths.StatefilePath)

	tests := []struct {
		name string
		args []string
	}{
		{name: "dry run", args: []string{"recover", "--dry-run", "--json"}},
		{name: "confirmed", args: []string{"recover", "--yes", "--json"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			args := append(test.args, "--manifest", fixture.manifestPath)
			exitCode := RunWithOptions(
				args,
				RunOptions{Stdout: &stdout, Stderr: &stderr},
			)
			if exitCode != 1 || stdout.Len() != 0 ||
				!strings.Contains(stderr.String(), "unsupported authority schema") ||
				!strings.Contains(stderr.String(), "use the daem version that wrote it") {
				t.Fatalf(
					"exitCode = %d stdout = %q stderr = %q",
					exitCode,
					stdout.String(),
					stderr.String(),
				)
			}
			if _, err := os.Lstat(legacyDir); err != nil {
				t.Fatalf("blocked recovery changed legacy tombstone: %v", err)
			}
			if got := readCLIRecoveryFile(t, fixture.hostPath); !bytes.Equal(got, hostBefore) {
				t.Fatalf("host content changed to %q, want %q", got, hostBefore)
			}
			if got := readCLIRecoveryFile(t, paths.StatefilePath); !bytes.Equal(got, stateBefore) {
				t.Fatalf("statefile changed to %q, want %q", got, stateBefore)
			}
		})
	}
}

type recoverCleanupCLIFixture struct {
	recoverConfirmationFixture
	paths       daempaths.Paths
	operationID string
	controlDir  string
	residueDir  string
	garbageDir  string
}

type failNthCleanupStore struct {
	mutationfs.Store
	failOn int
	calls  int
}

func (filesystem *failNthCleanupStore) CleanupRootedEntry(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
) (mutationfs.CommitOutcome, error) {
	filesystem.calls++
	if filesystem.calls != filesystem.failOn {
		return filesystem.Store.CleanupRootedEntry(ctx, capability, expected)
	}
	outcome, outcomeErr := mutationfs.NewCommitOutcome(
		mutationfs.CommitOutcomeUncommitted,
		nil,
	)
	if outcomeErr != nil {
		panic(outcomeErr)
	}
	privatePath := "private cleanup path"
	if capability != nil {
		if path, err := capability.Destination().LexicalPath(); err == nil {
			privatePath = path
		}
	}
	cause := fmt.Errorf("remove %s: permission denied", privatePath)
	if capability != nil {
		cause = errors.Join(cause, capability.Close())
	}
	return outcome, cause
}

func writeRecoverCleanupCLIFixture(
	t *testing.T,
	phase retirement.Phase,
	residuePresent bool,
) recoverCleanupCLIFixture {
	t.Helper()
	fixture := writeRecoverConfirmationFixture(t)
	paths, err := daempaths.Resolve(fixture.manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := recoverworkflow.Plan(t.Context(), recoverworkflow.PlanInput{
		ManifestPath: fixture.manifestPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	active, ok := journal.ActiveRecoveryPlan(prepared.Disclosure())
	if !ok {
		t.Fatalf(
			"authority kind = %q, want active journal",
			prepared.Disclosure().AuthorityKind(),
		)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := active.JournalAuthorityFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	record, err := retirement.NewRecord(active.OperationID(), fingerprint, phase)
	if err != nil {
		t.Fatal(err)
	}
	content, err := retirement.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	identity := record.Identity()
	controlDir := filepath.Join(paths.RecoveryDir, identity.ControlName())
	if err := os.MkdirAll(controlDir, retirement.DirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(controlDir, retirement.RecordFileName),
		content,
		retirement.RecordMode,
	); err != nil {
		t.Fatal(err)
	}
	residueDir := filepath.Join(paths.RecoveryDir, identity.ResidueName())
	if residuePresent {
		if err := os.Rename(fixture.operationDir, residueDir); err != nil {
			t.Fatal(err)
		}
	} else if err := os.RemoveAll(fixture.operationDir); err != nil {
		t.Fatal(err)
	}
	return recoverCleanupCLIFixture{
		recoverConfirmationFixture: fixture,
		paths:                      paths,
		operationID:                active.OperationID(),
		controlDir:                 controlDir,
		residueDir:                 residueDir,
		garbageDir:                 filepath.Join(paths.RecoveryDir, identity.GCName()),
	}
}

func (fixture recoverCleanupCLIFixture) assertCleanupPresent(t *testing.T) {
	t.Helper()
	if _, err := os.Lstat(fixture.controlDir); err != nil {
		t.Fatalf("cleanup control missing: %v", err)
	}
}

func (fixture recoverCleanupCLIFixture) assertCleanupAbsent(t *testing.T) {
	t.Helper()
	for _, path := range []string{
		fixture.controlDir,
		fixture.residueDir,
		fixture.garbageDir,
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("cleanup path %q exists or stat failed unexpectedly: %v", path, err)
		}
	}
}

func assertJSONKeys(t *testing.T, value map[string]any, want []string) {
	t.Helper()
	got := make([]string, 0, len(value))
	for key := range value {
		got = append(got, key)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("JSON keys = %v, want %v", got, want)
	}
}

func assertCleanupWriteJSON(
	t *testing.T,
	content []byte,
	fixture recoverCleanupCLIFixture,
	hasErrors bool,
	wantError string,
) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf("decode recovery JSON: %v", err)
	}
	keys := []string{
		"action_count",
		"actions",
		"authority_kind",
		"classification",
		"command",
		"has_errors",
		"mode",
		"operation_id",
		"schema_version",
	}
	if hasErrors {
		keys = append(keys, "errors")
	}
	assertJSONKeys(t, payload, keys)
	if payload["schema_version"] != float64(4) ||
		payload["command"] != "recover" ||
		payload["mode"] != "write" ||
		payload["authority_kind"] != "journal_cleanup" ||
		payload["operation_id"] != fixture.operationID ||
		payload["classification"] != "retained_cleanup_residue" ||
		payload["action_count"] != float64(1) ||
		payload["has_errors"] != hasErrors {
		t.Fatalf("cleanup write JSON = %#v", payload)
	}
	actions, ok := payload["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("cleanup actions = %#v, want one action", payload["actions"])
	}
	action, ok := actions[0].(map[string]any)
	if !ok {
		t.Fatalf("cleanup action = %#v, want object", actions[0])
	}
	assertJSONKeys(t, action, []string{"kind"})
	if action["kind"] != "finalize_journal_cleanup" {
		t.Fatalf("cleanup action = %#v", action)
	}
	if hasErrors {
		errorsValue, ok := payload["errors"].([]any)
		if !ok || len(errorsValue) != 1 || errorsValue[0] != wantError {
			t.Fatalf("cleanup JSON errors = %#v, want %q", payload["errors"], wantError)
		}
	}
}

func assertNoCleanupPrivateCause(
	t *testing.T,
	output string,
	fixture recoverCleanupCLIFixture,
) {
	t.Helper()
	if strings.Contains(output, fixture.paths.RecoveryDir) ||
		strings.Contains(output, ".daem-journal-") ||
		strings.Contains(output, "permission denied") {
		t.Fatalf("cleanup output exposed private cause: %s", output)
	}
}

func readCLIRecoveryFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
