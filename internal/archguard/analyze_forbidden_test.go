package archguard

import (
	"strings"
	"testing"
)

func TestAnalyzeRecordsReportsAdditionalForbiddenImportFamilies(t *testing.T) {
	cases := []struct {
		name   string
		record PackageRecord
		want   string
	}{
		{
			name: "resource source backend",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/resource/skill",
				Imports:    []string{"example.com/project/internal/supply/source/backend/gitcli"},
			},
			want: "resource package imports forbidden phase: source backend: internal/resource/skill -> internal/supply/source/backend/gitcli",
		},
		{
			name: "resource surface",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/resource/hook",
				Imports:    []string{"example.com/project/internal/target/surface"},
			},
			want: "resource package imports forbidden phase: surface: internal/resource/hook -> internal/target/surface",
		},
		{
			name: "declaration lock",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/declaration",
				Imports:    []string{"example.com/project/internal/realization/lock"},
			},
			want: "declaration package imports forbidden phase: lock: internal/declaration -> internal/realization/lock",
		},
		{
			name: "output statefile",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/output",
				Imports:    []string{"example.com/project/internal/assurance/statefile"},
			},
			want: "output package imports forbidden phase: statefile: internal/output -> internal/assurance/statefile",
		},
		{
			name: "payload presentation",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/effect/payload/skill",
				Imports:    []string{"example.com/project/internal/cli/present/json"},
			},
			want: "payload package imports forbidden phase: present: internal/effect/payload/skill -> internal/cli/present/json",
		},
		{
			name: "reconciliation source backend",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/reconcile",
				Imports:    []string{"example.com/project/internal/supply/source/backend/localfs"},
			},
			want: "reconciliation package imports forbidden phase: source backend: internal/reconcile -> internal/supply/source/backend/localfs",
		},
		{
			name: "reconciliation observe adapter",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/reconcile",
				Imports:    []string{"example.com/project/internal/assurance/observe/live"},
			},
			want: "reconciliation package imports forbidden phase: observe adapter: internal/reconcile -> internal/assurance/observe/live",
		},
		{
			name: "observe root statefile",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/assurance/observe",
				Imports:    []string{"example.com/project/internal/assurance/statefile"},
			},
			want: "observe package imports forbidden phase: statefile: internal/assurance/observe -> internal/assurance/statefile",
		},
		{
			name: "observe root workflow",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/assurance/observe",
				Imports:    []string{"example.com/project/internal/workflow/status"},
			},
			want: "observe package imports forbidden phase: workflow: internal/assurance/observe -> internal/workflow/status",
		},
		{
			name: "journal observe adapter",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/effect/journal",
				Imports:    []string{"example.com/project/internal/assurance/observe/live"},
			},
			want: "journal or execute package imports forbidden phase: observe adapter: internal/effect/journal -> internal/assurance/observe/live",
		},
		{
			name: "execute diagnose",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/effect/execute",
				Imports:    []string{"example.com/project/internal/diagnose"},
			},
			want: "journal or execute package imports forbidden phase: diagnose: internal/effect/execute -> internal/diagnose",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := FormatReport(AnalyzeRecords([]PackageRecord{tc.record}))
			if !strings.Contains(report, tc.want) {
				t.Fatalf("report = %q, want %q", report, tc.want)
			}
		})
	}
}

func TestAnalyzeRecordsRejectsStorageCommitInternalImports(t *testing.T) {
	report := FormatAnalysisReport(AnalyzeReport([]PackageRecord{{
		ImportPath: "github.com/isty2e/daem/internal/effect/storage/commit",
		Imports: []string{
			"github.com/isty2e/daem/internal/workflow/apply",
		},
	}}))
	want := "storage-commit-boundary-import: internal/effect/storage/commit -> internal/workflow/apply"
	if !strings.Contains(report, want) {
		t.Fatalf("report = %q, want %q", report, want)
	}
}

func TestAnalyzeRecordsAllowsStorageCommitEffectContractsOnly(t *testing.T) {
	report := AnalyzeReport([]PackageRecord{{
		ImportPath: "github.com/isty2e/daem/internal/effect/storage/commit",
		Imports: []string{
			"github.com/isty2e/daem/internal/effect/mutation/filesystem",
			"github.com/isty2e/daem/internal/effect/mutation/rootedpath",
		},
	}})
	if len(report.Violations) != 0 {
		t.Fatalf("storage Effect-contract import violations = %v", report.Violations)
	}
}

