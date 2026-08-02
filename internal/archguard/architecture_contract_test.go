package archguard

import "testing"

func TestArchitectureContractBaseline(t *testing.T) {
	records := loadRepoPackageRecords(t)
	violations := analyzeArchitectureContracts(records)
	if len(violations) != 0 {
		t.Fatalf("architecture contract baseline has failures:\n%s", FormatReport(violations))
	}
	t.Log("command: tools/test-go.sh -run TestArchitectureContractBaseline -count=1 -v ./internal/archguard")
}

func TestArchitectureContractRejectsPhaseByFamilyProductionCells(t *testing.T) {
	violations := AnalyzeRecords([]PackageRecord{{
		ImportPath: "example.com/project/internal/workflow/plugin",
		GoFiles:    []string{"install_plugin.go"},
	}})
	assertViolationRule(t, violations, ruleFutureFamilyWorkflowCell)
	assertViolationRule(t, violations, ruleFutureFamilyOperationCell)
}

func TestArchitectureContractRejectsUnexpectedCanonicalAuthorities(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/domain/skill",
			GoFiles:    []string{"model.go"},
			FileContents: map[string]string{
				"model.go": "package skill\ntype Skill struct{}\n",
			},
		},
		{
			ImportPath: "example.com/project/internal/intent/newfamily",
			GoFiles:    []string{"model.go"},
			FileContents: map[string]string{
				"model.go": "package newfamily\ntype Value struct{}\n",
			},
		},
		{
			ImportPath: "example.com/project/internal/locked/model",
			GoFiles:    []string{"model.go"},
			FileContents: map[string]string{
				"model.go": "package model\ntype Locked struct{}\n",
			},
		},
	}
	violations := analyzeCanonicalAuthorities(records)
	if countViolationRule(violations, ruleCanonicalAuthority) != 3 {
		t.Fatalf("canonical authority violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractAllowsSelectedDesiredAuthority(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/desired/skill",
			GoFiles:    []string{"model.go"},
			FileContents: map[string]string{
				"model.go": "package skill\ntype Skill struct{}\n",
			},
		},
		{
			ImportPath: "example.com/project/internal/realization/lock",
			GoFiles:    []string{"model.go"},
			FileContents: map[string]string{
				"model.go": "package lock\ntype LockedSubject struct{}\n",
			},
		},
	}
	if violations := analyzeCanonicalAuthorities(records); len(violations) != 0 {
		t.Fatalf("selected desired authority violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsPermanentlyRetiredPackages(t *testing.T) {
	var records []PackageRecord
	for _, retiredRoot := range []string{
		"internal/adopt/discover",
		"internal/declaration/codec/extension",
		"internal/declaration/codec/hook",
		"internal/declaration/codec/instructions",
		"internal/declaration/codec/mcpserver",
		"internal/declaration/codec/skill",
		"internal/declaration/codec/skillgroup",
		"internal/declaration/doc",
		"internal/declaration/edit",
		"internal/declaration/toml",
		"internal/diagnose/manifest",
		"internal/intent",
		"internal/effect/journal/pathstate",
		"internal/lifecycle",
		"internal/realization/lock/delta",
		"internal/realization/lock/snapshot",
		"internal/pathstate",
		"internal/resource",
		"internal/subprocess/capturetext",
		"internal/subprocess/childenv",
		"internal/subprocess/command",
		"internal/subprocess/processtree",
		"internal/subprocess/workdir",
		"internal/surface/destination",
		"internal/surface/directory",
		"internal/surface/file",
		"internal/surface/operation",
	} {
		records = append(
			records,
			PackageRecord{
				ImportPath: "example.com/project/" + retiredRoot,
				GoFiles:    []string{"model.go"},
			},
			PackageRecord{
				ImportPath: "example.com/project/" + retiredRoot + "/detail",
				GoFiles:    []string{"model.go"},
			},
		)
	}
	violations := analyzeCanonicalAuthorities(records)
	if countViolationRule(violations, ruleCanonicalAuthority) != len(records) {
		t.Fatalf("retired package violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsUnreachableProductionPackages(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/cmd/tool", Name: "main"},
		{
			ImportPath: "example.com/project/internal/orphan",
			Name:       "orphan",
			GoFiles:    []string{"model.go"},
			FileContents: map[string]string{
				"model.go": "package orphan\ntype Model struct{}\n",
			},
		},
	}
	violations := analyzeUnreachableProductionPackages(records, nil)
	assertViolationRule(t, violations, ruleUnreachableProductionPackage)
}

func TestArchitectureContractRejectsUnexportedOnlyProductionIsland(t *testing.T) {
	cases := map[string]string{
		"private function": "package scratch\nfunc helper() {}\n",
		"private type":     "package scratch\ntype model struct{}\n",
		"private values":   "package scratch\nconst state = 1\nvar current = state\n",
		"init only":        "package scratch\nfunc init() {}\n",
		"package only":     "package scratch\n",
		"test imported":    "package scratch\nfunc testSupport() {}\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			const scratchPath = "example.com/project/internal/scratch"
			records := []PackageRecord{
				{ImportPath: "example.com/project/cmd/tool", Name: "main"},
				{
					ImportPath: scratchPath,
					Name:       "scratch",
					GoFiles:    []string{"scratch.go"},
					FileContents: map[string]string{
						"scratch.go": source,
					},
					TestGoFiles: []string{"scratch_test.go"},
				},
			}
			if name == "test imported" {
				records = append(records, PackageRecord{
					ImportPath: "example.com/project/test/testkit",
					Name:       "testkit",
					Imports:    []string{scratchPath},
				})
			}
			violations := analyzeUnreachableProductionPackages(records, nil)
			if countViolationRule(violations, ruleUnreachableProductionPackage) != 1 {
				t.Fatalf("unexported package-island violations:\n%s", FormatReport(violations))
			}
		})
	}
}

