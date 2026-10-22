package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestSymlinkDeclarationAuthoringKeepsLogicalWorkspace(t *testing.T) {
	for _, parentLink := range []bool{false, true} {
		for _, manifestLink := range []bool{false, true} {
			for _, lockLink := range []bool{false, true} {
				t.Run(fmt.Sprintf("parent=%t/manifest=%t/lock=%t", parentLink, manifestLink, lockLink), func(t *testing.T) {
					root := t.TempDir()
					testkit.SetDefaultRootEnv(t, filepath.Join(root, "homes"))
					project := filepath.Join(root, "project")
					dot := filepath.Join(root, "dot")
					for _, dir := range []string{project, dot} {
						if err := os.Mkdir(dir, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					selected := project
					if parentLink {
						selected = filepath.Join(root, "alias")
						if err := os.Symlink("project", selected); err != nil {
							t.Fatal(err)
						}
					}
					manifest := filepath.Join(selected, "daem.toml")
					lockPath := filepath.Join(selected, "daem.lock.toml")
					testkit.WriteFile(t, project, "skills/demo/SKILL.md", "---\nname: demo\ndescription: fixture\n---\n")
					actualManifest := manifest
					if manifestLink {
						actualManifest = filepath.Join(dot, "daem.toml")
						if err := os.Symlink("../dot/daem.toml", manifest); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(actualManifest, []byte("version = 1\ntargets = [\"codex\"]\n\n[[skill]]\nname = \"demo\"\nscope = \"project\"\nsource = { path = \"skills/demo\", mode = \"vendor\" }\n"), 0o640); err != nil {
						t.Fatal(err)
					}
					run := func(args ...string) []byte {
						t.Helper()
						var stdout, stderr bytes.Buffer
						args = append(args, "--manifest", manifest)
						if code := testkit.RunCLI(args, &stdout, &stderr); code != 0 {
							t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout.String(), stderr.String())
						}
						return bytes.Clone(stdout.Bytes())
					}
					run("lock")
					if lockLink {
						if err := os.Rename(lockPath, filepath.Join(dot, "daem.lock.toml")); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink("../dot/daem.lock.toml", lockPath); err != nil {
							t.Fatal(err)
						}
					}
					before, err := lockfile.Load(t.Context(), lockPath)
					if err != nil {
						t.Fatal(err)
					}
					testkit.WriteFile(t, project, "skills/demo/SKILL.md", "---\nname: demo\ndescription: fixture\n---\nchanged\n")
					var result struct {
						Manifest string `json:"manifest_path"`
						Lockfile string `json:"lockfile_path"`
					}
					if err := json.Unmarshal(run("lock", "--json"), &result); err != nil {
						t.Fatal(err)
					}
					if result.Manifest != manifest || result.Lockfile != lockPath {
						t.Fatalf("selected addresses changed: %#v", result)
					}
					after, err := lockfile.Load(t.Context(), lockPath)
					if err != nil {
						t.Fatal(err)
					}
					if testkit.LockedSkills(t, before)[0].ContentHash == testkit.LockedSkills(t, after)[0].ContentHash {
						t.Fatal("linked lockfile was not updated for changed relative source")
					}
					testkit.WriteFile(t, project, "extra.md", "additional instruction\n")
					run("add", "instruction", "extra", filepath.Join(project, "extra.md"), "--target", "codex", "--scope", "project")
					run("remove", "skill", "demo")
					run("init", "--force")
					for _, file := range []struct {
						path string
						link bool
					}{
						{manifest, manifestLink}, {lockPath, lockLink},
					} {
						info, err := os.Lstat(file.path)
						if err != nil || (info.Mode()&os.ModeSymlink != 0) != file.link {
							t.Fatalf("selected entry changed: %s, %v", file.path, err)
						}
						if file.link {
							link, err := os.Readlink(file.path)
							if err != nil || link != "../dot/"+filepath.Base(file.path) {
								t.Fatalf("link spelling changed: %q, %v", link, err)
							}
						}
					}
					if _, err := os.Lstat(filepath.Join(dot, ".daem")); !os.IsNotExist(err) {
						t.Fatalf("storage moved beside the referent: %v", err)
					}
					if _, err := os.Lstat(filepath.Join(project, ".daem", "metadata-transaction")); !os.IsNotExist(err) {
						t.Fatalf("metadata transaction remains: %v", err)
					}
				})
			}
		}
	}
}
