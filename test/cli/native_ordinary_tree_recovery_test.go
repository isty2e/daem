package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func nativeOrdinaryTreeRecoveryFixture(t *testing.T) (nativefixture.RecoveryFixture, string) {
	t.Helper()
	fixture, _ := nativeOrdinaryRecoveryFixture(t)
	workspace := filepath.Dir(fixture.Manifest)
	manifest := string(testkit.ReadFile(t, fixture.Manifest)) + "\n[[skill]]\nname='sample'\nsource={path='skills/sample',mode='vendor'}\n"
	testkit.WriteFile(t, workspace, filepath.Base(fixture.Manifest), manifest)
	testkit.WriteFile(t, workspace, "skills/sample/SKILL.md", "---\nname: sample\ndescription: before\n---\n")
	testkit.WriteFile(t, workspace, "skills/sample/references/detail.txt", "before\n")
	nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
	nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes", "--verbose")
	testkit.WriteFile(t, workspace, "skills/sample/SKILL.md", "---\nname: sample\ndescription: after\n---\n")
	testkit.WriteFile(t, workspace, "skills/sample/references/detail.txt", "after\n")
	testkit.WriteFile(t, workspace, "preferences.toml", "model='skill-after'\n")
	nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
	return fixture, filepath.Join(workspace, ".agents", "skills", "sample")
}

func TestNativeOrdinaryTreeRecovery(t *testing.T) {
	for _, boundary := range []string{"host", "skill-removal", "return-error", "cancel", "state", "retirement"} {
		t.Run(boundary, func(t *testing.T) {
			fixture, skill := nativeOrdinaryTreeRecoveryFixture(t)
			before := nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, skill, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)
			watched := fixture
			watched.Destination = skill
			code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, watched, boundary, "apply", "--yes", "--verbose")
			if code == 0 || boundary != "return-error" && boundary != "cancel" && code != nativefixture.RecoveryExit {
				t.Fatalf("real tree effect did not reach %s: %d %s %s", boundary, code, stdout, stderr)
			}
			if boundary == "return-error" || boundary == "cancel" {
				if !bytes.Contains([]byte(stderr), []byte(nativefixture.RecoveryReceipt(skill))) {
					t.Fatal("fault preceded actual tree publication", stderr)
				}
			} else {
				classification := "needs_rollback"
				if boundary == "state" {
					classification = "clean_after"
				}
				if boundary == "retirement" {
					classification = "retained_cleanup_residue"
				}
				nativefixture.RequireRecoveryDryRun(t, fixture, classification)
				nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes")
			}
			if boundary != "state" && boundary != "retirement" && !reflect.DeepEqual(before, nativefixture.SnapshotRecoveryPaths(t, fixture.Destination, skill, fixture.Paths.StatefilePath, fixture.Paths.OwnershipRegistryPath)) {
				t.Fatal("joint recovery did not restore the full tree and native Before phase", stderr)
			}
			testkit.AssertNoRecoveryArtifacts(t, filepath.Dir(fixture.Manifest))
			nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
			if testkit.HashDirectory(t, filepath.Join(filepath.Dir(fixture.Manifest), "skills", "sample")) != testkit.HashDirectory(t, skill) {
				t.Fatal("retry omitted part of the skill tree")
			}
			nativefixture.RunRecoveryCommand(t, fixture, "none", "status", "--check", "--json")
		})
	}
}

func TestSkillRecoveryPreservesSurvivingModesAndRestoresRemovedEntryMode(t *testing.T) {
	fixture, skill := nativeOrdinaryTreeRecoveryFixture(t)
	workspace := filepath.Dir(fixture.Manifest)
	detail := filepath.Join(skill, "references", "detail.txt")
	source := filepath.Join(workspace, "skills", "sample")
	extra := filepath.Join(skill, "references", "tool")
	testkit.WriteFile(t, skill, "references/tool", "local tool\n")
	if err := os.Chmod(extra, 0o711); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, skill, "cache/data", "cached bytes\n")
	if err := os.Chmod(detail, 0o745); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "references", "detail.txt")); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, source, "references/tool", "new executable\n")
	if err := os.Chmod(filepath.Join(source, "references", "tool"), 0o775); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, source, "references/readme", "new text\n")
	if err := os.Chmod(filepath.Join(source, "references", "readme"), 0o664); err != nil {
		t.Fatal(err)
	}
	nativefixture.RunRecoveryCommand(t, fixture, "none", "lock")
	watched := fixture
	watched.Destination = skill
	if code, _, stderr := nativefixture.InvokeRecoveryCommand(t, watched, "host", "apply", "--yes", "--verbose"); code != nativefixture.RecoveryExit {
		t.Fatalf("did not reach published tree: %d %s", code, stderr)
	}
	checkMode := func(path string, mode os.FileMode) {
		t.Helper()
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: %v, %v; want %#o", path, info, err, mode)
		}
	}
	checkMode(filepath.Join(skill, "references", "tool"), 0o700)
	checkMode(filepath.Join(skill, "references", "readme"), 0o600)
	file := filepath.Join(skill, "SKILL.md")
	if err := os.Chmod(file, 0o744); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(skill, "references"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(skill, "references"), 0o700) })
	if err := os.Rename(source, source+".unavailable"); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, skill, "after-publication", "keep\n")
	if code, _, _ := nativefixture.InvokeRecoveryCommand(t, fixture, "none", "recover", "--yes"); code == 0 {
		t.Fatal("recovery overwrote a tree changed after publication")
	}
	late := filepath.Join(skill, "after-publication")
	testkit.AssertFileContent(t, late, "keep\n")
	if err := os.Remove(late); err != nil {
		t.Fatal(err)
	}
	nativefixture.RunRecoveryCommand(t, fixture, "none", "recover", "--yes", "--verbose")
	checkMode(extra, 0o711)
	testkit.AssertFileContent(t, extra, "local tool\n")
	testkit.AssertFileContent(t, filepath.Join(skill, "cache", "data"), "cached bytes\n")
	checkMode(file, 0o744)
	checkMode(filepath.Join(skill, "references"), 0o500)
	checkMode(detail, 0o745)
	testkit.AssertFileContent(t, detail, "before\n")
	testkit.AssertFileContent(t, file, "---\nname: sample\ndescription: before\n---\n")
	testkit.AssertNoRecoveryArtifacts(t, workspace)
}
