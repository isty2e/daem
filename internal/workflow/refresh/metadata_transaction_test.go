package refresh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestRefreshPreservesKnownFenceBesideUnknownJournal(t *testing.T) {
	cause := recoverygate.Combine(
		errors.New("recovery inventory inspection failed"),
		transaction.ErrAbandonedFileSetResidue,
	)
	planned, err := journalAndFileSetRefusal(CommandResult{Mode: ModeExecute}, cause)
	if err == nil || planned.result.ReasonCode != ReasonAbandonedFileSetResidue {
		t.Fatalf("result=%#v err=%v", planned.result, err)
	}
	state := planned.result.RecoveryBarrier
	if !state.JournalObserved() || state.JournalKnown() ||
		!state.FileSetKnown() || state.FileSet() != transaction.FileSetFenceAbandonedResidue ||
		!isPreservedReplanCause(err) {
		t.Fatalf("barrier state = %#v", state)
	}
	if !strings.Contains(planned.result.FailureDetail(), "journal recovery authority could not be classified") {
		t.Fatalf("detail = %q", planned.result.FailureDetail())
	}
}

func TestRefreshPlanningFailsClosedOnInterruptedMetadataTransaction(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)

	t.Run("dry-run", func(t *testing.T) {
		result, err := PlanDryRun(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		assertRefreshMetadataTransactionRefusal(t, result, err)
	})

	t.Run("write", func(t *testing.T) {
		prepared, err := PlanWrite(context.Background(), CommandInput{
			ManifestPath: manifestPath,
			ExtensionID:  "formatter",
		}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
		if prepared != nil {
			t.Cleanup(func() { _ = prepared.Close() })
			assertRefreshMetadataTransactionRefusal(t, prepared.Disclosure(), err)
			return
		}
		assertRefreshMetadataTransactionRefusal(t, CommandResult{}, err)
	})
}

func TestRefreshPlanningFailsClosedOnAbandonedFileSetResidue(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(paths.StateDir, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err == nil || !errors.Is(err, transaction.ErrAbandonedFileSetResidue) {
		t.Fatalf("error = %v, want ErrAbandonedFileSetResidue", err)
	}
	if result.ReasonCode != ReasonAbandonedFileSetResidue {
		t.Fatalf("reason = %q, want %q", result.ReasonCode, ReasonAbandonedFileSetResidue)
	}
	if got := result.FailureDetail(); !strings.Contains(got, "abandoned file-set residue") {
		t.Fatalf("detail = %q, want residue diagnosis", got)
	}
	joined := strings.Join(result.Remediation, "\n")
	if strings.Contains(joined, "retry the interrupted") {
		t.Fatalf("remediation = %q, want no interrupted-write retry", joined)
	}
	if !strings.Contains(joined, "preserve the reported residue") {
		t.Fatalf("remediation = %q, want preserve-for-analysis guidance", joined)
	}
}

func TestRefreshPlanningFailsClosedOnUnprovableFileSetFence(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4097; i++ {
		name := filepath.Join(paths.StateDir, fmt.Sprintf("entry-%04d", i))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err == nil || !errors.Is(err, transaction.ErrFileSetFenceUnprovable) {
		t.Fatalf("error = %v, want ErrFileSetFenceUnprovable", err)
	}
	if result.ReasonCode != ReasonFileSetFenceCensusLimit {
		t.Fatalf("reason = %q, want %q", result.ReasonCode, ReasonFileSetFenceCensusLimit)
	}
	if got := result.FailureDetail(); !strings.Contains(got, "bounded file-set fence census") {
		t.Fatalf("detail = %q, want census-limit diagnosis", got)
	}
	joined := strings.Join(result.Remediation, "\n")
	if strings.Contains(joined, "retry the interrupted") {
		t.Fatalf("remediation = %q, want no interrupted-write retry", joined)
	}
	if !strings.Contains(joined, "bounded file-set census") {
		t.Fatalf("remediation = %q, want bounded census guidance", joined)
	}
}

func TestRefreshSecondPassPreservesAbandonedFileSetResidue(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	builder := syntheticRefreshCommandBuilder(t)
	commandBuilder := func(input CommandBuildInput) (CommandSpec, error) {
		spec, err := builder(input)
		if err != nil {
			return CommandSpec{}, err
		}
		paths, resolveErr := daempaths.Resolve(manifestPath)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		plantAbandonedFileSetResidue(t, paths.StateDir)
		return spec, nil
	}

	result, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: commandBuilder})
	if err == nil || !errors.Is(err, transaction.ErrAbandonedFileSetResidue) {
		t.Fatalf("error = %v, want ErrAbandonedFileSetResidue", err)
	}
	if result.ReasonCode != ReasonAbandonedFileSetResidue {
		t.Fatalf("reason = %q, want %q", result.ReasonCode, ReasonAbandonedFileSetResidue)
	}
	joined := strings.Join(result.Remediation, "\n")
	if strings.Contains(joined, "rerun dry-run") || strings.Contains(joined, "retry the interrupted") {
		t.Fatalf("remediation = %q, want preserve-residue guidance not stale-plan retry", joined)
	}
}

func TestRefreshExecutePreservesAbandonedFileSetResidueAfterPlanning(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	plantAbandonedFileSetResidue(t, paths.StateDir)

	result, err := Execute(context.Background(), prepared, ExecuteOptions{})
	if err == nil || !errors.Is(err, transaction.ErrAbandonedFileSetResidue) {
		t.Fatalf("error = %v, want ErrAbandonedFileSetResidue", err)
	}
	if result.ReasonCode != ReasonAbandonedFileSetResidue {
		t.Fatalf("reason = %q, want %q", result.ReasonCode, ReasonAbandonedFileSetResidue)
	}
	joined := strings.Join(result.Remediation, "\n")
	if strings.Contains(joined, "review a new dry-run") || strings.Contains(joined, "retry the interrupted") {
		t.Fatalf("remediation = %q, want preserve-residue guidance not stale-plan retry", joined)
	}
}

func TestRefreshPlanningFailsClosedOnUncanonicalizableStateDir(t *testing.T) {
	manifestPath := writeNoObserverRefreshFixture(t)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	replaceStateDirWithFile(t, paths.StateDir)

	result, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err == nil || !errors.Is(err, transaction.ErrFileSetAccessUnprovable) {
		t.Fatalf("error = %v, want ErrFileSetAccessUnprovable", err)
	}
	if result.ReasonCode != ReasonFileSetAccessUnprovable {
		t.Fatalf("reason = %q, want %q", result.ReasonCode, ReasonFileSetAccessUnprovable)
	}
	joined := strings.Join(result.Remediation, "\n")
	if strings.Contains(joined, "retry the interrupted") || strings.Contains(joined, "rerun dry-run") {
		t.Fatalf("remediation = %q, want restore-access guidance not retry", joined)
	}
}

func plantAbandonedFileSetResidue(t *testing.T, stateDir string) {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	residue := filepath.Join(stateDir, ".daem-tmp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := os.Mkdir(residue, 0o700); err != nil {
		t.Fatal(err)
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

func TestRefreshPlanningCancellationDuringFenceInspectionStaysCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manifestPath := writeNoObserverRefreshFixture(t)
	result, err := PlanDryRun(ctx, CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "formatter",
	}, PlanOptions{CommandBuilder: syntheticRefreshCommandBuilder(t)})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if result.ReasonCode != ReasonCancelled {
		t.Fatalf("reason = %q, want %q", result.ReasonCode, ReasonCancelled)
	}
	joined := strings.Join(result.Remediation, "\n")
	if strings.Contains(joined, "retry the interrupted") {
		t.Fatalf("remediation = %q, want no interrupted-write retry", joined)
	}
}

func TestFileSetFenceRefusalClassifiesSentinels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantReason ReasonCode
		forbidden  string
	}{
		{
			name:       "cancelled census",
			err:        context.Canceled,
			wantReason: ReasonCancelled,
			forbidden:  "retry the interrupted",
		},
		{
			name:       "abandoned residue",
			err:        transaction.ErrAbandonedFileSetResidue,
			wantReason: ReasonAbandonedFileSetResidue,
			forbidden:  "retry the interrupted",
		},
		{
			name:       "census limit",
			err:        transaction.ErrFileSetFenceCensusLimit,
			wantReason: ReasonFileSetFenceCensusLimit,
			forbidden:  "retry the interrupted",
		},
		{
			name:       "access unprovable",
			err:        transaction.ErrFileSetAccessUnprovable,
			wantReason: ReasonFileSetAccessUnprovable,
			forbidden:  "recover --dry-run",
		},
		{
			name:       "interrupted marker",
			err:        transaction.ErrInterruptedFileSetTransaction,
			wantReason: ReasonInterruptedFileSetTransaction,
			forbidden:  "preserve the reported residue",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			planned, err := journalAndFileSetRefusal(CommandResult{}, test.err)
			if err == nil {
				t.Fatal("expected refusal")
			}
			if planned.result.ReasonCode != test.wantReason {
				t.Fatalf("reason = %q, want %q", planned.result.ReasonCode, test.wantReason)
			}
			joined := strings.Join(planned.result.Remediation, "\n")
			if strings.Contains(joined, test.forbidden) {
				t.Fatalf("remediation = %q, must not contain %q", joined, test.forbidden)
			}
		})
	}
}