func TestArchitectureContractRejectsDisconnectedProductionClusters(t *testing.T) {
	const childPath = "example.com/project/internal/orphan/child"
	records := []PackageRecord{
		{ImportPath: "example.com/project/cmd/tool", Name: "main"},
		{
			ImportPath: "example.com/project/internal/orphan",
			Name:       "orphan",
			Imports:    []string{childPath},
			GoFiles:    []string{"model.go"},
			FileContents: map[string]string{
				"model.go": "package orphan\ntype Model struct{}\n",
			},
		},
		{
			ImportPath: childPath,
			Name:       "child",
			GoFiles:    []string{"child.go"},
			FileContents: map[string]string{
				"child.go": "package child\ntype Child struct{}\n",
			},
		},
	}
	violations := analyzeUnreachableProductionPackages(records, nil)
	if countViolationRule(violations, ruleUnreachableProductionPackage) != 2 {
		t.Fatalf("disconnected cluster violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractFollowsProductionReachability(t *testing.T) {
	const modelPath = "example.com/project/internal/model"
	records := []PackageRecord{
		{ImportPath: "example.com/project/cmd/tool", Name: "main", Imports: []string{modelPath}},
		{
			ImportPath: modelPath,
			Name:       "model",
			GoFiles:    []string{"model.go"},
			FileContents: map[string]string{
				"model.go": "package model\ntype Model struct{}\n",
			},
		},
	}
	if violations := analyzeUnreachableProductionPackages(records, nil); len(violations) != 0 {
		t.Fatalf("reachable production package violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractDoesNotTreatTestOnlyImportsAsProductionCallers(t *testing.T) {
	const modelPath = "example.com/project/internal/model"
	records := []PackageRecord{
		{ImportPath: "example.com/project/cmd/tool", Name: "main"},
		{ImportPath: "example.com/project/test/testkit", Name: "testkit", Imports: []string{modelPath}},
		{
			ImportPath: modelPath,
			Name:       "model",
			GoFiles:    []string{"model.go"},
			FileContents: map[string]string{
				"model.go": "package model\ntype Model struct{}\n",
			},
		},
	}
	violations := analyzeUnreachableProductionPackages(records, nil)
	assertViolationRule(t, violations, ruleUnreachableProductionPackage)
}

func TestArchitectureContractRejectsReachableAndStaleTestToolAdmissions(t *testing.T) {
	const toolPath = "example.com/project/internal/archguard"
	const bridgePath = "example.com/project/internal/bridge"
	admissions := map[string]testToolAdmission{
		"internal/archguard": {Reason: "synthetic test/tool admission", Kind: testToolHelperPackage},
	}
	records := []PackageRecord{
		{ImportPath: "example.com/project/cmd/tool", Name: "main", Imports: []string{bridgePath}},
		{ImportPath: bridgePath, Name: "bridge", Imports: []string{toolPath}},
		{
			ImportPath: toolPath,
			Name:       "archguard",
			GoFiles:    []string{"model.go"},
			FileContents: map[string]string{
				"model.go": "package archguard\ntype Report struct{}\n",
			},
		},
	}

	reachable := analyzeUnreachableProductionPackages(records, admissions)
	assertViolationRule(t, reachable, ruleStaleTestToolAdmission)

	stale := analyzeUnreachableProductionPackages(nil, admissions)
	assertViolationRule(t, stale, ruleStaleTestToolAdmission)
}

func TestArchitectureContractAllowsActiveTestToolAdmission(t *testing.T) {
	const toolPath = "example.com/project/internal/archguard"
	admissions := map[string]testToolAdmission{
		"internal/archguard": {Reason: "synthetic test/tool admission", Kind: testToolHelperPackage},
	}
	records := []PackageRecord{
		{ImportPath: "example.com/project/cmd/tool", Name: "main"},
		{
			ImportPath: toolPath,
			Name:       "archguard",
			GoFiles:    []string{"report.go"},
			FileContents: map[string]string{
				"report.go": "package archguard\ntype Report struct{}\n",
			},
		},
	}

	if violations := analyzeUnreachableProductionPackages(records, admissions); len(violations) != 0 {
		t.Fatalf("active test/tool admission produced violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractAllowsTestsOnlyPackageAdmission(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/cmd/tool", Name: "main"},
		{
			ImportPath:   "example.com/project/test/cli",
			Name:         "cli_test",
			XTestGoFiles: []string{"help_test.go"},
		},
	}
	admissions := map[string]testToolAdmission{
		"test/cli": {Reason: "synthetic black-box tests", Kind: testToolTestsOnlyPackage},
	}
	if violations := analyzeUnreachableProductionPackages(records, admissions); len(violations) != 0 {
		t.Fatalf("tests-only admission produced violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsTestsOnlyPackageWithProductionSource(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/test/cli",
		Name:       "cli",
		GoFiles:    []string{"helper.go"},
		FileContents: map[string]string{
			"helper.go": "package cli\nfunc Helper() {}\n",
		},
		TestGoFiles: []string{"help_test.go"},
	}}
	admissions := map[string]testToolAdmission{
		"test/cli": {Reason: "synthetic black-box tests", Kind: testToolTestsOnlyPackage},
	}
	violations := analyzeUnreachableProductionPackages(records, admissions)
	assertViolationRule(t, violations, ruleStaleTestToolAdmission)
}

func TestArchitectureContractRejectsImplicitTestToolDescendantAdmission(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/cmd/tool", Name: "main"},
		{
			ImportPath:   "example.com/project/test/cli/future",
			Name:         "future_test",
			XTestGoFiles: []string{"future_test.go"},
		},
	}
	admissions := map[string]testToolAdmission{
		"test/cli": {Reason: "synthetic black-box tests", Kind: testToolTestsOnlyPackage},
	}

	violations := analyzeUnreachableProductionPackages(records, admissions)
	assertViolationRule(t, violations, ruleUnadmittedTestToolPackage)
}

func TestArchitectureContractRejectsUnadmittedTestsOnlyPackage(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/cmd/tool", Name: "main"},
		{
			ImportPath:   "example.com/project/test/future",
			Name:         "future_test",
			XTestGoFiles: []string{"future_test.go"},
		},
	}

	violations := analyzeUnreachableProductionPackages(records, nil)
	assertViolationRule(t, violations, ruleUnadmittedTestToolPackage)
}

func TestArchitectureContractRejectsUnadmittedTopLevelTestHelperAPI(t *testing.T) {
	records := []PackageRecord{
		{ImportPath: "example.com/project/cmd/tool", Name: "main"},
		{
			ImportPath: "example.com/project/test/helper",
			Name:       "helper",
			GoFiles:    []string{"helper.go"},
			FileContents: map[string]string{
				"helper.go": "package helper\nfunc Exported() {}\n",
			},
		},
	}
	violations := analyzeUnreachableProductionPackages(records, nil)
	assertViolationRule(t, violations, ruleUnreachableProductionPackage)
}

func TestArchitectureContractRejectsInvalidOwnerCatalogRows(t *testing.T) {
	violations := validateArchitectureOwnerRows("synthetic", map[string]string{
		"":                   "missing path",
		"internal/tool/*":    "wildcard path",
		"external/tool":      "external path",
		"internal/no-reason": " ",
	})
	if countViolationRule(violations, ruleInvalidArchitectureOwner) != 4 {
		t.Fatalf("invalid owner rows:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractRejectsInvalidTestToolAdmissionKind(t *testing.T) {
	violations := validateTestToolAdmissionRows(map[string]testToolAdmission{
		"test/helper": {Reason: "synthetic invalid shape", Kind: "unknown"},
	})
	assertViolationRule(t, violations, ruleInvalidArchitectureOwner)
}

func TestArchitectureContractRejectsStaleHostOwners(t *testing.T) {
	owners := map[string]string{
		"internal/effect/execute/hostroute": "synthetic private adapter",
	}
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/effect/execute/hostroute",
		GoFiles:    []string{"adapter.go"},
		FileContents: map[string]string{
			"adapter.go": "package hostroute\nconst generic = true\n",
		},
	}}
	violations := analyzeHostSpecialization(records, owners)
	assertViolationRule(t, violations, ruleInvalidArchitectureOwner)
}

func assertViolationRule(t *testing.T, violations []GuardrailFinding, rule string) {
	t.Helper()
	for _, violation := range violations {
		if violation.Rule == rule {
			return
		}
	}
	t.Fatalf("violations = %+v, want rule %q", violations, rule)
}

func countViolationRule(violations []GuardrailFinding, rule string) int {
	count := 0
	for _, violation := range violations {
		if violation.Rule == rule {
			count++
		}
	}
	return count
}
