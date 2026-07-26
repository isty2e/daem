package archguard

import (
	"strings"
	"testing"
)

func TestArchitectureContractRejectsDesiredReverseImports(t *testing.T) {
	const desiredPath = "example.com/project/internal/desired/skill"
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/cmd/tool",
			Name:       "main",
			Imports:    []string{desiredPath},
		},
		{
			ImportPath: desiredPath,
			Name:       "skill",
			Imports: []string{
				"example.com/project/internal/workflow/lock",
				"example.com/project/internal/supply/source/backend/gitcli",
				"example.com/project/internal/target/profile",
			},
			GoFiles: []string{"skill.go"},
			FileContents: map[string]string{
				"skill.go": "package skill\ntype Skill struct{}\n",
			},
		},
	}

	violations := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(violations, ruleDesiredImportDirection) != 3 {
		t.Fatalf("desired reverse-import violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractAllowsDesiredOwnedAndStableValueImports(t *testing.T) {
	const desiredPath = "example.com/project/internal/desired/skill"
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/cmd/tool",
			Name:       "main",
			Imports:    []string{desiredPath},
		},
		{
			ImportPath: desiredPath,
			Name:       "skill",
			Imports: []string{
				"example.com/project/internal/desired",
				"example.com/project/internal/target",
				"example.com/project/internal/supply/source",
				"fmt",
			},
			GoFiles: []string{"skill.go"},
			FileContents: map[string]string{
				"skill.go": "package skill\ntype Skill struct{}\n",
			},
		},
	}

	if violations := analyzeArchitectureDependencyDirections(records); len(violations) != 0 {
		t.Fatalf("valid desired imports produced violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsInternalImportsFromDesiredEntityIdentity(t *testing.T) {
	const entityPath = "example.com/project/internal/desired/entity"
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/cmd/tool",
			Name:       "main",
			Imports:    []string{entityPath},
		},
		{
			ImportPath: entityPath,
			Name:       "entity",
			Imports:    []string{"example.com/project/internal/target"},
			GoFiles:    []string{"id.go"},
			FileContents: map[string]string{
				"id.go": "package entity\ntype ID struct{}\n",
			},
		},
	}

	violations := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(violations, ruleDesiredImportDirection) != 1 {
		t.Fatalf("desired entity import violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsTopologyRootInternalImports(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/topology",
		Name:       "topology",
		Imports: []string{
			"example.com/project/internal/desired/entity",
			"fmt",
		},
	}}

	violations := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(violations, ruleTopologyImportDirection) != 1 {
		t.Fatalf("topology root import violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractAllowsTopologyFamilyLowererImports(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/topology/hook",
			Name:       "hook",
			Imports: []string{
				"example.com/project/internal/desired/entity",
				"example.com/project/internal/desired/hook",
				"example.com/project/internal/desired/hookasset",
				"example.com/project/internal/target",
				"example.com/project/internal/topology",
				"example.com/project/internal/topology/projection",
				"fmt",
			},
		},
		{
			ImportPath: "example.com/project/internal/topology/mcp",
			Name:       "mcp",
			Imports: []string{
				"example.com/project/internal/desired/mcp",
				"example.com/project/internal/target",
				"example.com/project/internal/topology",
				"fmt",
			},
		},
		{
			ImportPath: "example.com/project/internal/topology/extension",
			Name:       "extension",
			Imports: []string{
				"example.com/project/internal/desired/extension",
				"example.com/project/internal/topology",
				"fmt",
			},
		},
		{
			ImportPath: "example.com/project/internal/topology/resource",
			Name:       "resource",
			Imports: []string{
				"example.com/project/internal/desired/entity",
				"example.com/project/internal/topology",
				"fmt",
			},
		},
	}

	if violations := analyzeArchitectureDependencyDirections(records); len(violations) != 0 {
		t.Fatalf("valid topology family imports produced violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsTopologyFamilyReverseImports(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/topology/mcp",
		Name:       "mcp",
		Imports: []string{
			"example.com/project/internal/lifecycle",
			"example.com/project/internal/realization/lock",
			"example.com/project/internal/supply/source",
			"example.com/project/internal/realization/aggregate",
			"example.com/project/internal/workflow/apply",
		},
	}}

	violations := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(violations, ruleTopologyImportDirection) != 5 {
		t.Fatalf("topology family reverse-import violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsOperationSelectionInsideHookTopology(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/topology/hook",
		Name:       "hook",
		Imports:    []string{"example.com/project/internal/target/selection"},
	}}

	violations := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(violations, ruleTopologyImportDirection) != 1 {
		t.Fatalf("hook topology selection violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsTopologyFamilyCrossFamilyAndImplicitAdmissions(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/topology/mcp",
			Name:       "mcp",
			Imports: []string{
				"example.com/project/internal/desired/skill",
				"example.com/project/internal/desired/mcp/private",
				"example.com/project/internal/topology/extension",
			},
		},
		{
			ImportPath: "example.com/project/internal/topology/future",
			Name:       "future",
			Imports: []string{
				"example.com/project/internal/desired/mcp",
				"example.com/project/internal/realization/aggregate",
			},
		},
		{
			ImportPath: "example.com/project/internal/topology/extension",
			Name:       "extension",
			Imports: []string{
				"example.com/project/internal/desired/extension/private",
				"example.com/project/internal/desired/mcp",
				"example.com/project/internal/lifecycle",
				"example.com/project/internal/topology/mcp",
			},
		},
	}

	violations := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(violations, ruleTopologyImportDirection) != 9 {
		t.Fatalf("topology exact-admission violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsResourceLowererCrossFamilyAndHigherLayerImports(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/topology/resource",
		Name:       "resource",
		Imports: []string{
			"example.com/project/internal/desired/skill",
			"example.com/project/internal/realization/lock",
		},
	}}

	violations := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(violations, ruleTopologyImportDirection) != 2 {
		t.Fatalf("resource lowerer exact-admission violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsCrossBlockKernelImports(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/supply/source/acquisition",
			Imports:    []string{"example.com/project/internal/topology"},
		},
		{
			ImportPath: "example.com/project/internal/realization/aggregate/hook",
			Imports:    []string{"example.com/project/internal/assurance/runtimeprobe"},
		},
		{
			ImportPath: "example.com/project/internal/assurance/runtimeprobe",
			Imports:    []string{"example.com/project/internal/reconcile"},
		},
		{
			ImportPath: "example.com/project/internal/reconcile/build/hostroute",
			Imports:    []string{"example.com/project/internal/effect/execute"},
		},
		{
			ImportPath: "example.com/project/internal/effect/mutation",
			Imports:    []string{"example.com/project/internal/desired/skill"},
		},
	}

	violations := analyzeArchitectureDependencyDirections(records)
	for _, rule := range []string{
		ruleSupplyImportDirection,
		ruleRealizationImportDirection,
		ruleAssuranceImportDirection,
		ruleReconciliationImportDirection,
		ruleEffectImportDirection,
	} {
		if countViolationRule(violations, rule) != 1 {
			t.Errorf("%s count = %d, want 1\n%s", rule, countViolationRule(violations, rule), FormatReport(violations))
		}
	}
}

