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
		{"list", "nativeconfig/invocation", []string{ruleWorkflowNestedImport}},
		{"status", "workspacecleanup", []string{ruleWorkflowNestedImport}},
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

func TestNestedWorkflowPackagesKeepTheirIdentityAcrossGuards(t *testing.T) {
	module := goListModule{Path: "example.com/tool/plugin"}
	const workflow = "internal/workflow/apply/internal/"
	report := AnalyzeReport([]PackageRecord{
		{
			Module: module, ImportPath: module.Path + "/" + workflow + "desired",
			Imports: []string{module.Path + "/internal/paths"},
		},
		{
			Module: module, ImportPath: module.Path + "/" + workflow + "operationplan",
			Imports: []string{"fmt"}, GoFiles: []string{"observe_darwin.go"},
		},
	})
	if report.HasFailures() || report.HasShadowFindings() {
		t.Fatalf("nested workflow packages were mistaken for top-level packages: %+v", report)
	}
}

func TestForeignInternalImportsAreNotLocalPackages(t *testing.T) {
	module := goListModule{Path: "example.com/tool/plugin"}
	imports := []string{"example.com/tool/internal/workflow/status", "example.com/tool/internal/cli"}
	report := AnalyzeReport([]PackageRecord{
		{Module: module, ImportPath: module.Path + "/internal/desired", Imports: imports},
		{Module: module, ImportPath: module.Path + "/internal/operationplan", Imports: imports},
	})
	if report.HasFailures() || report.HasShadowFindings() {
		t.Fatalf("foreign imports were mistaken for repository packages: %+v", report)
	}
}

func TestWorkflowReportWithoutModuleMetadata(t *testing.T) {
	const prefix = "example.com/internal/project/"
	const from, to = "internal/workflow/apply", "internal/workflow/apply/internal/other"
	report := AnalyzeReport([]PackageRecord{{ImportPath: prefix + from, Imports: []string{prefix + to}}})
	if len(report.Violations) != 1 || report.Violations[0].Rule != ruleWorkflowNestedImport ||
		report.Violations[0].PackagePath != from || report.Violations[0].ImportPath != to {
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
