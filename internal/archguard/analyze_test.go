package archguard

import (
	"strings"
	"testing"
)

func TestAnalyzeRecordsAllowsStablePrimitiveImports(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/desired/skill",
			Imports: []string{
				"fmt",
				"example.com/project/internal/supply/source",
				"example.com/project/internal/target",
			},
		},
	}

	violations := AnalyzeRecords(records)
	if len(violations) != 0 {
		t.Fatalf("violations = %v, want none", violations)
	}
}

func TestAnalyzeRecordsKeepsJournalRecoveryPureAndWireNeutral(t *testing.T) {
	allowed := AnalyzeRecords([]PackageRecord{{
		ImportPath: "example.com/project/internal/effect/journal/recovery",
		Imports: []string{
			"fmt",
			"io/fs",
			"example.com/project/internal/assurance/durable",
			"example.com/project/internal/output",
			"example.com/project/internal/topology",
		},
	}})
	if len(allowed) != 0 {
		t.Fatalf("reviewed journal recovery imports produced violations: %v", allowed)
	}

	report := FormatReport(AnalyzeRecords([]PackageRecord{{
		ImportPath: "example.com/project/internal/effect/journal/recovery",
		Imports: []string{
			"context",
			"example.com/project/internal/effect/mutation/filesystem",
			"gopkg.in/yaml.v3",
		},
	}}))
	for _, want := range []string{
		"journal-recovery-boundary-import: internal/effect/journal/recovery -> context",
		"journal-recovery-boundary-import: internal/effect/journal/recovery -> internal/effect/mutation/filesystem",
		"journal-recovery-boundary-import: internal/effect/journal/recovery -> gopkg.in/yaml.v3",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}

func TestAnalyzeRecordsAllowsCLIPlatformAdmissionQuery(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/cli",
			Imports:    []string{"example.com/project/internal/platformsupport"},
		},
	}

	violations := AnalyzeRecords(records)
	if len(violations) != 0 {
		t.Fatalf("violations = %v, want exact platform policy import allowed", violations)
	}
}

func TestAnalyzeRecordsRejectsCLIPlatformSupportChildren(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/cli",
			Imports:    []string{"example.com/project/internal/platformsupport/adapter"},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	if !strings.Contains(report, "cli-direct-phase-import: internal/cli -> internal/platformsupport/adapter") {
		t.Fatalf("report = %q, want child package rejected", report)
	}
}

func TestAnalyzeRecordsAllowsCLIBuildIdentityQuery(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/cli",
			Imports:    []string{"example.com/project/internal/buildidentity"},
		},
	}

	violations := AnalyzeRecords(records)
	if len(violations) != 0 {
		t.Fatalf("violations = %v, want exact build identity import allowed", violations)
	}
}

func TestAnalyzeRecordsAllowsPresentationToReadWorkflowResults(t *testing.T) {
	violations := AnalyzeRecords([]PackageRecord{{
		ImportPath: "example.com/project/internal/cli/present",
		Imports: []string{
			"example.com/project/internal/workflow/apply",
			"example.com/project/internal/workflow/authoring",
			"example.com/project/internal/workflow/init",
			"example.com/project/internal/workflow/probe",
			"example.com/project/internal/workflow/status",
		},
	}})
	if len(violations) != 0 {
		t.Fatalf("presentation result mapping violations = %#v, want none", violations)
	}
}

func TestAnalyzeRecordsAllowsCLIToImportItsPresentationContract(t *testing.T) {
	violations := AnalyzeRecords([]PackageRecord{
		{
			ImportPath: "example.com/project/internal/cli",
			Imports: []string{
				"example.com/project/internal/cli/present",
				"example.com/project/internal/cli/present/progress",
			},
		},
		{ImportPath: "example.com/project/internal/cli/present/progress"},
	})
	if len(violations) != 0 {
		t.Fatalf("CLI presentation import violations = %#v, want none", violations)
	}
}