func TestArchitectureContractAllowsCanonicalContentHashScalarImports(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/assurance/durable",
			Imports:    []string{"example.com/project/internal/supply/artifact"},
		},
		{
			ImportPath: "example.com/project/internal/assurance/observe",
			Imports:    []string{"example.com/project/internal/supply/artifact"},
		},
		{
			ImportPath: "example.com/project/internal/reconcile",
			Imports:    []string{"example.com/project/internal/supply/artifact"},
		},
		{
			ImportPath: "example.com/project/internal/effect/execute",
			Imports: []string{
				"example.com/project/internal/supply/artifact",
				"example.com/project/internal/supply/artifact/access",
			},
		},
	}

	if violations := analyzeArchitectureDependencyDirections(records); len(violations) != 0 {
		t.Fatalf("canonical content-hash scalar imports produced violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractAllowsExactStableCrossBlockValueImports(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/supply/source/archive",
			Imports:    []string{"example.com/project/internal/target"},
		},
		{
			ImportPath: "example.com/project/internal/realization",
			Imports: []string{
				"example.com/project/internal/output",
				"example.com/project/internal/target",
			},
		},
		{
			ImportPath: "example.com/project/internal/assurance/durable",
			Imports: []string{
				"example.com/project/internal/output",
				"example.com/project/internal/output/ownership",
				"example.com/project/internal/target",
			},
		},
		{
			ImportPath: "example.com/project/internal/reconcile",
			Imports: []string{
				"example.com/project/internal/output",
				"example.com/project/internal/output/ownership",
				"example.com/project/internal/target",
			},
		},
		{
			ImportPath: "example.com/project/internal/effect/execute",
			Imports: []string{
				"example.com/project/internal/output",
				"example.com/project/internal/output/ownership",
				"example.com/project/internal/target",
			},
		},
	}

	if violations := analyzeArchitectureDependencyDirections(records); len(violations) != 0 {
		t.Fatalf("stable cross-block value imports produced violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsStableValuePackageDescendantsFromKernels(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/supply/source/archive",
			Imports:    []string{"example.com/project/internal/target/selection"},
		},
		{
			ImportPath: "example.com/project/internal/realization",
			Imports:    []string{"example.com/project/internal/output/ownership/store"},
		},
		{
			ImportPath: "example.com/project/internal/assurance/durable",
			Imports:    []string{"example.com/project/internal/output/ownership/store"},
		},
		{
			ImportPath: "example.com/project/internal/reconcile",
			Imports:    []string{"example.com/project/internal/output/ownership/store"},
		},
		{
			ImportPath: "example.com/project/internal/effect/mutation",
			Imports:    []string{"example.com/project/internal/output/ownership/store"},
		},
		{
			ImportPath: "example.com/project/internal/assurance/durable",
			Imports:    []string{"example.com/project/internal/output/hostpath"},
		},
		{
			ImportPath: "example.com/project/internal/reconcile",
			Imports:    []string{"example.com/project/internal/output/hostpath"},
		},
		{
			ImportPath: "example.com/project/internal/effect/mutation",
			Imports:    []string{"example.com/project/internal/output/hostpath"},
		},
	}

	violations := analyzeArchitectureDependencyDirections(records)
	for rule, want := range map[string]int{
		ruleSupplyImportDirection:         1,
		ruleRealizationImportDirection:    1,
		ruleAssuranceImportDirection:      2,
		ruleReconciliationImportDirection: 2,
		ruleEffectImportDirection:         2,
	} {
		if countViolationRule(violations, rule) != want {
			t.Errorf("%s count = %d, want %d\n%s", rule, countViolationRule(violations, rule), want, FormatReport(violations))
		}
	}
}

