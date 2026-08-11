package archguard

import "testing"

func TestAnalyzeImportsEmitsStableResidualRuleNames(t *testing.T) {
	cases := []struct {
		name        string
		packagePath string
		importPath  string
		wantRule    string
	}{
		{
			name:        "cli direct phase import",
			packagePath: "internal/cli",
			importPath:  "example.com/project/internal/effect/payload",
			wantRule:    ruleCLIDirectPhaseImport,
		},
		{
			name:        "lock build boundary import",
			packagePath: "internal/realization/lock/build",
			importPath:  "example.com/project/internal/manifest",
			wantRule:    ruleLockBuildBoundaryImport,
		},
		{
			name:        "lock build diagnose import",
			packagePath: "internal/realization/lock/build",
			importPath:  "example.com/project/internal/diagnose",
			wantRule:    ruleLockBuildBoundaryImport,
		},
		{
			name:        "lock build output project import",
			packagePath: "internal/realization/lock/build",
			importPath:  "example.com/project/internal/output/project/skill",
			wantRule:    ruleLockBuildBoundaryImport,
		},
		{
			name:        "lock build payload import",
			packagePath: "internal/realization/lock/build",
			importPath:  "example.com/project/internal/effect/payload/skill",
			wantRule:    ruleLockBuildBoundaryImport,
		},
		{
			name:        "lock build statefile import",
			packagePath: "internal/realization/lock/build",
			importPath:  "example.com/project/internal/assurance/statefile",
			wantRule:    ruleLockBuildBoundaryImport,
		},
		{
			name:        "lock build journal pathstate import",
			packagePath: "internal/realization/lock/build",
			importPath:  "example.com/project/internal/effect/journal/recovery",
			wantRule:    ruleLockBuildBoundaryImport,
		},
		{
			name:        "lock generate mutation import",
			packagePath: "internal/workflow/lock/generate",
			importPath:  "example.com/project/internal/effect/mutation",
			wantRule:    ruleLockGenerateBoundaryImport,
		},
		{
			name:        "lock generate workflow import",
			packagePath: "internal/workflow/lock/generate",
			importPath:  "example.com/project/internal/workflow/lock",
			wantRule:    ruleLockGenerateBoundaryImport,
		},
		{
			name:        "lock generate unknown phase import",
			packagePath: "internal/workflow/lock/generate",
			importPath:  "example.com/project/internal/futurephase",
			wantRule:    ruleLockGenerateBoundaryImport,
		},
		{
			name:        "canonical lock lockfile import",
			packagePath: "internal/realization/lock",
			importPath:  "example.com/project/internal/realization/lockfile",
			wantRule:    ruleLockCanonicalLockfileImport,
		},
		{
			name:        "lockfile behavior import",
			packagePath: "internal/realization/lockfile",
			importPath:  "example.com/project/internal/workflow/status",
			wantRule:    ruleLockfileBehaviorImport,
		},
		{
			name:        "lockfile generation import",
			packagePath: "internal/realization/lockfile",
			importPath:  "example.com/project/internal/workflow/lock/generate",
			wantRule:    ruleLockfileBehaviorImport,
		},
		{
			name:        "observe plan import",
			packagePath: "internal/assurance/observe/live",
			importPath:  "example.com/project/internal/reconcile",
			wantRule:    ruleObserveReconciliationImport,
		},
		{
			name:        "statefile behavior import",
			packagePath: "internal/assurance/statefile",
			importPath:  "example.com/project/internal/assurance/observe/live",
			wantRule:    ruleStatefileBehaviorImport,
		},
		{
			name:        "assurance imports effect",
			packagePath: "internal/assurance/hostroute",
			importPath:  "example.com/project/internal/effect/execute",
			wantRule:    ruleAssuranceEffectImport,
		},
		{
			name:        "host route effect imports assurance",
			packagePath: "internal/effect/execute/hostroute",
			importPath:  "example.com/project/internal/assurance/hostroute",
			wantRule:    ruleHostRouteCommandAssurance,
		},
		{
			name:        "paths internal import",
			packagePath: "internal/paths",
			importPath:  "example.com/project/internal/target",
			wantRule:    rulePathsInternalImport,
		},
		{
			name:        "journal recovery effect import",
			packagePath: "internal/effect/journal/recovery",
			importPath:  "example.com/project/internal/effect/storage/commit",
			wantRule:    ruleJournalRecoveryImport,
		},
		{
			name:        "manifest bridge import",
			packagePath: "internal/workflow/status",
			importPath:  "example.com/project/internal/manifest",
			wantRule:    ruleManifestBridgeImport,
		},
		{
			name:        "workflow reverse import",
			packagePath: "internal/target",
			importPath:  "example.com/project/internal/workflow/status",
			wantRule:    ruleWorkflowReverseImport,
		},
		{
			name:        "workflow nested import",
			packagePath: "internal/workflow/apply",
			importPath:  "example.com/project/internal/workflow/status",
			wantRule:    ruleWorkflowNestedImport,
		},
		{
			name:        "unadmitted presentation workflow import",
			packagePath: "internal/cli/present",
			importPath:  "example.com/project/internal/workflow/readiness",
			wantRule:    rulePresentWorkflowImport,
		},
		{
			name:        "unadmitted diagnostic workflow import",
			packagePath: "internal/cli/present",
			importPath:  "example.com/project/internal/workflow/diagnose",
			wantRule:    rulePresentWorkflowImport,
		},
		{
			name:        "canonical lock refinement reverse import",
			packagePath: "internal/realization/lock",
			importPath:  "example.com/project/internal/realization/lock/refine",
			wantRule:    ruleLockCanonicalReverseImport,
		},
		{
			name:        "lock refinement assembly import",
			packagePath: "internal/realization/lock/refine",
			importPath:  "example.com/project/internal/realization/lock/build",
			wantRule:    ruleLockRefineBoundaryImport,
		},
		{
			name:        "lock refinement acquisition import",
			packagePath: "internal/realization/lock/refine",
			importPath:  "example.com/project/internal/supply/source/acquisition",
			wantRule:    ruleLockRefineBoundaryImport,
		},
		{
			name:        "lock refinement artifact access import",
			packagePath: "internal/realization/lock/refine",
			importPath:  "example.com/project/internal/supply/artifact/access",
			wantRule:    ruleLockRefineBoundaryImport,
		},
		{
			name:        "lock refinement concrete codec import",
			packagePath: "internal/realization/lock/refine",
			importPath:  "example.com/project/internal/realization/aggregate/codec/mcp",
			wantRule:    ruleLockRefineBoundaryImport,
		},
		{
			name:        "lock refinement operating system import",
			packagePath: "internal/realization/lock/refine",
			importPath:  "os",
			wantRule:    ruleLockRefineBoundaryImport,
		},
		{
			name:        "lock refinement third party import",
			packagePath: "internal/realization/lock/refine",
			importPath:  "example.net/effectful",
			wantRule:    ruleLockRefineBoundaryImport,
		},
		{
			name:        "lockfile refinement import",
			packagePath: "internal/realization/lockfile",
			importPath:  "example.com/project/internal/realization/lock/refine",
			wantRule:    ruleLockfileBehaviorImport,
		},
		{
			name:        "surface profile workflow import",
			packagePath: "internal/realization",
			importPath:  "example.com/project/internal/workflow/status",
			wantRule:    ruleSurfaceProfileBoundary,
		},
		{
			name:        "surface profile lock import",
			packagePath: "internal/realization",
			importPath:  "example.com/project/internal/realization/lock",
			wantRule:    ruleSurfaceProfileBoundary,
		},
		{
			name:        "surface aggregate output import",
			packagePath: "internal/realization/aggregate",
			importPath:  "example.com/project/internal/output/project/mcp",
			wantRule:    ruleSurfaceHardFamilyBoundary,
		},
		{
			name:        "surface commandhook lifecycle import",
			packagePath: "internal/realization/aggregate/hook",
			importPath:  "example.com/project/internal/lifecycle",
			wantRule:    ruleSurfaceHardFamilyBoundary,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations := analyzeImports(tc.packagePath, []string{tc.importPath})
			if !containsFindingRule(violations, tc.wantRule) {
				t.Fatalf("violations = %+v, want rule %q", violations, tc.wantRule)
			}
		})
	}
}

