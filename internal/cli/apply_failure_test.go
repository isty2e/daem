package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/encoding/toml"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestApplyFailureDetailPreservesCancellationAcrossPreEffectStages(t *testing.T) {
	for _, stage := range []applyFailureStage{
		applyFailureProjection,
		applyFailureDiff,
		applyFailureConfirmation,
		applyFailureOutput,
	} {
		diagnostic, reason := applyFailurePrimary(
			stage,
			context.Canceled,
			applyworkflow.CommandResult{},
		)
		detail := diagnostic.HumanText()
		if detail != "apply was cancelled before effects" ||
			reason != applyworkflow.FailureReasonCancelled {
			t.Fatalf("stage %q = %q/%q, want pre-effect cancellation", stage, detail, reason)
		}
	}
}

func TestApplyFailureDetailPreservesPostEffectDeadline(t *testing.T) {
	diagnostic, reason := applyFailurePrimary(
		applyFailureDiff,
		context.DeadlineExceeded,
		applyworkflow.CommandResult{ExecutionAttempted: true},
	)
	detail := diagnostic.HumanText()
	if detail != "apply was cancelled after an effect boundary was crossed" ||
		reason != applyworkflow.FailureReasonCancelled {
		t.Fatalf("detail/reason = %q/%q, want post-effect cancellation", detail, reason)
	}
}

func TestApplyFailureRejectsUnownedInputLookalikes(t *testing.T) {
	positioned := tomlsyntax.Admit(context.Background(), []byte("private=[\n"), tomlsyntax.StandardLimits())
	if positioned == nil {
		t.Fatal("fixture did not produce a positioned error")
	}
	for _, err := range []error{
		errors.New("invalid manifest: /private/credential=secret"),
		fmt.Errorf("internal /private/credential=secret: %w", positioned),
		fmt.Errorf("%w: /private/credential=secret", applyworkflow.ErrReadLockfile),
	} {
		for _, stage := range []applyFailureStage{applyFailurePlanning, applyFailureExecution} {
			result := applyworkflow.CommandResult{}
			failure := applyworkflow.ClassifyFailure(err, result)
			diagnostic, reason := applyFailurePrimary(stage, err, result)
			detail := diagnostic.HumanText()
			if detail != failure.Detail() || reason != failure.Reason() {
				t.Fatalf("unowned error escaped its closed projection: %v -> %q/%q", err, detail, reason)
			}
			var output bytes.Buffer
			printApplyFailure(&output, err, stage, result, false)
			printApplyWorkflowHints(&output, "/private/manifest.toml", result, err)
			if strings.Contains(output.String(), "/private/") || strings.Contains(output.String(), "credential=secret") {
				t.Fatalf("unknown internal evidence disclosed: %q", &output)
			}
		}
	}
}

func TestApplyInputDiagnosticPreservesHigherPriorityFailures(t *testing.T) {
	root := isolatedApplyAuthorityRoot(t)
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version=1\ntargets=[\"codex\"]\nbogus=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, inputErr := applyworkflow.PlanDryRun(context.Background(), applyworkflow.CommandInput{ManifestPath: manifestPath})
	if inputErr == nil {
		t.Fatal("fixture did not fail input loading")
	}
	if _, owned := applyworkflow.InputErrorCause(inputErr); !owned {
		t.Fatal("selected input did not retain provenance")
	}
	for _, err := range []error{
		errors.Join(inputErr, fileset.ErrInterruptedFileSetTransaction),
		errors.Join(fileset.ErrInterruptedFileSetTransaction, inputErr),
	} {
		diagnostic, reason := applyFailurePrimary(applyFailurePlanning, err, plan.CommandResult)
		detail := diagnostic.HumanText()
		failure := applyworkflow.ClassifyFailure(err, plan.CommandResult)
		if detail != failure.PrimaryDetail() || reason != applyworkflow.FailureReasonInterruptedFileSetTransaction {
			t.Fatalf("input overrode recovery fence: %q/%q", detail, reason)
		}
		var hints bytes.Buffer
		printApplyWorkflowHints(&hints, manifestPath, plan.CommandResult, err)
		if hints.Len() != 0 {
			t.Fatalf("fenced input gained ordinary repair hints: %q", &hints)
		}
	}
	for _, err := range []error{errors.Join(inputErr, context.Canceled), errors.Join(context.Canceled, inputErr)} {
		diagnostic, reason := applyFailurePrimary(applyFailurePlanning, err, plan.CommandResult)
		detail := diagnostic.HumanText()
		failure := applyworkflow.ClassifyFailure(err, plan.CommandResult)
		if detail != failure.Detail() || reason != applyworkflow.FailureReasonCancelled {
			t.Fatalf("input overrode cancellation: %q/%q", detail, reason)
		}
		var hints bytes.Buffer
		printApplyWorkflowHints(&hints, manifestPath, plan.CommandResult, err)
		if hints.Len() != 0 {
			t.Fatalf("cancelled input gained repair hints: %q", &hints)
		}
	}
	for _, stage := range []applyFailureStage{applyFailurePlanning, applyFailureExecution} {
		result := applyworkflow.CommandResult{ExecutionAttempted: true}
		diagnostic, reason := applyFailurePrimary(stage, inputErr, result)
		detail := diagnostic.HumanText()
		failure := applyworkflow.ClassifyFailure(inputErr, result)
		if detail != failure.Detail() || reason != failure.Reason() {
			t.Fatalf("input replaced post-effect outcome: %q/%q", detail, reason)
		}
	}
	cause, _ := applyworkflow.InputErrorCause(inputErr)
	before := applyworkflow.ClassifyFailure(cause, plan.CommandResult)
	after := applyworkflow.ClassifyFailure(inputErr, plan.CommandResult)
	var beforeJSON, afterJSON bytes.Buffer
	if err := clipresent.PrintApplyResultJSON(&beforeJSON, clipresent.ApplyResultJSONInput{Failure: &before}); err != nil {
		t.Fatal(err)
	}
	if err := clipresent.PrintApplyResultJSON(&afterJSON, clipresent.ApplyResultJSONInput{Failure: &after}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeJSON.Bytes(), afterJSON.Bytes()) || strings.Contains(afterJSON.String(), manifestPath) {
		t.Fatalf("marker changed path-neutral JSON: before=%q after=%q", &beforeJSON, &afterJSON)
	}
}
