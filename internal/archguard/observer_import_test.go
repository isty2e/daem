package archguard

import "testing"

func TestLiveObserverImportBoundary(t *testing.T) {
	const (
		piReader = "assurance/observe/pipackage"
		hostPath = "assurance/observe/hostpath"
		journal  = "journal or execute package imports forbidden phase: observe adapter"
	)
	for _, tc := range []struct{ from, to, rule string }{
		{"effect/journal/record", piReader, journal},
		{"effect/journal/record", "assurance/observe/relation/piorder", ""},
		{"effect/execute/configrelation", "assurance/observe/relation/host", journal},
		{"effect/execute/configrelation", "assurance/observe/relation/selecteddoc", ""},
		{"effect/execute/configrelation", "assurance/runtimeprobe/mcp", journal},
		{"reconcile/build/documenttransition", piReader, "reconciliation package imports forbidden phase: observe adapter"},
		{"effect/protection/scan", piReader, ruleEffectObserverImport},
		{"effect/payload/build", piReader, ""},
		{"output/ownership/store", "assurance/runtimeprobe/mcp", ruleEffectObserverImport},
		{"effect/journal/record", "assurance/observe/lock/future", journal},
		{"effect/journal/record", piReader + "/future", ""},
		{"effect/mutation", "filesnapshot", ""},
		{"effect/metadataconversion", hostPath, ""},
		{"effect/protection/namespace", hostPath, ""},
		{"effect/payload", hostPath, ruleEffectImportDirection},
	} {
		t.Run(tc.from+"->"+tc.to, func(t *testing.T) {
			const prefix = "example.com/project/internal/"
			report := AnalyzeReport([]PackageRecord{{ImportPath: prefix + tc.from, Imports: []string{prefix + tc.to}}})
			if tc.rule != "" {
				assertViolationRule(t, report.Violations, tc.rule)
			} else if report.HasFailures() {
				t.Fatal(FormatAnalysisReport(report))
			}
		})
	}
}