func TestWorkflowLockGenerateCompositionEdgesAreExact(t *testing.T) {
	for _, packagePath := range []string{
		"internal/workflow/authoring",
		"internal/workflow/lock",
	} {
		violations := analyzeImports(packagePath, []string{
			"example.com/project/internal/workflow/lock/generate",
		})
		for _, violation := range violations {
			if violation.Rule == ruleWorkflowNestedImport {
				t.Errorf("%s -> lock/generate rejected: %#v", packagePath, violations)
			}
		}
	}

	for _, testCase := range []struct {
		packagePath string
		importPath  string
	}{
		{packagePath: "internal/workflow/apply", importPath: "internal/workflow/lock/generate"},
		{packagePath: "internal/workflow/authoring", importPath: "internal/workflow/status"},
		{packagePath: "internal/workflow/lock", importPath: "internal/workflow/authoring"},
	} {
		violations := analyzeImports(testCase.packagePath, []string{"example.com/project/" + testCase.importPath})
		assertViolationRule(t, violations, ruleWorkflowNestedImport)
	}
}

func TestWorkflowReadinessCompositionEdgesAreExact(t *testing.T) {
	for _, packagePath := range []string{
		"internal/workflow/apply",
		"internal/workflow/list",
		"internal/workflow/status",
	} {
		violations := analyzeImports(packagePath, []string{
			"example.com/project/internal/workflow/readiness",
		})
		for _, violation := range violations {
			if violation.Rule == ruleWorkflowNestedImport {
				t.Errorf("%s -> readiness rejected: %#v", packagePath, violations)
			}
		}
	}

	for _, packagePath := range []string{
		"internal/workflow/authoring",
		"internal/workflow/diagnose",
		"internal/workflow/lock",
	} {
		violations := analyzeImports(packagePath, []string{
			"example.com/project/internal/workflow/readiness",
		})
		assertViolationRule(t, violations, ruleWorkflowNestedImport)
	}
}

