package clipresent

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPrintPlanJSONPreservesExplicitCheckCleanValues(t *testing.T) {
	for _, clean := range []bool{false, true} {
		for _, mode := range []string{"status", "check"} {
			var output bytes.Buffer
			if err := PrintPlanJSON(&output, PlanJSONInput{
				Command:    "status",
				Mode:       mode,
				CheckClean: &clean,
			}); err != nil {
				t.Fatal(err)
			}

			var report struct {
				SchemaVersion int   `json:"schema_version"`
				CheckClean    *bool `json:"check_clean"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.SchemaVersion != 16 || report.CheckClean == nil || *report.CheckClean != clean {
				t.Fatalf("mode=%s schema_version=%d check_clean=%v, want explicit %t in version16", mode, report.SchemaVersion, report.CheckClean, clean)
			}
		}
	}
}

func TestPrintPlanJSONRejectsMissingOrInapplicableCheckAssessmentBeforeWriting(t *testing.T) {
	tests := []struct {
		name  string
		input PlanJSONInput
	}{
		{name: "status without assessment", input: PlanJSONInput{Command: "status", Mode: "status"}},
		{name: "check without assessment", input: PlanJSONInput{Command: "status", Mode: "check"}},
		{
			name: "apply dry-run with status assessment",
			input: PlanJSONInput{
				Command:    "apply",
				Mode:       "dry-run",
				CheckClean: planTestCheckAssessment(false),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := PrintPlanJSON(&output, test.input); err == nil {
				t.Fatal("accepted missing or inapplicable status assessment")
			}
			if output.Len() != 0 {
				t.Fatalf("invalid assessment emitted an ordinary result: %q", output.String())
			}
		})
	}
}

func planTestCheckAssessment(clean bool) *bool {
	return &clean
}
