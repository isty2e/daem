package cli_test

import (
	"bytes"
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
	nativefixture.RunRecoveryCommand(t, fixture, "none", "apply", "--yes")
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
