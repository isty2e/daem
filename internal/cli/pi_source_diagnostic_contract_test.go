package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/diagnose"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	statusworkflow "github.com/isty2e/daem/internal/workflow/status"
)

func TestPiSourceWarningsDoNotChangeStatusChecksOrErrorFlags(t *testing.T) {
	selection, err := targetselection.ForDiagnostics([]string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		source, err := desiredextension.NewAuthoredSourceRef(desiredextension.SourceKindHostSource,
			"git+https://router.home.arpa/acme/PI_PRIVATE_MARKER.git#v1")
		if err != nil {
			t.Fatal(err)
		}
		value, err := desiredextension.New(desiredextension.Spec{
			Name: "source-example", Carrier: desiredextension.CarrierPiPackage,
			Target: target.TargetPi, Scope: scope, Source: source,
		})
		if err != nil {
			t.Fatal(err)
		}
		diagnostics, err := diagnose.PiPackageSourceDiagnostics([]desiredextension.Extension{value}, selection)
		if err != nil || len(diagnostics) != 1 {
			t.Fatalf("diagnostics=%#v error=%v", diagnostics, err)
		}
		for _, base := range []statusworkflow.CommandResult{
			{},
			{LockfileMissing: true},
			{Reconciliation: statusPendingManagedPathPlan(t)},
		} {
			warned := base
			warned.Diagnostics = diagnostics
			if warned.CheckClean() != base.CheckClean() {
				t.Fatal("warning changed check_clean")
			}
			for _, check := range []bool{false, true} {
				if statusCheckExitCode(warned.CheckClean(), check) != statusCheckExitCode(base.CheckClean(), check) {
					t.Fatal("warning changed status exit code")
				}
			}
			var before, after struct {
				HasErrors  bool  `json:"has_errors"`
				CheckClean *bool `json:"check_clean"`
			}
			for index, result := range []statusworkflow.CommandResult{base, warned} {
				var output bytes.Buffer
				clean := result.CheckClean()
				if err := clipresent.PrintPlanJSON(&output, clipresent.PlanJSONInput{
					Command: "status", Mode: "inspect", LockfileMissing: result.LockfileMissing,
					CheckClean: &clean, Reconciliation: result.Reconciliation, Diagnostics: result.Diagnostics,
				}); err != nil {
					t.Fatal(err)
				}
				destination := &before
				if index == 1 {
					destination = &after
				}
				if err := json.Unmarshal(output.Bytes(), destination); err != nil {
					t.Fatal(err)
				}
			}
			if before.HasErrors != after.HasErrors || before.CheckClean == nil || after.CheckClean == nil || *before.CheckClean != *after.CheckClean {
				t.Fatalf("warning changed JSON error/check flags: before=%#v after=%#v", before, after)
			}
		}
		for _, verbose := range []bool{false, true} {
			var human bytes.Buffer
			clipresent.PrintDiagnosticsWithOptions(&human, diagnostics, clipresent.HumanOptions{Verbose: verbose})
			if !strings.Contains(human.String(), "source-example") || strings.Contains(human.String(), "PI_PRIVATE_MARKER") ||
				strings.Contains(human.String(), "router.home.arpa") {
				t.Fatalf("human warning violated source disclosure: %q", human.String())
			}
		}
	}
}
