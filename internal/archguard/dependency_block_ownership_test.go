package archguard

import (
	"strings"
	"testing"
)

var commandPackageAdmissions = map[string]string{
	"cmd/daem": "primary CLI executable composition root",
}

func TestBuildSelectedProductionPackagesHaveExactlyOneArchitectureClassification(t *testing.T) {
	const modulePath = "github.com/isty2e/daem/"

	seenCommands := make(map[string]bool, len(commandPackageAdmissions))
	for _, record := range loadRepoPackageRecords(t) {
		if len(productionFiles(record)) == 0 {
			continue
		}
		if !strings.HasPrefix(record.ImportPath, modulePath) {
			t.Fatalf("repository package %q is outside module %q", record.ImportPath, modulePath)
		}
		packagePath := strings.TrimPrefix(record.ImportPath, modulePath)

		switch {
		case strings.HasPrefix(packagePath, "internal/"):
			blocks := semanticDependencyBlocksForPackage(packagePath)
			if len(blocks) != 1 {
				t.Errorf("%s belongs to %d semantic dependency blocks, want exactly one", packagePath, len(blocks))
			}
		case strings.HasPrefix(packagePath, "test/"):
			if _, admitted := testToolPackageAdmissions[packagePath]; !admitted {
				t.Errorf("%s has no exact test/tool architecture admission", packagePath)
			}
		case strings.HasPrefix(packagePath, "cmd/"):
			if _, admitted := commandPackageAdmissions[packagePath]; !admitted {
				t.Errorf("%s has no exact command-root architecture admission", packagePath)
			} else {
				seenCommands[packagePath] = true
			}
		default:
			t.Errorf("%s is outside the admitted command, internal, and test architecture roots", packagePath)
		}
	}
	for packagePath := range commandPackageAdmissions {
		if !seenCommands[packagePath] {
			t.Errorf("command-root architecture admission %q is stale", packagePath)
		}
	}
}

func TestSemanticNamespaceRootsCannotBecomePackages(t *testing.T) {
	for _, packagePath := range []string{"internal/effect", "internal/supply"} {
		t.Run(packagePath, func(t *testing.T) {
			blocks := semanticDependencyBlocksForPackage(packagePath)
			if len(blocks) != 0 {
				t.Fatalf("semantic namespace root %q belongs to %d dependency blocks, want none", packagePath, len(blocks))
			}

			report := FormatReport(AnalyzeRecords([]PackageRecord{{
				ImportPath: "example.com/project/" + packagePath,
			}}))
			if !strings.Contains(report, ruleSemanticBlockOwnership+": "+packagePath) {
				t.Fatalf("semantic namespace root %q became an admitted package:\n%s", packagePath, report)
			}
		})
	}
}

func TestSemanticDependencyBlockClassifiesBoundaryAndStableLeafPackagesExplicitly(t *testing.T) {
	for _, packagePath := range []string{
		"internal/encoding/jsonstrict",
		"internal/output",
		"internal/output/ownership",
		"internal/output/ownership/store",
		"internal/realization/aggregate/codec",
		"internal/realization/aggregate/codec/hook",
		"internal/realization/aggregate/codec/mcp",
		"internal/realization/lock/snapshottest",
		"internal/realization/lockfile",
		"internal/target",
		"internal/target/availability",
		"internal/target/selection",
		"internal/workflow/lock/generate",
	} {
		if got := semanticDependencyBlockForPackage(packagePath); got != dependencyBoundary {
			t.Errorf("%s block = %d, want boundary", packagePath, got)
		}
	}
}

func TestSemanticDependencyBlockClassifiesOwnerLocalRealizationKernelsExplicitly(t *testing.T) {
	for _, packagePath := range []string{
		"internal/realization",
		"internal/realization/aggregate",
		"internal/realization/aggregate/hook",
		"internal/realization/delegate",
		"internal/realization/delegate/mcp",
		"internal/realization/lock",
		"internal/realization/lock/build",
		"internal/realization/lock/refine",
		"internal/realization/profile",
		"internal/realization/relation",
	} {
		if got := semanticDependencyBlockForPackage(packagePath); got != dependencyRealization {
			t.Errorf("%s block = %d, want realization", packagePath, got)
		}
	}
}

func TestSemanticDependencyBlockClassifiesSupplyAndEffectNeighborhoodsExactly(t *testing.T) {
	expected := map[semanticDependencyBlock][]string{
		dependencySupply: {
			"internal/supply/artifact",
			"internal/supply/artifact/access",
			"internal/supply/compat/skill",
			"internal/supply/compat/skill/repair",
			"internal/supply/source",
			"internal/supply/source/acquisition",
			"internal/supply/source/archive",
			"internal/supply/source/directfile",
		},
		dependencyEffect: {
			"internal/effect/execute",
			"internal/effect/journal",
			"internal/effect/journal/recovery",
			"internal/effect/mutation",
			"internal/effect/mutation/filesystem",
			"internal/effect/mutation/filesystem/artifactstage",
			"internal/effect/mutation/ownership",
			"internal/effect/mutation/rootedpath",
			"internal/effect/payload",
		},
		dependencyBoundary: {
			"internal/supply/source/backend/gitcli",
			"internal/supply/source/backend/localfs",
			"internal/supply/source/backend/s3object",
			"internal/supply/source/cache",
			"internal/supply/source/resolution",
			"internal/supply/source/sourcetest",
			"internal/effect/execute/configrelation",
			"internal/effect/execute/delegate",
			"internal/effect/execute/hostroute",
			"internal/effect/payload/build",
			"internal/effect/storage/carrierclaim",
			"internal/effect/storage/commit",
		},
	}

	for block, packagePaths := range expected {
		for _, packagePath := range packagePaths {
			blocks := semanticDependencyBlocksForPackage(packagePath)
			if len(blocks) != 1 || blocks[0] != block {
				t.Errorf("%s blocks = %v, want only %d", packagePath, blocks, block)
			}
		}
	}
}

func TestEffectStorageBoundaryAdmissionIsExact(t *testing.T) {
	for _, packagePath := range []string{
		"internal/effect/storage/carrierclaim",
		"internal/effect/storage/commit",
	} {
		if got := semanticDependencyBlockForPackage(packagePath); got != dependencyBoundary {
			t.Errorf("%s block = %d, want boundary", packagePath, got)
		}
	}

	for _, packagePath := range []string{
		"internal/effect/storage",
		"internal/effect/storage/carrierclaim/detail",
		"internal/effect/storage/index",
		"internal/effect/storage/commit/detail",
	} {
		if got := semanticDependencyBlockForPackage(packagePath); got != dependencyUnknown {
			t.Errorf("%s block = %d, want unknown", packagePath, got)
		}
	}
}

func TestSemanticDependencyBlockRejectsUnclassifiedFuturePackage(t *testing.T) {
	const packagePath = "internal/future"
	const repositoryModulePath = "github.com/isty2e/daem/"
	if got := semanticDependencyBlockForPackage(packagePath); got != dependencyUnknown {
		t.Fatalf("%s block = %d, want unknown", packagePath, got)
	}

	findings := analyzeArchitectureDependencyDirections([]PackageRecord{{
		ImportPath: repositoryModulePath + packagePath,
	}})
	if len(findings) != 1 {
		t.Fatalf("findings = %#v, want one ownership violation", findings)
	}
	if findings[0].Rule != ruleSemanticBlockOwnership {
		t.Fatalf("rule = %q, want %q", findings[0].Rule, ruleSemanticBlockOwnership)
	}
}
