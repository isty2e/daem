//go:build darwin || linux

package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunApplyRejectsProjectSkillAncestorAliasesWithoutOutsideWrites(t *testing.T) {
	type containmentProbe struct {
		unsafeDestination  string
		untouchedDirectory string
	}
	tests := []struct {
		name        string
		prepareLink func(t *testing.T, root string, outside string) containmentProbe
		wantFailure rootedpath.FailureKind
	}{
		{
			name: "outside ancestor symlink",
			prepareLink: func(t *testing.T, root string, outside string) containmentProbe {
				t.Helper()
				if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
					t.Fatalf("create outside ancestor symlink: %v", err)
				}
				return containmentProbe{
					unsafeDestination:  filepath.Join(outside, "skills", "oracle"),
					untouchedDirectory: outside,
				}
			},
			wantFailure: rootedpath.FailureAncestorSymlink,
		},
		{
			name: "inside ancestor symlink",
			prepareLink: func(t *testing.T, root string, _ string) containmentProbe {
				t.Helper()
				referent := filepath.Join(root, "redirected-agents")
				if err := os.Mkdir(referent, 0o700); err != nil {
					t.Fatalf("create inside ancestor referent: %v", err)
				}
				if err := os.Symlink(referent, filepath.Join(root, ".agents")); err != nil {
					t.Fatalf("create inside ancestor symlink: %v", err)
				}
				return containmentProbe{
					unsafeDestination:  filepath.Join(referent, "skills", "oracle"),
					untouchedDirectory: referent,
				}
			},
			wantFailure: rootedpath.FailureAncestorSymlink,
		},
		{
			name: "dangling ancestor symlink",
			prepareLink: func(t *testing.T, root string, outside string) containmentProbe {
				t.Helper()
				referent := filepath.Join(outside, "missing")
				if err := os.Symlink(referent, filepath.Join(root, ".agents")); err != nil {
					t.Fatalf("create dangling ancestor symlink: %v", err)
				}
				return containmentProbe{
					unsafeDestination:  filepath.Join(referent, "skills", "oracle"),
					untouchedDirectory: outside,
				}
			},
			wantFailure: rootedpath.FailureDanglingAncestorSymlink,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			manifestPath := filepath.Join(root, "daem.toml")
			lockfilePath := filepath.Join(root, "daem.lock.toml")
			testkit.WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
			skillHash := testkit.HashDirectory(t, filepath.Join(root, "skills", "oracle"))
			testkit.WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)
			testkit.WriteLockfile(t, lockfilePath, testkit.ExactSupplyLockfile(t, testkit.ExactSupplyFixture{Kind: testkit.ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: skillHash}))
			probe := tt.prepareLink(t, root, outside)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(
				[]string{"apply", "--manifest", manifestPath, "--yes"},
				&stdout,
				&stderr,
			)

			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			combinedOutput := stdout.String() + stderr.String()
			if !strings.Contains(combinedOutput, string(tt.wantFailure)) {
				t.Fatalf("output = %q, want failure %q", combinedOutput, tt.wantFailure)
			}
			if _, err := os.Lstat(probe.unsafeDestination); !os.IsNotExist(err) {
				t.Fatalf("unsafe destination stat error = %v, want absent", err)
			}
			entries, err := os.ReadDir(probe.untouchedDirectory)
			if err != nil {
				t.Fatalf("read untouched directory: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("blocked apply wrote through alias: %v", entries)
			}
			if _, err := os.Stat(filepath.Join(root, ".daem", "state.json")); !os.IsNotExist(err) {
				t.Fatalf("statefile stat error = %v, want absent", err)
			}
			testkit.AssertNoRecoveryArtifacts(t, root)
		})
	}
}
