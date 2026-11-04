package archguard

import "testing"

func TestNativeFragmentCompilerRemainsACompositionBoundary(t *testing.T) {
	const compiler = "example.com/project/internal/realization/lock/nativefragment"
	for _, kernel := range []string{
		"internal/realization/lock",
		"internal/realization/profile",
		"internal/reconcile",
	} {
		findings := analyzeArchitectureDependencyDirections([]PackageRecord{{ImportPath: "example.com/project/" + kernel, Imports: []string{compiler}}})
		if len(findings) == 0 {
			t.Fatalf("semantic kernel %q admitted raw-source interpretation", kernel)
		}
	}
	for _, boundary := range []string{
		"internal/realization/lock/build",
		"internal/assurance/observe/lock",
	} {
		findings := analyzeArchitectureDependencyDirections([]PackageRecord{{ImportPath: "example.com/project/" + boundary, Imports: []string{compiler}}})
		if len(findings) != 0 {
			t.Fatalf("source composition boundary %q cannot share its compiler:\n%s", boundary, FormatReport(findings))
		}
	}
	if findings := analyzeImports("internal/realization/lock/refine", []string{compiler}); len(findings) == 0 {
		t.Fatal("static Desired-to-Topology refinement acquired source interpretation")
	}
}