func TestArchitectureContractRejectsStableValuesOutsideTheirAdmittedBlocks(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/supply/source/archive",
			Imports:    []string{"example.com/project/internal/output"},
		},
		{
			ImportPath: "example.com/project/internal/realization",
			Imports:    []string{"example.com/project/internal/output/ownership"},
		},
	}

	violations := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(violations, ruleSupplyImportDirection) != 1 ||
		countViolationRule(violations, ruleRealizationImportDirection) != 1 {
		t.Fatalf("stable-value scope violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsSupplyBehaviorFromEffect(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/effect/execute",
		Imports:    []string{"example.com/project/internal/supply/source/acquisition"},
	}}

	violations := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(violations, ruleEffectImportDirection) != 1 {
		t.Fatalf("effect supply import violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsArtifactBehaviorImportsFromLaterKernels(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/assurance/durable",
			Imports:    []string{"example.com/project/internal/supply/artifact/access"},
		},
		{
			ImportPath: "example.com/project/internal/reconcile",
			Imports:    []string{"example.com/project/internal/supply/artifact/access"},
		},
	}

	violations := analyzeArchitectureDependencyDirections(records)
	if countViolationRule(violations, ruleAssuranceImportDirection) != 1 ||
		countViolationRule(violations, ruleReconciliationImportDirection) != 1 {
		t.Fatalf("artifact behavior import violations:\n%s", FormatReport(violations))
	}
}

