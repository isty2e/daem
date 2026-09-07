package clipresent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/recoverygate"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestPrintApplyResultJSONProjectsTypedPathNeutralFailure(t *testing.T) {
	privateCause := "private_token=boundary-secret /Users/alice/private.json\n\x1b[2J\u202e"
	failure := applyworkflow.ClassifyFailure(
		errors.New(privateCause),
		applyworkflow.CommandResult{ExecutionAttempted: true},
	)
	var output bytes.Buffer
	if err := PrintApplyResultJSON(&output, ApplyResultJSONInput{
		ExecutionAttempted: true,
		Failure:            &failure,
	}); err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}

	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Errors        []struct {
			Code    applyworkflow.FailureReason  `json:"code"`
			Phase   applyworkflow.FailurePhase   `json:"phase"`
			Outcome applyworkflow.FailureOutcome `json:"outcome"`
			Message string                       `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if payload.SchemaVersion != contractversion.ApplyResultJSON || len(payload.Errors) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	got := payload.Errors[0]
	if got.Code != applyworkflow.FailureReasonApplyIncomplete ||
		got.Phase != applyworkflow.FailurePhaseExecution ||
		got.Outcome != applyworkflow.FailureOutcomeIncomplete ||
		got.Message != "apply did not complete after an effect boundary was crossed" {
		t.Fatalf("failure = %#v", got)
	}
	for _, private := range []string{
		"boundary-secret",
		"/Users/alice/private.json",
		"private_token",
		"\\u001b",
		"\\u202e",
	} {
		if strings.Contains(output.String(), private) {
			t.Fatalf("JSON contains private cause fragment %q: %s", private, output.String())
		}
	}
}

func TestPrintApplyResultJSONProjectsAbandonedFileSetResidue(t *testing.T) {
	failure := applyworkflow.ClassifyFailure(
		fmt.Errorf("private path %s: %w", "/Users/alice/.daem-tmp-secret", fileset.ErrAbandonedFileSetResidue),
		applyworkflow.CommandResult{},
	)
	var output bytes.Buffer
	if err := PrintApplyResultJSON(&output, ApplyResultJSONInput{Failure: &failure}); err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}
	var payload struct {
		Errors []struct {
			Code    applyworkflow.FailureReason `json:"code"`
			Message string                      `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Errors) != 1 ||
		payload.Errors[0].Code != applyworkflow.FailureReasonAbandonedFileSetResidue ||
		!strings.Contains(payload.Errors[0].Message, "preserve") ||
		strings.Contains(payload.Errors[0].Message, "refused before effects") ||
		strings.Contains(output.String(), "/Users/alice") {
		t.Fatalf("payload = %s", output.String())
	}
}

func TestPrintApplyResultJSONProjectsUnprovableFileSetFence(t *testing.T) {
	failure := applyworkflow.ClassifyFailure(
		fmt.Errorf("inspect file-set state dir: %w", fileset.ErrFileSetAccessUnprovable),
		applyworkflow.CommandResult{},
	)
	var output bytes.Buffer
	if err := PrintApplyResultJSON(&output, ApplyResultJSONInput{Failure: &failure}); err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}
	var payload struct {
		Errors []struct {
			Code    applyworkflow.FailureReason `json:"code"`
			Message string                      `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Errors) != 1 ||
		payload.Errors[0].Code != applyworkflow.FailureReasonFileSetAccessUnprovable ||
		!strings.Contains(payload.Errors[0].Message, "access or identity") ||
		strings.Contains(payload.Errors[0].Message, "refused before effects") {
		t.Fatalf("payload = %s", output.String())
	}
}

func TestPrintApplyResultJSONProjectsJournalCleanupReason(t *testing.T) {
	failure := applyworkflow.ClassifyFailure(
		journal.ErrIncompleteJournalCleanup,
		applyworkflow.CommandResult{},
	)
	var output bytes.Buffer
	if err := PrintApplyResultJSON(&output, ApplyResultJSONInput{Failure: &failure}); err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}
	var payload struct {
		Errors []struct {
			Code applyworkflow.FailureReason `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Errors) != 1 ||
		payload.Errors[0].Code != applyworkflow.FailureReasonJournalCleanupIncomplete {
		t.Fatalf("payload = %s", output.String())
	}
}

func TestPrintApplyResultJSONPreservesKnownFenceBesideUnknownJournal(t *testing.T) {
	failure := applyworkflow.ClassifyFailure(
		recoverygate.Combine(
			errors.New("recovery inventory inspection failed"),
			fileset.ErrAbandonedFileSetResidue,
		),
		applyworkflow.CommandResult{},
	)
	if failure.Reason() != applyworkflow.FailureReasonAbandonedFileSetResidue {
		t.Fatalf("reason = %q, want abandoned residue", failure.Reason())
	}
	var output bytes.Buffer
	if err := PrintApplyResultJSON(&output, ApplyResultJSONInput{Failure: &failure}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Errors []struct {
			Code            string `json:"code"`
			Message         string `json:"message"`
			RecoveryBarrier struct {
				Journal string `json:"journal"`
				FileSet string `json:"file_set"`
			} `json:"recovery_barrier"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Errors) != 1 ||
		payload.Errors[0].Code != string(applyworkflow.FailureReasonAbandonedFileSetResidue) ||
		payload.Errors[0].RecoveryBarrier.Journal != "unknown" ||
		payload.Errors[0].RecoveryBarrier.FileSet != "abandoned_residue" ||
		!strings.Contains(payload.Errors[0].Message, "journal recovery authority could not be classified") {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestPrintApplyResultJSONProjectsInterruptedApplyFileSetFence(t *testing.T) {
	failure := applyworkflow.ClassifyFailure(
		recoverygate.Combine(
			fmt.Errorf("%w; run: daem recover --dry-run", journal.ErrInterruptedApply),
			fileset.ErrAbandonedFileSetResidue,
		),
		applyworkflow.CommandResult{},
	)
	var output bytes.Buffer
	if err := PrintApplyResultJSON(&output, ApplyResultJSONInput{Failure: &failure}); err != nil {
		t.Fatalf("PrintApplyResultJSON returned error: %v", err)
	}
	var payload struct {
		Errors []struct {
			Code    applyworkflow.FailureReason `json:"code"`
			Message string                      `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Errors) != 1 ||
		payload.Errors[0].Code != applyworkflow.FailureReasonInterruptedApplyFileSetFence ||
		!strings.Contains(payload.Errors[0].Message, "run daem recover") ||
		!strings.Contains(payload.Errors[0].Message, "file-set fence remains") ||
		strings.Contains(payload.Errors[0].Message, "refused before effects") {
		t.Fatalf("payload = %s", output.String())
	}
}
