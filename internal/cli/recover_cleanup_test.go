package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
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

func TestRunRecoverCleanupHumanOutputIsPathNeutralAndDryRunIsEffectFree(t *testing.T) {
	fixture := writeRecoverCleanupCLIFixture(t, retirement.PhaseFinalizing, false)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions(
		[]string{"recover", "--dry-run", "--manifest", fixture.manifestPath},
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
	for _, want := range []string{
		"recover: retained_cleanup_residue",
		"operation: " + fixture.operationID,
		"finalize_journal_cleanup",
		"journal cleanup only; host, statefile, and ownership data are unchanged",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), ".daem-journal-") ||
		strings.Contains(stdout.String(), fixture.paths.RecoveryDir) {
		t.Fatalf("human cleanup output exposed private retirement path: %q", stdout.String())
	}
	fixture.assertCleanupPresent(t)
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
		!strings.Contains(stderr.String(), "recover canceled") ||
		!strings.Contains(stderr.String(), "Proceed with recover? [y/N]:") {
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
	fixture.assertCleanupAbsent(t)
	if got := readCLIRecoveryFile(t, fixture.hostPath); !bytes.Equal(got, hostBefore) {
		t.Fatalf("host content changed to %q, want %q", got, hostBefore)
	}
	if got := readCLIRecoveryFile(t, fixture.paths.StatefilePath); !bytes.Equal(got, stateBefore) {
		t.Fatalf("statefile changed to %q, want %q", got, stateBefore)
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

func readCLIRecoveryFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