func TestJournalAndFileSetRefusalJointContract(t *testing.T) {
	t.Parallel()
	journalErr := fmt.Errorf("%w; run: daem recover --dry-run", journal.ErrInterruptedApply)
	err := recoverygate.Combine(journalErr, transaction.ErrAbandonedFileSetResidue)
	planned, refuseErr := journalAndFileSetRefusal(CommandResult{}, err)
	if refuseErr == nil {
		t.Fatal("expected refusal")
	}
	if planned.result.ReasonCode != ReasonInterruptedApplyFileSetFence {
		t.Fatalf("reason = %q, want %q", planned.result.ReasonCode, ReasonInterruptedApplyFileSetFence)
	}
	joined := strings.Join(planned.result.Remediation, "\n")
	if !strings.Contains(joined, "recover --dry-run first") ||
		!strings.Contains(joined, "fence remains") {
		t.Fatalf("remediation = %q, want recover-first continuing fence", joined)
	}
}

func TestJournalAndFileSetRefusalUnprovableAccessForbidsRecoverFirst(t *testing.T) {
	t.Parallel()
	err := recoverygate.Combine(
		errors.New("recovery inventory inspect failed"),
		transaction.ErrFileSetAccessUnprovable,
	)
	planned, refuseErr := journalAndFileSetRefusal(CommandResult{}, err)
	if refuseErr == nil {
		t.Fatal("expected refusal")
	}
	if planned.result.ReasonCode != ReasonFileSetAccessUnprovable {
		t.Fatalf("reason = %q, want %q", planned.result.ReasonCode, ReasonFileSetAccessUnprovable)
	}
	joined := strings.Join(planned.result.Remediation, "\n")
	if strings.Contains(joined, "recover --dry-run") {
		t.Fatalf("remediation = %q, want restore-access without recover-first", joined)
	}
}