func TestAnalyzeReportIncludesCrossBlockKernelGuards(t *testing.T) {
	report := AnalyzeReport([]PackageRecord{{
		ImportPath: "example.com/project/internal/supply/source/archive",
		Imports:    []string{"example.com/project/internal/reconcile"},
	}})
	if !containsFindingRule(report.Violations, ruleSupplyImportDirection) {
		t.Fatalf("AnalyzeReport violations:\n%s", FormatAnalysisReport(report))
	}
}

func TestArchitectureContractKeepsBoundaryAdaptersOutOfSemanticKernelRules(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/supply/source/resolution",
			Imports:    []string{"example.com/project/internal/topology"},
		},
		{
			ImportPath: "example.com/project/internal/assurance/observe/live",
			Imports:    []string{"example.com/project/internal/supply/artifact"},
		},
		{
			ImportPath: "example.com/project/internal/workflow/readiness",
			Imports:    []string{"example.com/project/internal/reconcile"},
		},
		{
			ImportPath: "example.com/project/internal/subprocess",
			Imports:    []string{"example.com/project/internal/desired/skill"},
		},
		{
			ImportPath: "example.com/project/internal/assurance/runtimeprobe/mcp",
			Imports:    []string{"example.com/project/internal/subprocess"},
		},
		{
			ImportPath: "example.com/project/internal/effect/journal",
			Imports:    []string{"example.com/project/internal/desired/entity"},
		},
	}

	if violations := analyzeArchitectureDependencyDirections(records); len(violations) != 0 {
		t.Fatalf("boundary adapters or stable identity imports produced violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractClassifiesMCPRuntimeProbeAdapterOnlyAsBoundary(t *testing.T) {
	blocks := semanticDependencyBlocksForPackage("internal/assurance/runtimeprobe/mcp")
	if len(blocks) != 1 || blocks[0] != dependencyBoundary {
		t.Fatalf("MCP runtime probe blocks = %v, want Boundary only", blocks)
	}
}

func TestArchitectureContractClassifiesAssuranceOwnerLocalBoundariesExactly(t *testing.T) {
	for _, packagePath := range []string{
		"internal/assurance/runtimeprobe/mcp",
		"internal/assurance/observe/antigravityplugin",
		"internal/assurance/observe/claudeplugin",
		"internal/assurance/observe/codexplugin",
		"internal/assurance/observe/live",
		"internal/assurance/observe/lock",
		"internal/assurance/observe/ownership",
		"internal/assurance/observe/pipackage",
		"internal/assurance/observe/relation/host",
		"internal/assurance/statefile",
	} {
		blocks := semanticDependencyBlocksForPackage(packagePath)
		if len(blocks) != 1 || blocks[0] != dependencyBoundary {
			t.Errorf("%s blocks = %v, want Boundary only", packagePath, blocks)
		}
	}

	for _, packagePath := range []string{
		"internal/assurance/durable",
		"internal/assurance/hostroute",
		"internal/assurance/runtimeprobe",
		"internal/assurance/observe",
		"internal/assurance/observe/config",
		"internal/assurance/observe/contribution",
		"internal/assurance/observe/mcp",
		"internal/assurance/observe/relation",
	} {
		blocks := semanticDependencyBlocksForPackage(packagePath)
		if len(blocks) != 1 || blocks[0] != dependencyAssurance {
			t.Errorf("%s blocks = %v, want Assurance only", packagePath, blocks)
		}
	}
}

func TestArchitectureContractRejectsConcreteAggregateCodecsFromSemanticKernels(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/reconcile",
			Imports: []string{
				"example.com/project/internal/realization/aggregate/codec",
			},
		},
		{
			ImportPath: "example.com/project/internal/effect/execute",
			Imports: []string{
				"example.com/project/internal/realization/aggregate/codec/mcp",
			},
		},
		{
			ImportPath: "example.com/project/internal/workflow/readiness",
			Imports: []string{
				"example.com/project/internal/realization/aggregate/codec/hook",
			},
		},
		{
			ImportPath: "example.com/project/internal/assurance/observe/mcp",
			Imports: []string{
				"example.com/project/internal/realization/aggregate/codec/mcp",
			},
		},
		{
			ImportPath: "example.com/project/internal/cli/present",
			Imports: []string{
				"example.com/project/internal/realization/aggregate/codec",
			},
		},
		{
			ImportPath: "example.com/project/internal/workflow/apply",
			Imports: []string{
				"example.com/project/internal/realization/aggregate/codec",
			},
		},
		{
			ImportPath: "example.com/project/internal/workflow/help",
			Imports: []string{
				"example.com/project/internal/realization/aggregate/codec",
			},
		},
	}

	report := FormatReport(AnalyzeRecords(records))
	for _, want := range []string{
		"aggregate-codec-boundary-import: internal/effect/execute -> internal/realization/aggregate/codec/mcp",
		"aggregate-codec-boundary-import: internal/assurance/observe/mcp -> internal/realization/aggregate/codec/mcp",
		"aggregate-codec-boundary-import: internal/reconcile -> internal/realization/aggregate/codec",
		"aggregate-codec-boundary-import: internal/cli/present -> internal/realization/aggregate/codec",
		"aggregate-codec-boundary-import: internal/workflow/readiness -> internal/realization/aggregate/codec/hook",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
	if strings.Contains(report, "internal/workflow/apply -> internal/realization/aggregate/codec") {
		t.Fatalf("report = %q, boundary workflow must be allowed to compose concrete codecs", report)
	}
	if strings.Contains(report, "internal/workflow/help -> internal/realization/aggregate/codec") {
		t.Fatalf("report = %q, help workflow must be allowed to read finite codec capabilities", report)
	}
}