func TestAnalyzeRecordsRejectsPresentationBoundaryReversal(t *testing.T) {
	cases := []struct {
		name        string
		packagePath string
		importPath  string
		wantRule    string
	}{
		{
			name:        "domain imports presentation",
			packagePath: "internal/diagnose",
			importPath:  "example.com/project/internal/cli/present",
			wantRule:    ruleCLIPresentationReverse,
		},
		{
			name:        "presentation imports cli",
			packagePath: "internal/cli/present",
			importPath:  "example.com/project/internal/cli",
			wantRule:    ruleInternalImportsCLI,
		},
		{
			name:        "cli imports presentation child",
			packagePath: "internal/cli",
			importPath:  "example.com/project/internal/cli/present/command",
			wantRule:    ruleCLIDirectPhaseImport,
		},
		{
			name:        "cli imports progress grandchild",
			packagePath: "internal/cli",
			importPath:  "example.com/project/internal/cli/present/progress/internal",
			wantRule:    ruleCLIDirectPhaseImport,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations := analyzeImports(tc.packagePath, []string{tc.importPath})
			if !containsFindingRule(violations, tc.wantRule) {
				t.Fatalf("violations = %#v, want rule %q", violations, tc.wantRule)
			}
		})
	}
}

func TestAnalyzeRecordsRejectsCLIBuildIdentityChildren(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/cli",
			Imports:    []string{"example.com/project/internal/buildidentity/adapter"},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	if !strings.Contains(report, "cli-direct-phase-import: internal/cli -> internal/buildidentity/adapter") {
		t.Fatalf("report = %q, want child package rejected", report)
	}
}

func TestAnalyzeRecordsEnforcesBuildIdentityAndReleaseArtifactLeaves(t *testing.T) {
	allowed := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/buildidentity",
			Imports:    []string{"example.com/project/internal/platformsupport"},
		},
		{
			ImportPath: "example.com/project/internal/releaseartifact",
			Imports: []string{
				"example.com/project/internal/buildidentity",
				"example.com/project/internal/platformsupport",
			},
		},
	}
	if violations := AnalyzeRecords(allowed); len(violations) != 0 {
		t.Fatalf("allowed leaf imports produced violations: %v", violations)
	}

	rejected := append(allowed, PackageRecord{
		ImportPath: "example.com/project/internal/buildidentity",
		Imports:    []string{"example.com/project/internal/releaseartifact"},
	}, PackageRecord{
		ImportPath: "example.com/project/internal/releaseartifact",
		Imports:    []string{"example.com/project/internal/workflow/lock"},
	})
	report := FormatReport(AnalyzeRecords(rejected))
	for _, want := range []string{
		"build-identity-boundary-import: internal/buildidentity -> internal/releaseartifact",
		"release-artifact-boundary-import: internal/releaseartifact -> internal/workflow/lock",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}