func TestExecuteMayConsumeCanonicalLockedRealizationButNotLockBehavior(t *testing.T) {
	if violations := analyzeImports(
		"internal/effect/execute/hostroute",
		[]string{"example.com/project/internal/realization/lock"},
	); len(violations) != 0 {
		t.Fatalf("locked realization import violations = %+v", violations)
	}

	for _, importPath := range []string{
		"example.com/project/internal/realization/lock/build",
		"example.com/project/internal/workflow/lock/generate",
	} {
		violations := analyzeImports("internal/effect/execute/hostroute", []string{importPath})
		if !containsFindingRule(
			violations,
			"journal or execute package imports forbidden phase: lock build",
		) {
			t.Fatalf("lock behavior import %q violations = %+v", importPath, violations)
		}
	}
}

func TestStatefileMayConsumeCanonicalRelationSummariesButNotRelationAdapters(t *testing.T) {
	if violations := analyzeImports(
		"internal/assurance/statefile",
		[]string{"example.com/project/internal/assurance/observe/relation"},
	); len(violations) != 0 {
		t.Fatalf("canonical relation summary import violations = %+v", violations)
	}

	for _, importPath := range []string{
		"example.com/project/internal/assurance/observe/relation/host",
		"example.com/project/internal/assurance/observe/live",
	} {
		violations := analyzeImports("internal/assurance/statefile", []string{importPath})
		if !containsFindingRule(violations, ruleStatefileBehaviorImport) {
			t.Fatalf(
				"statefile relation adapter import %q violations = %+v, want %q",
				importPath,
				violations,
				ruleStatefileBehaviorImport,
			)
		}
	}
}

