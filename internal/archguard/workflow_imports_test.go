package archguard

import (
	"fmt"
	"slices"
	"testing"
)

func TestWorkflowImportReport(t *testing.T) {
	const module = "example.com/internal/project"
	const root = "internal/workflow/"
	for _, tc := range []struct {
		from, to string
		rules    []string
	}{
		{"list", "readiness", nil},
		{"list", "nativeconfig", nil},
		{"list", "status", []string{ruleWorkflowNestedImport}},
		{"readiness", "status", []string{ruleWorkflowNestedImport}},
		{"list", "protection/recovery", []string{ruleWorkflowNestedImport}},
		{"authoring", "workspacecleanup", []string{ruleWorkflowNestedImport}},
		{"workspacecleanup", "readiness", []string{ruleWorkflowNestedImport}},
		{"apply/internal/other", "status", []string{ruleWorkflowNestedImport}},
		{"apply", "apply/internal/other", []string{ruleWorkflowNestedImport}},
		{"lock/generate", "nativeconfig", []string{ruleLockGenerateBoundaryImport, ruleWorkflowNestedImport}},
	} {
		t.Run(tc.from+"->"+tc.to, func(t *testing.T) {
			from, to := root+tc.from, root+tc.to
			data := fmt.Sprintf(`{"Module":{"Path":%q},"ImportPath":%q,"Imports":[%q]}`, module, module+"/"+from, module+"/"+to)
			report, err := AnalyzeGoListReport([]byte(data))
			if err != nil {
				t.Fatal(err)
			}
			var rules []string
			for _, finding := range report.Violations {
				if finding.PackagePath != from || finding.ImportPath != to {
					t.Fatalf("unexpected endpoints: %+v", finding)
				}
				rules = append(rules, finding.Rule)
			}
			slices.Sort(rules)
			if !slices.Equal(rules, tc.rules) {
				t.Errorf("blocking rules = %v, want %v", rules, tc.rules)
			}
		})
	}
}

func TestUnclassifiedWorkflowWithoutWorkflowImports(t *testing.T) {
	report := AnalyzeReport([]PackageRecord{{
		Module:     goListModule{Path: "example.com/tool/plugin"},
		ImportPath: "example.com/tool/plugin/internal/workflow/apply/other",
		Imports:    []string{"fmt", "example.com/tool/internal/workflow/status"},
	}})
	if report.HasFailures() {
		t.Fatal(FormatAnalysisReport(report))
	}
}

func TestWorkflowRoleGrants(t *testing.T) {
	services := []string{"nativeconfig", "readiness", "nativeconfig/invocation", "protection/invocation", "lock/generate", "workspacecleanup", "protection/recovery"}
	for _, tc := range []struct {
		role workflowRoles
		want []string
	}{
		{nativeInputs, []string{"nativeconfig"}},
		{workspaceAssessment, []string{"nativeconfig", "readiness"}},
		{forwardInvocation, []string{"nativeconfig/invocation", "protection/invocation"}},
		{lockGeneration, []string{"lock/generate"}},
		{workspaceCleanup, []string{"workspacecleanup"}},
		{protectionRecovery, []string{"protection/recovery"}},
	} {
		for _, service := range services {
			if got := tc.role.allows(service); got != slices.Contains(tc.want, service) {
				t.Errorf("role %d allows %s = %t, want services %v", tc.role, service, got, tc.want)
			}
		}
	}
}