func TestMapPreservedReplanCauseKeepsJournalAndCancellation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantReason ReasonCode
	}{
		{
			name:       "journal only",
			err:        errors.Join(mutation.StalePlanError{}, journal.ErrInterruptedApply),
			wantReason: ReasonInterruptedApply,
		},
		{
			name: "joint journal and fence",
			err: errors.Join(
				mutation.StalePlanError{},
				recoverygate.Combine(journal.ErrInterruptedApply, transaction.ErrAbandonedFileSetResidue),
			),
			wantReason: ReasonInterruptedApplyFileSetFence,
		},
		{
			name: "cleanup only",
			err: errors.Join(
				mutation.StalePlanError{},
				journal.ErrIncompleteJournalCleanup,
			),
			wantReason: ReasonJournalCleanupIncomplete,
		},
		{
			name:       "cancellation after journal observation",
			err:        recoverygate.Combine(journal.ErrInterruptedApply, context.Canceled),
			wantReason: ReasonCancelled,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, mapped, ok := mapPreservedReplanCause(CommandResult{}, test.err)
			if !ok || mapped == nil {
				t.Fatalf("mapPreservedReplanCause = (%#v, %v, %t), want mapped", result, mapped, ok)
			}
			if result.ReasonCode != test.wantReason {
				t.Fatalf("reason = %q, want %q", result.ReasonCode, test.wantReason)
			}
		})
	}
}

func TestStaleBeforeAttemptPreservesFenceSentinels(t *testing.T) {
	t.Parallel()
	causes := []error{
		transaction.ErrAbandonedFileSetResidue,
		errors.Join(mutation.StalePlanError{}, transaction.ErrFileSetAccessUnprovable),
	}
	want := []ReasonCode{ReasonAbandonedFileSetResidue, ReasonFileSetAccessUnprovable}
	for index, cause := range causes {
		result, err := staleBeforeAttempt(CommandResult{Attempted: true}, cause)
		if err == nil {
			t.Fatal("expected refusal")
		}
		if result.ReasonCode != want[index] {
			t.Fatalf("reason = %q, want %q", result.ReasonCode, want[index])
		}
		if result.Attempted {
			t.Fatal("attempted must stay false")
		}
		joined := strings.Join(result.Remediation, "\n")
		if strings.Contains(joined, "review a new dry-run") || strings.Contains(joined, "retry the interrupted") {
			t.Fatalf("remediation = %q, want fence guidance not stale-plan retry", joined)
		}
	}
}

func assertRefreshMetadataTransactionRefusal(
	t testing.TB,
	result CommandResult,
	err error,
) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
	if result.ResultClass != "" && result.ResultClass != ResultRefused {
		t.Fatalf("result class = %q, want refused", result.ResultClass)
	}
	if result.ReasonCode != "" && result.ReasonCode != ReasonInterruptedFileSetTransaction {
		t.Fatalf("reason = %q, want %q", result.ReasonCode, ReasonInterruptedFileSetTransaction)
	}
}
