package clipresent

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
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
