package archguard

import (
	"path"
	"testing"
)

func TestArchitectureContractRejectsGenericHostReferences(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/workflow/apply",
		GoFiles:    []string{"synthetic.go"},
		FileContents: map[string]string{
			"synthetic.go": `package apply
import targetalias "example.com/project/internal/target"
var selected = targetalias.TargetSynthetic
`,
		},
	}}
	violations := analyzeHostSpecialization(records, nil)
	assertViolationRule(t, violations, ruleGenericHostReference)
}

func TestArchitectureContractGuardsGenericEffectAndPersistencePackages(t *testing.T) {
	for _, packagePath := range []string{
		"internal/effect/execute",
		"internal/effect/journal",
		"internal/assurance/statefile",
	} {
		t.Run(packagePath, func(t *testing.T) {
			records := []PackageRecord{{
				ImportPath: "example.com/project/" + packagePath,
				GoFiles:    []string{"synthetic.go"},
				FileContents: map[string]string{
					"synthetic.go": "package synthetic\nconst host = \"claude\"\n",
				},
			}}
			violations := analyzeHostSpecialization(records, nil)
			assertViolationRule(t, violations, ruleGenericHostReference)
		})
	}
}

func TestArchitectureContractRejectsHostNamedGenericFilesAndDotImportedTargets(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/workflow/apply",
		GoFiles:    []string{"claude.go", "dot_target.go"},
		FileContents: map[string]string{
			"claude.go": `package apply
const route = "generic"
`,
			"dot_target.go": `package apply
import . "example.com/project/internal/target"
var _ = TargetSynthetic
`,
		},
	}}
	violations := analyzeHostSpecialization(records, nil)
	if countViolationRule(violations, ruleGenericHostReference) != 2 {
		t.Fatalf("host path and dot-import violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractHostGuardIgnoresCommentsTestsAndLookalikes(t *testing.T) {
	records := []PackageRecord{{
		ImportPath:  "example.com/project/internal/workflow/apply",
		GoFiles:     []string{"clean.go"},
		TestGoFiles: []string{"host_test.go"},
		FileContents: map[string]string{
			"clean.go": `package apply
// Claude and TargetSynthetic are documentation only.
func Pipeline(value string) string { return value }
func openCodec(value string) string { return value }
`,
			"host_test.go": `package apply
const host = "claude"
`,
		},
	}}
	if violations := analyzeHostSpecialization(records, nil); len(violations) != 0 {
		t.Fatalf("host guard reported comments, tests, or lookalikes:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractHostGuardIncludesCgoProductionFiles(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/workflow/apply",
		CgoFiles:   []string{"host.go"},
		FileContents: map[string]string{
			"host.go": "package apply\nconst command = \"claude\"\n",
		},
	}}
	violations := analyzeHostSpecialization(records, nil)
	assertViolationRule(t, violations, ruleGenericHostReference)
}

func TestArchitectureContractHostGuardIgnoresAdmittedTestTools(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/realization/lock/snapshottest",
		GoFiles:    []string{"fixture.go"},
		FileContents: map[string]string{
			"fixture.go": "package snapshottest\nconst claudeFixture = \"claude\"\n",
		},
	}}
	if violations := analyzeHostSpecialization(records, nil); len(violations) != 0 {
		t.Fatalf("host guard reported admitted test/tool support:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractHostOwnerAppliesToItsWholeExactPackage(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/effect/execute/hostroute",
		GoFiles:    []string{"adapter.go", "neighbor.go"},
		FileContents: map[string]string{
			"adapter.go":  "package hostroute\nconst claudeCommand = \"claude\"\n",
			"neighbor.go": "package hostroute\nconst codexCommand = \"codex\"\n",
		},
	}}
	owners := map[string]string{
		"internal/effect/execute/hostroute": "synthetic private adapter",
	}
	if violations := analyzeHostSpecialization(records, owners); len(violations) != 0 {
		t.Fatalf("exact owner package produced violations:\n%s", FormatReport(violations))
	}
}

func TestArchitectureContractHostOwnerDoesNotAdmitDescendantPackages(t *testing.T) {
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/effect/execute/hostroute",
			GoFiles:    []string{"adapter.go"},
			FileContents: map[string]string{
				"adapter.go": "package hostroute\nconst claudeCommand = \"claude\"\n",
			},
		},
		{
			ImportPath: "example.com/project/internal/effect/execute/hostroute/detail",
			GoFiles:    []string{"leak.go"},
			FileContents: map[string]string{
				"leak.go": "package detail\nconst codexCommand = \"codex\"\n",
			},
		},
	}
	owners := map[string]string{
		"internal/effect/execute/hostroute": "synthetic private adapter",
	}
	violations := analyzeHostSpecialization(records, owners)
	if len(violations) != 1 || violations[0].PackagePath != "internal/effect/execute/hostroute/detail" {
		t.Fatalf("descendant owner violations = %+v", violations)
	}
}

