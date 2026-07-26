package archguard

import (
	"strings"
	"testing"
)

func TestAnalyzeRecordsReportsFutureFamilyTopLevelMonoliths(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/internal/mcp"},
		{ImportPath: "example.com/project/internal/mcp/server"},
		{ImportPath: "example.com/project/internal/plugin"},
		{ImportPath: "example.com/project/internal/plugin/marketplace"},
		{ImportPath: "example.com/project/internal/package"},
		{ImportPath: "example.com/project/internal/package/resolution"},
		{ImportPath: "example.com/project/internal/packages"},
		{ImportPath: "example.com/project/internal/packages/catalog"},
		{ImportPath: "example.com/project/internal/extension"},
		{ImportPath: "example.com/project/internal/extension/catalog"},
		{ImportPath: "example.com/project/internal/extensions"},
		{ImportPath: "example.com/project/internal/extensions/catalog"},
		{ImportPath: "example.com/tools/internal/fixture/internal/plugin"},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"future-mcp-plugin-monolith: internal/mcp",
		"future-mcp-plugin-monolith: internal/mcp/server",
		"future-mcp-plugin-monolith: internal/plugin",
		"future-mcp-plugin-monolith: internal/plugin/marketplace",
		"future-mcp-plugin-monolith: internal/package",
		"future-mcp-plugin-monolith: internal/package/resolution",
		"future-mcp-plugin-monolith: internal/packages",
		"future-mcp-plugin-monolith: internal/packages/catalog",
		"future-mcp-plugin-monolith: internal/extension",
		"future-mcp-plugin-monolith: internal/extension/catalog",
		"future-mcp-plugin-monolith: internal/extensions",
		"future-mcp-plugin-monolith: internal/extensions/catalog",
		"future-mcp-plugin-monolith: internal/plugin",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}

func TestAnalyzeRecordsReportsFutureFamilyResourceBucketsAndWorkflowCells(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/internal/resource/mcp"},
		{ImportPath: "example.com/project/internal/resource/plugin/install"},
		{ImportPath: "example.com/project/internal/resource/package/lock"},
		{ImportPath: "example.com/project/internal/resource/packages/catalog"},
		{ImportPath: "example.com/project/internal/resource/extension/render"},
		{ImportPath: "example.com/project/internal/resource/extensions/catalog"},
		{ImportPath: "example.com/tools/internal/fixture/internal/resource/plugin"},
		{ImportPath: "example.com/project/internal/workflow/mcp"},
		{ImportPath: "example.com/project/internal/workflow/plugin"},
		{ImportPath: "example.com/project/internal/workflow/package"},
		{ImportPath: "example.com/project/internal/workflow/packages/catalog"},
		{ImportPath: "example.com/project/internal/workflow/extension"},
		{ImportPath: "example.com/project/internal/workflow/extensions/catalog"},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"future-family-resource-bucket: internal/resource/mcp",
		"future-family-resource-bucket: internal/resource/plugin/install",
		"future-family-resource-bucket: internal/resource/package/lock",
		"future-family-resource-bucket: internal/resource/packages/catalog",
		"future-family-resource-bucket: internal/resource/extension/render",
		"future-family-resource-bucket: internal/resource/extensions/catalog",
		"future-family-resource-bucket: internal/resource/plugin",
		"future-family-workflow-cell: internal/workflow/mcp",
		"future-family-workflow-cell: internal/workflow/plugin",
		"future-family-workflow-cell: internal/workflow/package",
		"future-family-workflow-cell: internal/workflow/packages/catalog",
		"future-family-workflow-cell: internal/workflow/extension",
		"future-family-workflow-cell: internal/workflow/extensions/catalog",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}

func TestAnalyzeRecordsReportsFutureFamilyOperationCells(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/realization/lock/build",
			GoFiles: []string{
				"mcp_add.go",
				"mcp_lock.go",
				"plugin_install.go",
				"install_plugin.go",
				"plugininstall.go",
				"package_update.go",
				"render_extension.go",
				"mcp_subject.go",
				"plugin_route_contract.go",
			},
			TestGoFiles: []string{
				"mcp_apply_test.go",
				"mcpapply_test.go",
			},
		},
		{ImportPath: "example.com/tools/internal/fixture/internal/realization/lock/build/mcp_lock"},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"future-family-operation-cell: internal/realization/lock/build/mcp_add.go",
		"future-family-operation-cell: internal/realization/lock/build/mcp_lock.go",
		"future-family-operation-cell: internal/realization/lock/build/plugin_install.go",
		"future-family-operation-cell: internal/realization/lock/build/install_plugin.go",
		"future-family-operation-cell: internal/realization/lock/build/plugininstall.go",
		"future-family-operation-cell: internal/realization/lock/build/package_update.go",
		"future-family-operation-cell: internal/realization/lock/build/render_extension.go",
		"future-family-operation-cell: internal/realization/lock/build/mcp_apply_test.go",
		"future-family-operation-cell: internal/realization/lock/build/mcpapply_test.go",
		"future-family-operation-cell: internal/realization/lock/build/mcp_lock",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
	for _, unwanted := range []string{
		"internal/realization/lock/build/mcp_subject.go",
		"internal/realization/lock/build/plugin_route_contract.go",
	} {
		if strings.Contains(report, unwanted) {
			t.Fatalf("report = %q, did not want phase-owned vocabulary reported for %q", report, unwanted)
		}
	}
}
