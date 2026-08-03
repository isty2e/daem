package archguard

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestMandatoryTestLanesOwnEveryTestPackage(t *testing.T) {
	root := findRepoRoot(t)
	allTestPackages := testPackagePaths(loadRepoPackageRecords(t))
	ownedBy := make(map[string][]string, len(allTestPackages))

	for _, lane := range []string{"full", "race", "repository"} {
		selectors := testLanePackageSelectors(t, root, lane)
		for _, packagePath := range testPackagePaths(loadSelectedPackageRecords(t, root, selectors)) {
			ownedBy[packagePath] = append(ownedBy[packagePath], lane)
		}
	}

	var unowned []string
	for _, packagePath := range allTestPackages {
		if len(ownedBy[packagePath]) == 0 {
			unowned = append(unowned, packagePath)
		}
	}
	if len(unowned) != 0 {
		t.Fatalf("test packages without a mandatory lane owner: %s", strings.Join(unowned, ", "))
	}
}

func TestRepositoryLaneOwnsOnlyRepositoryContracts(t *testing.T) {
	root := findRepoRoot(t)
	selectors := testLanePackageSelectors(t, root, "repository")
	got := testPackagePaths(loadSelectedPackageRecords(t, root, selectors))
	want := []string{"github.com/isty2e/daem/internal/archguard"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("repository lane packages = %v, want %v", got, want)
	}
}

func TestRaceLaneKeepsProductAndCLICoverageWithoutRepositoryTooling(t *testing.T) {
	root := findRepoRoot(t)
	selectors := testLanePackageSelectors(t, root, "race")
	got := testPackagePaths(loadSelectedPackageRecords(t, root, selectors))
	excluded := map[string]struct{}{
		"github.com/isty2e/daem/internal/archguard": {},
		"github.com/isty2e/daem/test/tooling":       {},
	}
	want := make([]string, 0)
	for _, packagePath := range testPackagePaths(loadRepoPackageRecords(t)) {
		if _, skip := excluded[packagePath]; !skip {
			want = append(want, packagePath)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("race lane test packages = %v, want every test package except %v: %v", got, excluded, want)
	}
}

func testLanePackageSelectors(t *testing.T, root string, lane string) []string {
	t.Helper()
	command := exec.Command(filepath.Join(root, "tools", "test.sh"), "packages", lane)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect %s lane packages: %v\n%s", lane, err, output)
	}
	selectors := strings.Fields(string(output))
	if len(selectors) == 0 {
		t.Fatalf("%s lane has no package selectors", lane)
	}
	return selectors
}

func loadSelectedPackageRecords(t *testing.T, root string, selectors []string) []PackageRecord {
	t.Helper()
	arguments := append([]string{"list", "-json"}, selectors...)
	command := exec.Command("go", arguments...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list %v failed: %v\n%s", selectors, err, output)
	}
	records, err := ParseGoListJSON(output)
	if err != nil {
		t.Fatalf("parse go list for %v: %v", selectors, err)
	}
	return records
}

func testPackagePaths(records []PackageRecord) []string {
	paths := make([]string, 0, len(records))
	for _, record := range records {
		if len(record.TestGoFiles) == 0 && len(record.XTestGoFiles) == 0 {
			continue
		}
		paths = append(paths, record.ImportPath)
	}
	sort.Strings(paths)
	return paths
}
