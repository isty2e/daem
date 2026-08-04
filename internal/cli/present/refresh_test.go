package clipresent

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

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
