package archguard

import (
	"strings"
	"testing"
)

func TestAnalyzeRecordsReportsPathsNonStdlibImports(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/paths",
			Imports: []string{
				"fmt",
				"net/http",
				"example/module",
				"github.com/owner/module",
				"example.com/project/internal/target",
			},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"paths-internal-import: internal/paths -> example/module",
		"paths-internal-import: internal/paths -> github.com/owner/module",
		"paths-internal-import: internal/paths -> internal/target",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
	for _, unwanted := range []string{
		"internal/paths -> fmt",
		"internal/paths -> net/http",
	} {
		if strings.Contains(report, unwanted) {
			t.Fatalf("report = %q, did not want stdlib import %q reported", report, unwanted)
		}
	}
}

func TestAnalyzeRecordsReportsForbiddenPathShapes(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/internal/resource/skill/compat"},
		{ImportPath: "example.com/project/internal/resource/skill/repair"},
		{ImportPath: "example.com/project/internal/resource/hook/render"},
		{ImportPath: "example.com/project/internal/resource/hook/import"},
		{ImportPath: "example.com/project/internal/resource/hook/doctor"},
		{ImportPath: "example.com/project/internal/resource/hook/apply"},
		{ImportPath: "example.com/project/internal/mcp"},
		{ImportPath: "example.com/project/internal/plugin/marketplace"},
		{ImportPath: "example.com/project/internal/workflow/app"},
		{ImportPath: "example.com/project/internal/app/serviceapi"},
		{ImportPath: "example.com/project/internal/foo/manager"},
		{
			ImportPath:  "example.com/project/internal/workflow/authoring",
			GoFiles:     []string{"skill_add.go", "skill_lock.go", "ordinary.go", "handler.go", "processor.go"},
			TestGoFiles: []string{"hook_render_test.go", "instruction_import_test.go"},
		},
		{
			ImportPath: "example.com/project/internal/app",
			GoFiles:    []string{"service.go"},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"forbidden-resource-operation-cell: internal/resource/skill/compat",
		"forbidden-resource-operation-cell: internal/resource/skill/repair",
		"forbidden-resource-operation-cell: internal/resource/hook/render",
		"forbidden-resource-operation-cell: internal/resource/hook/import",
		"forbidden-resource-operation-cell: internal/resource/hook/doctor",
		"forbidden-resource-operation-cell: internal/resource/hook/apply",
		"future-mcp-plugin-monolith: internal/mcp",
		"future-mcp-plugin-monolith: internal/plugin/marketplace",
		"forbidden-generic-role-shape: internal/workflow/app",
		"forbidden-generic-role-shape: internal/app/serviceapi",
		"forbidden-generic-role-shape: internal/foo/manager",
		"forbidden-generic-role-shape: internal/workflow/authoring/handler.go",
		"forbidden-generic-role-shape: internal/workflow/authoring/processor.go",
		"forbidden-generic-role-shape: internal/app/service.go",
		"forbidden-resource-operation-cell: internal/workflow/authoring/skill_add.go",
		"forbidden-resource-operation-cell: internal/workflow/authoring/skill_lock.go",
		"forbidden-resource-operation-cell: internal/workflow/authoring/hook_render_test.go",
		"forbidden-resource-operation-cell: internal/workflow/authoring/instruction_import_test.go",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}

func TestAnalyzeRecordsAllowsResourceFamilyPackagesUnderPhaseLocalRoots(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/internal/desired/skill"},
		{ImportPath: "example.com/project/internal/desired/hook"},
		{ImportPath: "example.com/project/internal/desired/instructions"},
		{ImportPath: "example.com/project/internal/resource/hook"},
		{ImportPath: "example.com/project/internal/supply/compat/skill/repair"},
		{ImportPath: "example.com/project/internal/output/project/instructions"},
		{ImportPath: "example.com/project/internal/effect/payload/skill"},
		{ImportPath: "example.com/project/internal/adopt/hook"},
		{ImportPath: "example.com/project/internal/realization/aggregate/codec/hook"},
		{ImportPath: "example.com/project/internal/topology/hook"},
	}

	report := FormatReport(AnalyzeRecords(records))
	if strings.Contains(report, ruleForbiddenResourceOperation) {
		t.Fatalf("report = %q, did not want allowed resource-family phase roots reported", report)
	}
}

func TestAnalyzeRecordsReportsResourceFamilyPackagesOutsidePhaseLocalRoots(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/internal/workflow/skill"},
		{ImportPath: "example.com/project/internal/diagnose/hook"},
		{ImportPath: "example.com/project/internal/realization/lock/build/instructions"},
		{ImportPath: "example.com/project/internal/cli/skillgroup"},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"forbidden-resource-operation-cell: internal/workflow/skill",
		"forbidden-resource-operation-cell: internal/diagnose/hook",
		"forbidden-resource-operation-cell: internal/realization/lock/build/instructions",
		"forbidden-resource-operation-cell: internal/cli/skillgroup",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}
