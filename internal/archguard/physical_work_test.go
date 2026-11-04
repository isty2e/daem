package archguard

import (
	"strings"
	"testing"
)

func TestPhysicalWorkExtractionPreservesJournalPhaseBoundary(t *testing.T) {
	for _, subject := range []string{
		"internal/effect/mutation", "internal/resource", "internal/resource/declared",
		"internal/declaration", "internal/output/ownership/store", "internal/reconcile",
	} {
		for _, forbidden := range []string{
			"internal/effect/journal/recovery", "internal/effect/physicalwork", "internal/effect/physicalwork/child",
		} {
			t.Run(subject+"/"+forbidden, func(t *testing.T) {
				findings := AnalyzeRecords([]PackageRecord{{
					ImportPath: "example.com/project/" + subject,
					Imports:    []string{"example.com/project/" + forbidden},
				}})
				found := false
				for _, finding := range findings {
					if finding.ImportPath == forbidden && strings.HasSuffix(finding.Rule, ": journal") {
						found = true
					}
				}
				if !found {
					t.Fatalf("extracted journal allocation bypassed the phase boundary: %v", findings)
				}
			})
		}
	}
}

func TestPhysicalWorkContractsKeepPureInwardDependencies(t *testing.T) {
	allowed := []PackageRecord{
		{ImportPath: "example.com/project/internal/effect/workmeasure", Imports: []string{"fmt"}},
		{ImportPath: "example.com/project/internal/effect/journal/recovery", Imports: []string{"example.com/project/internal/effect/workmeasure"}},
		{ImportPath: "example.com/project/internal/effect/physicalwork", Imports: []string{
			"fmt", "slices", "example.com/project/internal/effect/journal/recovery",
			"example.com/project/internal/effect/workmeasure", "example.com/project/internal/effect/mutation/filesystem",
		}},
	}
	if findings := AnalyzeRecords(allowed); len(findings) != 0 {
		t.Fatalf("owned allocation graph was rejected: %v", findings)
	}
	for _, subject := range []string{"physicalwork", "workmeasure"} {
		for _, forbidden := range []string{"os", "context", "example.com/project/internal/workflow/apply", "example.com/project/internal/effect/storage/commit"} {
			findings := AnalyzeRecords([]PackageRecord{{
				ImportPath: "example.com/project/internal/effect/" + subject,
				Imports:    []string{forbidden},
			}})
			rule := rulePhysicalWorkImport
			if subject == "workmeasure" {
				rule = ruleWorkMeasureImport
			}
			if countViolationRule(findings, rule) != 1 {
				t.Fatalf("%s accepted %s: %v", subject, forbidden, findings)
			}
		}
	}
}