func TestProgressPresentationMayConsumeOnlyLockWorkflowFacts(t *testing.T) {
	if violations := analyzeImports(
		"internal/cli/present/progress",
		[]string{"example.com/project/internal/workflow/lock"},
	); containsFindingRule(violations, rulePresentWorkflowImport) {
		t.Fatalf("lock workflow fact import violations = %+v", violations)
	}

	for _, importPath := range []string{
		"example.com/project/internal/workflow/apply",
		"example.com/project/internal/workflow/lock/generate",
		"example.com/project/internal/workflow/status",
	} {
		violations := analyzeImports("internal/cli/present/progress", []string{importPath})
		if !containsFindingRule(violations, rulePresentWorkflowImport) {
			t.Fatalf(
				"progress workflow import %q violations = %+v, want %q",
				importPath,
				violations,
				rulePresentWorkflowImport,
			)
		}
	}
}

func TestLockGenerateBoundaryAllowsFrozenDependencies(t *testing.T) {
	for _, importPath := range []string{
		"example.com/project/internal/supply/artifact",
		"example.com/project/internal/desired/skill",
		"example.com/project/internal/realization/lock/build",
		"example.com/project/internal/realization/lock",
		"example.com/project/internal/realization/lockfile",
		"example.com/project/internal/paths",
		"example.com/project/internal/supply/source/backend/localfs",
		"example.com/project/internal/realization/aggregate/hook",
	} {
		violations := analyzeImports("internal/workflow/lock/generate", []string{importPath})
		for _, violation := range violations {
			if violation.Rule == ruleLockGenerateBoundaryImport {
				t.Fatalf("allowed import %q produced %s", importPath, violation.Rule)
			}
		}
	}

	violations := analyzeImports(
		"internal/workflow/lock/generate",
		[]string{"example.com/project/internal/realization/aggregate/hook/privatecodec"},
	)
	if !containsFindingRule(violations, ruleLockGenerateBoundaryImport) {
		t.Fatalf("commandhook descendant violations = %+v, want %s", violations, ruleLockGenerateBoundaryImport)
	}
}

func TestLockOwnershipGuardAllowsCanonicalDirectionAndPathLookalikes(t *testing.T) {
	cases := []struct {
		packagePath string
		importPath  string
	}{
		{packagePath: "internal/realization/lock/refine", importPath: "example.com/project/internal/realization/lock"},
		{packagePath: "internal/realization/lock/refine", importPath: "example.com/project/internal/supply/artifact"},
		{packagePath: "internal/realization/lock/refine", importPath: "example.com/project/internal/desired/skill"},
		{packagePath: "internal/realization/lock/refine", importPath: "example.com/project/internal/realization/aggregate"},
		{packagePath: "internal/realization/lock/refine", importPath: "example.com/project/internal/topology/mcp"},
		{packagePath: "internal/realization/lock/refine", importPath: "fmt"},
		{packagePath: "internal/realization/lock/refine", importPath: "path"},
		{packagePath: "internal/realization/lock/refine", importPath: "sort"},
		{packagePath: "internal/realization/lock/refine", importPath: "strings"},
		{packagePath: "internal/realization/lockfile", importPath: "example.com/project/internal/realization/lock"},
		{packagePath: "internal/realization/lock", importPath: "example.com/project/internal/locksmith"},
	}
	for _, tc := range cases {
		violations := analyzeImports(tc.packagePath, []string{tc.importPath})
		for _, violation := range violations {
			if violation.Rule == ruleLockCanonicalReverseImport ||
				violation.Rule == ruleLockRefineBoundaryImport ||
				violation.Rule == ruleLockfileBehaviorImport {
				t.Fatalf("allowed import %s -> %s produced %+v", tc.packagePath, tc.importPath, violation)
			}
		}
	}
}

func TestAnalyzeForbiddenShapesEmitsStableResidualRuleNames(t *testing.T) {
	violations := analyzeForbiddenShapes("internal/state", PackageRecord{})
	if !containsFindingRule(violations, ruleForbiddenStatePackageShape) {
		t.Fatalf("violations = %+v, want rule %q", violations, ruleForbiddenStatePackageShape)
	}
}

func containsFindingRule(findings []GuardrailFinding, rule string) bool {
	for _, finding := range findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}