func TestArchitectureContractRejectsProductionImportsOfTestTools(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/cmd/tool",
			Name:       "main",
			Imports:    []string{"example.com/project/internal/archguard"},
		},
		{ImportPath: "example.com/project/internal/archguard", Name: "archguard"},
	}

	violations := analyzeProductionTestToolImports(records, map[string]testToolAdmission{
		"internal/archguard": {Reason: "synthetic test/tool package", Kind: testToolHelperPackage},
	})
	assertViolationRule(t, violations, ruleProductionImportsTestTool)
}

func TestArchitectureContractRejectsProductionImportsOfTestToolDescendants(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/cmd/tool",
		Name:       "main",
		Imports:    []string{"example.com/project/internal/archguard/private"},
	}}
	violations := analyzeProductionTestToolImports(records, map[string]testToolAdmission{
		"internal/archguard": {Reason: "synthetic test/tool package", Kind: testToolHelperPackage},
	})
	assertViolationRule(t, violations, ruleProductionImportsTestTool)
}

func TestArchitectureContractRejectsProductionImportsOfTopLevelTestHelpers(t *testing.T) {
	const helperPath = "example.com/project/test/testkit"
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/cmd/tool",
			Name:       "main",
			Imports:    []string{helperPath},
		},
		{ImportPath: helperPath, Name: "testkit"},
	}
	violations := analyzeProductionTestToolImports(records, map[string]testToolAdmission{
		"test/testkit": {Reason: "synthetic top-level test helper", Kind: testToolHelperPackage},
	})
	assertViolationRule(t, violations, ruleProductionImportsTestTool)
}

func TestTestToolOwnershipRequiresExactNestedAdmission(t *testing.T) {
	admission, ok := testToolOwner("test/testkit/doctorenv", map[string]testToolAdmission{
		"test/testkit":           {Reason: "parent", Kind: testToolHelperPackage},
		"test/testkit/doctorenv": {Reason: "exact", Kind: testToolHelperPackage},
	})
	if !ok || admission.Reason != "exact" {
		t.Fatalf("admission = %+v, %t; want exact nested owner", admission, ok)
	}
}

func TestTestToolOwnershipDoesNotImplicitlyAdmitDescendants(t *testing.T) {
	_, ok := testToolOwner("test/testkit/future", map[string]testToolAdmission{
		"test/testkit": {Reason: "parent", Kind: testToolHelperPackage},
	})
	if ok {
		t.Fatal("future descendant inherited admission; want exact admission")
	}
}
