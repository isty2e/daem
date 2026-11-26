package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/fileset"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/toml"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestApplyReportsSELinuxRefusalWithoutDisclosingDestinationInJSON(t *testing.T) {
	const privateDir = "private-label-fixture"
	const raw = "private-kernel-fixture"
	denial := fmt.Errorf("%s: %w", raw, os.ErrPermission)
	forward := &storagecommit.SELinuxLabelError{Destination: filepath.Join(privateDir, `quoted"dir`, "AGENTS.md"), Err: denial}
	restore := &storagecommit.SELinuxLabelError{Destination: filepath.Join(privateDir, `quoted"dir`, "settings.json"), Err: denial}
	rollback := applyprogram.RollbackFailure{Cause: restore}

	for _, tc := range []struct {
		name              string
		err               error
		forward, rollback bool
		reason            applyworkflow.FailureReason
		outcome           applyworkflow.FailureOutcome
	}{
		{"forward", forward, true, false, applyworkflow.FailureReasonApplyIncomplete, applyworkflow.FailureOutcomeIncomplete},
		{"restored", applyprogram.NewRollbackError(forward, nil), true, false, applyworkflow.FailureReasonApplyIncomplete, applyworkflow.FailureOutcomeRolledBack},
		{"rollback refused", errors.Join(errors.New("state commit failed"), rollback), false, true, applyworkflow.FailureReasonApplyIncomplete, applyworkflow.FailureOutcomeIncomplete},
		{"restore cause also unwrapped", errors.Join(restore, rollback), false, true, applyworkflow.FailureReasonApplyIncomplete, applyworkflow.FailureOutcomeIncomplete},
		{"both writes refused", errors.Join(forward, rollback), true, true, applyworkflow.FailureReasonApplyIncomplete, applyworkflow.FailureOutcomeIncomplete},
		{"cancelled", errors.Join(forward, context.Canceled), false, false, applyworkflow.FailureReasonCancelled, applyworkflow.FailureOutcomeIncomplete},
		{"rollback after cancellation", errors.Join(context.Canceled, rollback), false, true, applyworkflow.FailureReasonCancelled, applyworkflow.FailureOutcomeIncomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := applyworkflow.CommandResult{ExecutionAttempted: true}
			var human, wire bytes.Buffer
			printApplyFailure(&human, tc.err, applyFailureExecution, result, false)
			failure := applyworkflow.ClassifyFailure(tc.err, result)
			if err := clipresent.PrintApplyResultJSON(&wire, clipresent.ApplyResultJSONInput{ExecutionAttempted: true, Failure: &failure}); err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Errors []struct {
					Code    applyworkflow.FailureReason
					Phase   applyworkflow.FailurePhase
					Outcome applyworkflow.FailureOutcome
					Message string
				}
			}
			if err := json.Unmarshal(wire.Bytes(), &payload); err != nil || len(payload.Errors) != 1 {
				t.Fatalf("expected one JSON error, got %s (%v)", &wire, err)
			}
			entry := payload.Errors[0]
			if entry.Code != tc.reason || entry.Phase != applyworkflow.FailurePhaseExecution || entry.Outcome != tc.outcome || strings.Contains(wire.String(), privateDir) || strings.Contains(wire.String(), raw) {
				t.Fatal("wrong classification or private error evidence in JSON:", &wire)
			}
			for _, text := range []string{entry.Message, human.String()} {
				primary, secondary, rollbackReported := strings.Cut(text, "rollback failed:")
				if strings.Contains(primary, forward.Summary()) != tc.forward || rollbackReported != tc.rollback || strings.Contains(secondary, restore.Summary()) != tc.rollback {
					t.Fatalf("wrong cause for forward or rollback failure: %s", text)
				}
			}
			forwardNote := clipresent.NoteGuidance("affected file: " + clipresent.Quote(forward.Destination)).HumanText()
			restoreNote := clipresent.NoteGuidance("rollback file: " + clipresent.Quote(restore.Destination)).HumanText()
			if strings.Contains(human.String(), forwardNote) != tc.forward || strings.Contains(human.String(), restoreNote) != tc.rollback || strings.Contains(human.String(), "daem recover --dry-run") != tc.rollback || strings.Contains(human.String(), raw) {
				t.Fatal("wrong file or recovery guidance:", &human)
			}
		})
	}
}

