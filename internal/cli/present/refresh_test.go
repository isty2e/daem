package clipresent

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/subprocess"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

func TestRefreshJSONPreservesKnownFenceBesideUnknownJournal(t *testing.T) {
	cause := recoverygate.Combine(
		errors.New("recovery inventory inspection failed"),
		fileset.ErrAbandonedFileSetResidue,
	)
	result := refreshworkflow.CommandResult{
		Mode:            refreshworkflow.ModeExecute,
		ResultClass:     refreshworkflow.ResultRefused,
		ReasonCode:      refreshworkflow.ReasonAbandonedFileSetResidue,
		RecoveryBarrier: recoverygate.StateOf(cause),
	}
	report := RefreshReportFrom(result)
	if report.Result.RecoveryBarrier == nil ||
		report.Result.RecoveryBarrier.Journal != "unknown" ||
		report.Result.RecoveryBarrier.FileSet != "abandoned_residue" {
		t.Fatalf("recovery barrier = %#v", report.Result.RecoveryBarrier)
	}
}

func TestRefreshJSONPreservesFrozenNestedShapeAndEmptyArrays(t *testing.T) {
	report := RefreshReportFrom(refreshworkflow.CommandResult{
		Mode:        refreshworkflow.ModeDryRun,
		ResultClass: refreshworkflow.ResultPlanned,
	})
	var output bytes.Buffer
	if err := PrintRefreshJSON(&output, report); err != nil {
		t.Fatalf("PrintRefreshJSON returned error: %v", err)
	}

	var document map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("Unmarshal document: %v", err)
	}
	assertJSONKeys(t, document, []string{
		"command",
		"disclosure",
		"has_errors",
		"mode",
		"result",
		"route",
		"schema_version",
		"selection",
	})
	assertRawObjectKeys(t, document["selection"], []string{
		"carrier_family",
		"id",
		"scope",
		"target",
	})
	assertRawObjectKeys(t, document["route"], []string{
		"adapter_contract_version",
		"execution_subject",
		"observation_posture",
		"operation",
		"request_hash",
		"route_id",
	})
	assertRawObjectKeys(t, document["disclosure"], []string{
		"args",
		"command",
		"cwd_policy",
		"effect_classes",
		"env_names",
		"invocation_kind",
		"non_claims",
		"retained_effect_classes",
		"timeout_seconds",
	})
	assertRawObjectKeys(t, document["result"], []string{
		"attempt_history",
		"attempted",
		"class",
		"detail",
		"remediation",
	})

	var disclosure struct {
		Args                  []string `json:"args"`
		EnvNames              []string `json:"env_names"`
		EffectClasses         []string `json:"effect_classes"`
		RetainedEffectClasses []string `json:"retained_effect_classes"`
		NonClaims             []string `json:"non_claims"`
	}
	if err := json.Unmarshal(document["disclosure"], &disclosure); err != nil {
		t.Fatalf("Unmarshal disclosure: %v", err)
	}
	for name, values := range map[string][]string{
		"args":                    disclosure.Args,
		"env_names":               disclosure.EnvNames,
		"effect_classes":          disclosure.EffectClasses,
		"retained_effect_classes": disclosure.RetainedEffectClasses,
		"non_claims":              disclosure.NonClaims,
	} {
		if values == nil {
			t.Errorf("%s encoded as null, want []", name)
		}
	}
	var result struct {
		Detail      string   `json:"detail"`
		Remediation []string `json:"remediation"`
	}
	if err := json.Unmarshal(document["result"], &result); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	if result.Remediation == nil {
		t.Error("remediation encoded as null, want []")
	}
	if result.Detail != "" {
		t.Fatalf("successful detail = %q, want empty", result.Detail)
	}
}

func TestRefreshJSONProjectsCleanupOnlyContinuingFenceReason(t *testing.T) {
	report := RefreshReportFrom(refreshworkflow.CommandResult{
		Mode:        refreshworkflow.ModeDryRun,
		ResultClass: refreshworkflow.ResultRefused,
		ReasonCode:  refreshworkflow.ReasonJournalCleanupFileSetFence,
		Remediation: []string{"run daem recover --dry-run"},
	})
	var output bytes.Buffer
	if err := PrintRefreshJSON(&output, report); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Result struct {
			ReasonCode string `json:"reason_code"`
			Detail     string `json:"detail"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Result.ReasonCode != string(refreshworkflow.ReasonJournalCleanupFileSetFence) ||
		payload.Result.Detail != "journal cleanup is incomplete; run daem recover first; the file-set fence remains after recover" {
		t.Fatalf("payload = %s", output.String())
	}
}

func TestRefreshJSONKeepsProcessAndAuthorityOutcomesSeparate(t *testing.T) {
	report := RefreshReportFrom(refreshworkflow.CommandResult{
		Mode:        refreshworkflow.ModeExecute,
		ResultClass: refreshworkflow.ResultPartial,
		ReasonCode:  refreshworkflow.ReasonMutationAuthority,
		Attempted:   true,
		ProcessOutcome: &refreshworkflow.ProcessOutcome{
			Started:  true,
			Reason:   subprocess.CommandReasonTimeout,
			TimedOut: true,
		},
		AuthorityOutcome: &refreshworkflow.AuthorityOutcome{WorkDirFailed: true},
	})
	var output bytes.Buffer
	if err := PrintRefreshJSON(&output, report); err != nil {
		t.Fatalf("PrintRefreshJSON returned error: %v", err)
	}
	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Result        struct {
			ProcessOutcome struct {
				Reason   string `json:"reason"`
				TimedOut bool   `json:"timed_out"`
			} `json:"process_outcome"`
			AuthorityOutcome struct {
				WorkDirFailed bool `json:"workdir_failed"`
			} `json:"authority_outcome"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal refresh result: %v", err)
	}
	if payload.Result.ProcessOutcome.Reason != "timeout" ||
		!payload.Result.ProcessOutcome.TimedOut ||
		!payload.Result.AuthorityOutcome.WorkDirFailed {
		t.Fatalf("refresh outcomes = process %#v, authority %#v", payload.Result.ProcessOutcome, payload.Result.AuthorityOutcome)
	}
}

func assertRawObjectKeys(
	t *testing.T,
	raw json.RawMessage,
	want []string,
) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("Unmarshal nested object: %v", err)
	}
	assertJSONKeys(t, object, want)
}

func assertJSONKeys(
	t *testing.T,
	object map[string]json.RawMessage,
	want []string,
) {
	t.Helper()
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, want) {
		t.Fatalf("JSON keys = %v, want %v", keys, want)
	}
}