func TestAnalyzeRecordsReportsForbiddenImports(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/resource/skill",
			Imports: []string{
				"example.com/project/internal/declaration",
				"example.com/project/internal/realization/lockfile",
			},
		},
		{
			ImportPath: "example.com/project/internal/declaration/codec",
			Imports:    []string{"example.com/project/internal/resource/skill"},
		},
		{
			ImportPath: "example.com/project/internal/output/project/skill",
			Imports:    []string{"example.com/project/internal/effect/payload/skill"},
		},
		{
			ImportPath: "example.com/project/internal/effect/payload/skill",
			Imports: []string{
				"example.com/project/internal/output/project/skill",
				"example.com/project/internal/reconcile",
			},
		},
		{
			ImportPath: "example.com/project/internal/reconcile",
			Imports: []string{
				"example.com/project/internal/output/project/skill",
				"example.com/project/internal/effect/execute",
			},
		},
		{
			ImportPath: "example.com/project/internal/effect/execute",
			Imports: []string{
				"example.com/project/internal/realization/lock/build",
				"example.com/project/internal/effect/payload/build",
			},
		},
		{
			ImportPath: "example.com/project/internal/cli",
			Imports:    []string{"example.com/project/internal/target/surface"},
		},
		{
			ImportPath: "example.com/project/internal/assurance/observe/live",
			Imports:    []string{"example.com/project/internal/output/project/hook"},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"resource package imports forbidden phase: declaration: internal/resource/skill -> internal/declaration",
		"resource package imports forbidden phase: lockfile: internal/resource/skill -> internal/realization/lockfile",
		"declaration package imports forbidden phase: resource family: internal/declaration/codec -> internal/resource/skill",
		"output package imports forbidden phase: payload: internal/output/project/skill -> internal/effect/payload/skill",
		"payload package imports forbidden phase: output project: internal/effect/payload/skill -> internal/output/project/skill",
		"payload package imports forbidden phase: reconciliation: internal/effect/payload/skill -> internal/reconcile",
		"reconciliation package imports forbidden phase: output project: internal/reconcile -> internal/output/project/skill",
		"reconciliation package imports forbidden phase: execute: internal/reconcile -> internal/effect/execute",
		"journal or execute package imports forbidden phase: lock build: internal/effect/execute -> internal/realization/lock/build",
		"journal or execute package imports forbidden phase: payload build: internal/effect/execute -> internal/effect/payload/build",
		"cli-direct-phase-import: internal/cli -> internal/target/surface",
		"observe adapter package imports forbidden phase: output project: internal/assurance/observe/live -> internal/output/project/hook",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}

func TestAnalyzeRecordsRejectsDeclaredResourceIntentSeam(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/resource/declared",
			Imports: []string{
				"example.com/project/internal/intent",
				"example.com/project/internal/resource/skill",
				"example.com/project/internal/output/project",
			},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	if !strings.Contains(report, "declared resource package imports forbidden phase: intent: internal/resource/declared -> internal/intent") {
		t.Fatalf("report = %q, want declared resource intent import rejected", report)
	}
	if strings.Contains(report, "internal/resource/declared -> internal/resource/skill") {
		t.Fatalf("report = %q, did not want declared resource family seam rejected", report)
	}
	if !strings.Contains(report, "declared resource package imports forbidden phase: output: internal/resource/declared -> internal/output/project") {
		t.Fatalf("report = %q, want declared resource output import rejected", report)
	}
}

func TestAnalyzeReportReportsObserveAdapterReconciliationImport(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/assurance/observe/live",
			Imports:    []string{"example.com/project/internal/reconcile"},
		},
	}

	report := AnalyzeReport(records)
	if len(report.Violations) != 1 {
		t.Fatalf("violations = %+v, want one", report.Violations)
	}
	violation := report.Violations[0]
	if violation.Rule != ruleObserveReconciliationImport {
		t.Fatalf("violation = %+v, want observe-reconciliation-import", violation)
	}
}

func TestAnalyzeRecordsReportsForbiddenStatePackageShape(t *testing.T) {
	report := FormatReport(AnalyzeRecords([]PackageRecord{
		{ImportPath: "example.com/project/internal/state"},
		{ImportPath: "example.com/project/internal/state/projector"},
	}))
	for _, want := range []string{
		"forbidden-state-package-shape: internal/state",
		"forbidden-state-package-shape: internal/state/projector",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}

func TestAnalyzeRecordsRejectsRetiredExecutePackages(t *testing.T) {
	for _, packagePath := range []string{
		"internal/effect/execute/stateupdate",
		"internal/effect/execute/commandexec",
		"internal/effect/execute/processcwd",
		"internal/effect/execute/mcpprobe",
		"internal/effect/execute/delegateexec",
	} {
		report := FormatReport(AnalyzeRecords([]PackageRecord{{
			ImportPath: "example.com/project/" + packagePath,
		}}))
		want := "retired-execute-package: " + packagePath
		if !strings.Contains(report, want) {
			t.Errorf("report = %q, want %q", report, want)
		}
	}
}