func TestNativeProtectionDiagnosticsAreActionableOnlyInPlanning(t *testing.T) {
	for _, unverified := range []bool{false, true} {
		err, privatePath := nativeProtectionFailure(t, unverified, ".codex/auth.json")
		var protection *journalnamespace.ProtectionError
		if !errors.As(err, &protection) {
			t.Fatal("fixture did not reach native protection", err)
		}
		diagnostic, reason := applyFailurePrimary(applyFailurePlanning, err, applyworkflow.CommandResult{}, false)
		if reason != applyworkflow.FailureReasonApplyRefused || diagnostic.Message() != protection.Error() || len(diagnostic.Guidance()) != 1 || diagnostic.Guidance()[0].Kind() != clipresent.GuidanceInstruction {
			t.Fatal("planning hid the native cause or omitted repair guidance", diagnostic.HumanText(), reason)
		}
		if unverified && (!strings.Contains(diagnostic.Message(), "CODEX_HOME") || errors.Is(err, configuration.ErrReserved)) {
			t.Fatal("unverified location became a reserved hit or lost its selector source", diagnostic.HumanText())
		}
		var wire bytes.Buffer
		if err := clipresent.PrintCLIErrorJSON(&wire, "apply", clipresent.CLIErrorOperation, diagnostic); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(wire.String(), privatePath) || strings.Contains(wire.String(), "private-fixture-value") || strings.Contains(diagnostic.HumanText(), "force") {
			t.Fatal("default protection diagnostic disclosed private evidence or a bypass", wire.String())
		}
		for _, sample := range []struct {
			stage  applyFailureStage
			cause  error
			result applyworkflow.CommandResult
		}{
			{applyFailurePlanning, errors.Join(err, context.Canceled), applyworkflow.CommandResult{}},
			{applyFailurePlanning, errors.Join(err, fileset.ErrInterruptedFileSetTransaction), applyworkflow.CommandResult{}},
			{applyFailureExecution, err, applyworkflow.CommandResult{ExecutionAttempted: true}},
			{applyFailurePlanning, err, applyworkflow.CommandResult{ExecutionAttempted: true}},
		} {
			actual, reason := applyFailurePrimary(sample.stage, sample.cause, sample.result, false)
			owned := applyworkflow.ClassifyFailure(sample.cause, sample.result)
			if actual.Message() != owned.PrimaryDetail() || reason != owned.Reason() || len(actual.Guidance()) != 0 {
				t.Fatal("protection cause overrode the workflow's failure disposition", actual.HumanText(), reason)
			}
		}
		wrapped := fmt.Errorf("recovery write failed and current durable authority could not be classified: %w", err)
		if generic := clipresent.FailureDiagnosticFromError(wrapped); generic.Message() != wrapped.Error() || len(generic.Guidance()) != 0 {
			t.Fatal("generic rendering promoted a nested protection cause", generic.HumanText())
		}
		planning := clipresent.RecoverPlanningDiagnostic(err, clipresent.HumanOptions{})
		if planning.Message() != "recover failed: "+err.Error() || len(planning.Guidance()) != 1 {
			t.Fatal("recovery planning discarded its cause or repair", planning.HumanText())
		}
		if strings.Contains(planning.Guidance()[0].Message(), "remove the declaration") || !strings.Contains(planning.Guidance()[0].Message(), "journal") {
			t.Fatal("recovery gave declaration-editing advice for frozen journal aliases", planning.HumanText())
		}
	}
}

func nativeProtectionFailure(t *testing.T, unverified bool, path string) (error, string) {
	t.Helper()
	base := t.TempDir()
	budget := physicalwork.NewCaptureBudget(nil)
	root, err := rootedpath.CaptureCanonicalRootBounded(base, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	project, _ := configuration.ProjectRootReference()
	roots, err := (journalnamespace.Roots{}).With(project, journalnamespace.RootSelection{SelectedPath: base, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	var selectors []journalnamespace.ReservationSelector
	for _, host := range []target.Target{target.TargetPi, target.TargetCodex} {
		reference, _ := configuration.RootReferenceForScope(host, target.ScopeGlobal)
		var selector journalnamespace.ReservationSelector
		if unverified && host == target.TargetCodex {
			selector, err = journalnamespace.UnavailableReservationSelector(reference, "CODEX_HOME", errors.New(base+"/private-fixture-value"))
		} else {
			name := ".pi/agent"
			if host == target.TargetCodex {
				name = ".codex"
			}
			selector, err = journalnamespace.NewReservationSelector(reference, filepath.Join(base, name))
		}
		if err != nil {
			t.Fatal(err)
		}
		selectors = append(selectors, selector)
	}
	protection, err := journalnamespace.NewReservationSelectors(selectors)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	alias, err := aggregate.NewDocumentAddress(target.TargetCodex, target.ScopeProject, destination)
	if err != nil {
		t.Fatal(err)
	}
	_, err = journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, protection, storagecommit.Adapter{}, budget)
	return err, base
}

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
			false,
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
		false,
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
			diagnostic, reason := applyFailurePrimary(stage, err, result, false)
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
		diagnostic, reason := applyFailurePrimary(applyFailurePlanning, err, plan.CommandResult, false)
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
		diagnostic, reason := applyFailurePrimary(applyFailurePlanning, err, plan.CommandResult, false)
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
		diagnostic, reason := applyFailurePrimary(stage, inputErr, result, false)
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

func TestApplyStaleAgentConfigDiagnosticIsPlanningOnly(t *testing.T) {
	declaration, err := entity.New(entity.KindAgentConfig, "preferences")
	if err != nil {
		t.Fatal(err)
	}
	stale := lockobserve.NativeSourceStaleError{DeclarationID: declaration}
	result := applyworkflow.CommandResult{}

	planning := applyFailureDiagnostic("", stale, applyFailurePlanning, result, false)
	if !strings.Contains(planning.Message(), "agent-config/preferences") || len(planning.Guidance()) != 1 {
		t.Fatal("the fixture did not receive the planning diagnosis", planning.HumanText())
	}
	execution := applyFailureDiagnostic("", stale, applyFailureExecution, result, false)
	if execution.Message() != applyworkflow.ClassifyFailure(stale, result).PrimaryDetail() || len(execution.Guidance()) != 0 {
		t.Fatal("an execution-stage check gained relock advice before effects", execution.HumanText())
	}

	protection, _ := nativeProtectionFailure(t, false, ".codex/auth.json")
	protected := applyFailureDiagnostic("", protection, applyFailurePlanning, result, false)
	combined := applyFailureDiagnostic("", errors.Join(stale, protection), applyFailurePlanning, result, false)
	if combined.HumanText() != protected.HumanText() {
		t.Fatal("stale-source advice displaced a protection refusal", combined.HumanText())
	}
}