func TestAnalyzeRecordsRejectsStorageCommitEffectContractDescendants(t *testing.T) {
	report := AnalyzeReport([]PackageRecord{{
		ImportPath: "github.com/isty2e/daem/internal/effect/storage/commit",
		Imports: []string{
			"github.com/isty2e/daem/internal/effect/mutation/filesystem/private",
			"github.com/isty2e/daem/internal/effect/mutation/rootedpath/private",
		},
	}})
	if countViolationRule(report.Violations, ruleStorageCommitImport) != 2 ||
		countViolationRule(report.Violations, rulePackagePlacementOwnership) != 2 {
		t.Fatalf("storage Effect-contract descendant violations = %v, want two storage and two placement findings", report.Violations)
	}
}

func TestAnalyzeRecordsReportsInternalCLIImports(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/supply/source",
			Imports:    []string{"example.com/project/internal/cli"},
		},
		{
			ImportPath: "example.com/project/internal/resource/skill",
			Imports:    []string{"example.com/project/internal/cli/present"},
		},
		{
			ImportPath: "example.com/project/internal/resource/hook",
			Imports:    []string{"example.com/project/internal/clihelpers"},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"internal package imports CLI: internal/supply/source -> internal/cli",
		"resource package imports forbidden phase: present: internal/resource/skill -> internal/cli/present",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
	if strings.Contains(report, "internal/resource/hook -> internal/clihelpers") {
		t.Fatalf("report = %q, did not want clihelpers reported as CLI", report)
	}
}

func TestAnalyzeRecordsUsesLastInternalSegment(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/internal/tools/internal/resource/skill",
			Imports:    []string{"example.com/internal/tools/internal/declaration"},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	want := "resource package imports forbidden phase: declaration: internal/resource/skill -> internal/declaration"
	if !strings.Contains(report, want) {
		t.Fatalf("report = %q, want %q", report, want)
	}
}

func TestAnalyzeRecordsReportsWorkflowReverseImports(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/resource/skill",
			Imports:    []string{"example.com/project/internal/workflow/lock"},
		},
		{
			ImportPath: "example.com/project/internal/target",
			Imports:    []string{"example.com/project/internal/workflow/status"},
		},
		{
			ImportPath: "example.com/project/internal/cli",
			Imports:    []string{"example.com/project/internal/workflow/status"},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	if !strings.Contains(report, "workflow-reverse-import: internal/resource/skill -> internal/workflow/lock") {
		t.Fatalf("report = %q, want resource workflow reverse import", report)
	}
	if !strings.Contains(report, "workflow-reverse-import: internal/target -> internal/workflow/status") {
		t.Fatalf("report = %q, want primitive workflow reverse import", report)
	}
	if strings.Contains(report, "workflow-reverse-import: internal/cli -> internal/workflow/status") {
		t.Fatalf("report = %q, did not want CLI workflow import reported as reverse phase import", report)
	}
}

func TestAnalyzeRecordsDoesNotTreatTargetSelectionAsWorkflow(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/assurance/observe/live",
			Imports:    []string{"example.com/project/internal/target/selection"},
		},
		{
			ImportPath: "example.com/project/internal/effect/payload/build",
			Imports:    []string{"example.com/project/internal/target/selection"},
		},
		{
			ImportPath: "example.com/project/internal/resource/skill",
			Imports:    []string{"example.com/project/internal/target/selection"},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, unwanted := range []string{
		"internal/assurance/observe/live -> internal/target/selection",
		"internal/effect/payload/build -> internal/target/selection",
		"internal/resource/skill -> internal/target/selection",
	} {
		if strings.Contains(report, unwanted) {
			t.Fatalf("report = %q, did not want target selection import %q reported as workflow", report, unwanted)
		}
	}
}

func TestAnalyzeRecordsReportsTargetSelectionForbiddenImports(t *testing.T) {
	cases := []struct {
		name   string
		record PackageRecord
		want   string
	}{
		{
			name: "intent",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/target/selection",
				Imports:    []string{"example.com/project/internal/intent"},
			},
			want: "target selection package imports forbidden phase: intent: internal/target/selection -> internal/intent",
		},
		{
			name: "declared resource",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/target/selection",
				Imports:    []string{"example.com/project/internal/resource/declared"},
			},
			want: "target selection package imports forbidden phase: declared resource: internal/target/selection -> internal/resource/declared",
		},
		{
			name: "resource family",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/target/selection",
				Imports:    []string{"example.com/project/internal/resource/skill"},
			},
			want: "target selection package imports forbidden phase: resource family: internal/target/selection -> internal/resource/skill",
		},
		{
			name: "workflow",
			record: PackageRecord{
				ImportPath: "example.com/project/internal/target/selection",
				Imports:    []string{"example.com/project/internal/workflow/status"},
			},
			want: "target selection package imports forbidden phase: workflow: internal/target/selection -> internal/workflow/status",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := FormatReport(AnalyzeRecords([]PackageRecord{tc.record}))
			if !strings.Contains(report, tc.want) {
				t.Fatalf("report = %q, want %q", report, tc.want)
			}
		})
	}
}
