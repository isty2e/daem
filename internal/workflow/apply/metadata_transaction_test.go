package apply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/recoverygate"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestApplyPlanningFailsClosedOnInterruptedMetadataTransaction(t *testing.T) {
	t.Run("dry-run", func(t *testing.T) {
		manifestPath, stateDir := applyMetadataTransactionFixture(t)
		writeApplyMetadataTransactionMarker(t, stateDir)

		_, err := PlanDryRun(context.Background(), CommandInput{ManifestPath: manifestPath})
		assertApplyMetadataTransactionError(t, err)
	})

	t.Run("write", func(t *testing.T) {
		manifestPath, stateDir := applyMetadataTransactionFixture(t)
		writeApplyMetadataTransactionMarker(t, stateDir)

		prepared, err := PlanWrite(context.Background(), CommandInput{ManifestPath: manifestPath})
		if prepared != nil {
			t.Cleanup(func() { _ = prepared.Close() })
		}
		assertApplyMetadataTransactionError(t, err)
	})
}

func TestApplyExecutionRejectsMetadataTransactionStartedAfterPlanning(t *testing.T) {
	manifestPath, stateDir := applyMetadataTransactionFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	writeApplyMetadataTransactionMarker(t, stateDir)

	_, err = ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{})
	if err == nil {
		t.Fatal("ExecuteWithOptions succeeded after metadata transaction evidence appeared")
	}
}

func applyMetadataTransactionFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifestPath := filepath.Join(root, "daem.toml")
	writeApplyFile(t, manifestPath, "version = 1\ntargets = [\"codex\"]\n")
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	paths := applyTestPaths(t, root)
	return manifestPath, paths.StateDir
}

func writeApplyMetadataTransactionMarker(t *testing.T, stateDir string) {
	t.Helper()
	authorityPath, err := transaction.FileSetAuthorityPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(authorityPath, 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(stateDir, "absent-target")
	content := `{"version":` + strconv.Itoa(contractversion.MetadataTransaction) + `,"targets":[{"path":` + strconv.Quote(targetPath) +
		`,"before":{"exists":false},"write":false}]}`
	if err := os.WriteFile(filepath.Join(authorityPath, "transaction.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestApplyPlanningFailsClosedOnAbandonedFileSetResidue(t *testing.T) {
	manifestPath, stateDir := applyMetadataTransactionFixture(t)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(stateDir, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := PlanDryRun(context.Background(), CommandInput{ManifestPath: manifestPath})
	if err == nil || !errors.Is(err, transaction.ErrAbandonedFileSetResidue) {
		t.Fatalf("error = %v, want ErrAbandonedFileSetResidue", err)
	}
	failure := ClassifyFailure(err, CommandResult{})
	if failure.Reason() != FailureReasonAbandonedFileSetResidue {
		t.Fatalf("reason = %q, want %q", failure.Reason(), FailureReasonAbandonedFileSetResidue)
	}
	if strings.Contains(failure.Detail(), "refused before effects") ||
		strings.Contains(failure.Detail(), residue) {
		t.Fatalf("detail = %q, want typed path-neutral residue diagnosis", failure.Detail())
	}
}

func TestApplyPlanningFailsClosedOnUnprovableFileSetFence(t *testing.T) {
	manifestPath, stateDir := applyMetadataTransactionFixture(t)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4097; i++ {
		name := filepath.Join(stateDir, fmt.Sprintf("entry-%04d", i))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	_, err := PlanDryRun(context.Background(), CommandInput{ManifestPath: manifestPath})
	if err == nil || !errors.Is(err, transaction.ErrFileSetFenceUnprovable) {
		t.Fatalf("error = %v, want ErrFileSetFenceUnprovable", err)
	}
	failure := ClassifyFailure(err, CommandResult{})
	if failure.Reason() != FailureReasonFileSetFenceCensusLimit {
		t.Fatalf("reason = %q, want %q", failure.Reason(), FailureReasonFileSetFenceCensusLimit)
	}
	if strings.Contains(failure.Detail(), "refused before effects") {
		t.Fatalf("detail = %q, want typed unprovable fence diagnosis", failure.Detail())
	}
}

func TestApplyPlanningFailsClosedOnUncanonicalizableStateDir(t *testing.T) {
	manifestPath, stateDir := applyMetadataTransactionFixture(t)
	replaceStateDirWithFile(t, stateDir)

	_, err := PlanDryRun(context.Background(), CommandInput{ManifestPath: manifestPath})
	if err == nil || !errors.Is(err, transaction.ErrFileSetAccessUnprovable) {
		t.Fatalf("error = %v, want ErrFileSetAccessUnprovable", err)
	}
	failure := ClassifyFailure(err, CommandResult{})
	if failure.Reason() != FailureReasonFileSetAccessUnprovable {
		t.Fatalf("reason = %q, want %q", failure.Reason(), FailureReasonFileSetAccessUnprovable)
	}
	if strings.Contains(failure.Detail(), "refused before effects") ||
		strings.Contains(failure.Detail(), "authoritative inputs changed") ||
		strings.Contains(failure.Detail(), "authorized apply plan changed") {
		t.Fatalf("detail = %q, want restore-access guidance", failure.Detail())
	}
}

func TestClassifyFailureJointJournalAndFileSetFence(t *testing.T) {
	t.Parallel()
	journalErr := fmt.Errorf("%w; run: daem recover --dry-run", journal.ErrInterruptedApply)
	err := recoverygate.Combine(journalErr, transaction.ErrAbandonedFileSetResidue)
	failure := ClassifyFailure(err, CommandResult{})
	if failure.Reason() != FailureReasonInterruptedApplyFileSetFence {
		t.Fatalf("reason = %q, want %q", failure.Reason(), FailureReasonInterruptedApplyFileSetFence)
	}
	detail := failure.Detail()
	if !strings.Contains(detail, "run daem recover") ||
		!strings.Contains(detail, "file-set fence remains") {
		t.Fatalf("detail = %q, want recover-first continuing fence", detail)
	}
	if strings.Contains(detail, "refused before effects") {
		t.Fatalf("detail = %q, want joint diagnosis", detail)
	}
}

func TestClassifyFailureUnprovableAccessDoesNotRecommendRecover(t *testing.T) {
	t.Parallel()
	err := recoverygate.Combine(
		errors.New("recovery inventory inspect failed"),
		transaction.ErrFileSetAccessUnprovable,
	)
	failure := ClassifyFailure(err, CommandResult{})
	if failure.Reason() != FailureReasonFileSetAccessUnprovable {
		t.Fatalf("reason = %q, want %q", failure.Reason(), FailureReasonFileSetAccessUnprovable)
	}
	if strings.Contains(failure.Detail(), "run daem recover") {
		t.Fatalf("detail = %q, want restore-access without recover-first", failure.Detail())
	}
}

func replaceStateDirWithFile(t *testing.T, stateDir string) {
	t.Helper()
	if err := os.RemoveAll(stateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(stateDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stateDir, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertApplyMetadataTransactionError(t *testing.T, err error) {
	t.Helper()
	if err == nil || !errors.Is(err, transaction.ErrInterruptedFileSetTransaction) {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
	failure := ClassifyFailure(err, CommandResult{})
	if failure.Reason() != FailureReasonInterruptedFileSetTransaction {
		t.Fatalf("reason = %q, want %q", failure.Reason(), FailureReasonInterruptedFileSetTransaction)
	}
}