func TestArchitectureContractLockedRealizationOwnerRejectsHostBranches(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/realization/lock",
		GoFiles:    []string{"spec.go"},
		FileContents: map[string]string{
			"spec.go": `package lock
func selected(target string) bool {
	if target == "claude" {
		return true
	}
	return false
}
`,
		},
	}}
	owners := map[string]string{
		"internal/realization/lock": "synthetic static locked realization profiles",
	}
	violations := analyzeHostSpecialization(records, owners)
	assertViolationRule(t, violations, ruleGenericHostReference)
}

func TestArchitectureContractHostGuardConfinesRelationObservation(t *testing.T) {
	content := "package relation\nconst command = \"claude\"\n"
	records := []PackageRecord{
		{
			ImportPath: "example.com/project/internal/assurance/observe/relation",
			GoFiles:    []string{"leak.go"},
			FileContents: map[string]string{
				"leak.go": content,
			},
		},
		{
			ImportPath: "example.com/project/internal/assurance/observe/relation/host",
			GoFiles:    []string{"adapter.go"},
			FileContents: map[string]string{
				"adapter.go": content,
			},
		},
	}
	owners := map[string]string{
		"internal/assurance/observe/relation/host": "synthetic private relation observer",
	}
	violations := analyzeHostSpecialization(records, owners)
	if len(violations) != 1 || violations[0].PackagePath != "internal/assurance/observe/relation" {
		t.Fatalf("relation observation boundary violations = %+v", violations)
	}
}

func TestArchitectureContractRejectsCLIAndPresentationHostBranches(t *testing.T) {
	for _, record := range loadRepoPackageRecords(t) {
		packagePath, internal := internalPath(record.ImportPath)
		if !internal || !isPackageOrChild(packagePath, "internal/cli") {
			continue
		}
		for _, fileName := range productionFiles(record) {
			content, ok := packageFileContent(record, fileName)
			if !ok {
				t.Fatalf("read production source %s/%s", packagePath, fileName)
			}
			descriptors, err := hostBranchDescriptors(path.Join(packagePath, fileName), content)
			if err != nil {
				t.Fatalf("inspect production source %s/%s: %v", packagePath, fileName, err)
			}
			for _, descriptor := range descriptors {
				t.Errorf("%s/%s owns host-specific semantic control flow: %s", packagePath, fileName, descriptor)
			}
		}
	}
}

func TestHostBranchGuardDistinguishesDisplayTextFromSemanticControlFlow(t *testing.T) {
	content := []byte(`package boundary
const help = "use --target claude-code"
func selected(host string) bool {
	if host == "claude" {
		return true
	}
	return false
}
`)
	descriptors, err := hostBranchDescriptors("internal/cli/present/fixture.go", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptors) != 1 {
		t.Fatalf("host branch descriptors = %#v, want one semantic branch and no display-text finding", descriptors)
	}
}

func TestArchitectureContractHostGuardFailsClosedOnMalformedSource(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/workflow/apply",
		GoFiles:    []string{"broken.go"},
		FileContents: map[string]string{
			"broken.go": "package apply\nfunc {",
		},
	}}
	violations := analyzeHostSpecialization(records, nil)
	assertViolationRule(t, violations, ruleArchitectureSourceUnreadable)
}

func TestArchitectureContractRejectsUnknownTypedTargetLiteral(t *testing.T) {
	tests := map[string]string{
		"typed value": `package apply
import targetalias "example.com/project/internal/target"
const selected targetalias.Target = "synthetic-host"
`,
		"conversion": `package apply
import targetalias "example.com/project/internal/target"
var selected = targetalias.Target("synthetic-host")
`,
		"parser": `package apply
import targetalias "example.com/project/internal/target"
var selected, _ = targetalias.ParseTarget("synthetic-host")
`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			records := []PackageRecord{{
				ImportPath: "example.com/project/internal/workflow/apply",
				GoFiles:    []string{"typed_literal.go"},
				FileContents: map[string]string{
					"typed_literal.go": content,
				},
			}}
			violations := analyzeHostSpecialization(records, nil)
			assertViolationRule(t, violations, ruleGenericHostReference)
		})
	}
}

func TestArchitectureContractAllowsTypedTargetZeroValue(t *testing.T) {
	records := []PackageRecord{{
		ImportPath: "example.com/project/internal/effect/execute",
		GoFiles:    []string{"zero.go"},
		FileContents: map[string]string{
			"zero.go": `package execute
import targetalias "example.com/project/internal/target"
var previous = targetalias.Target("")
`,
		},
	}}
	if violations := analyzeHostSpecialization(records, nil); len(violations) != 0 {
		t.Fatalf("target zero value produced host-specialization violations:\n%s", FormatReport(violations))
	}
}
