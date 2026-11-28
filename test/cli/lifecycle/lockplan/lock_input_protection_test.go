package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/test/testkit"
)

func TestLockPublicationPreservesLinkedSource(t *testing.T) {
	const empty = "version = 1\ntargets = [\"codex\"]\n"
	const instruction = "\n[instructions.guide]\nsource = 'source.md'\n"
	const skill = "\n[[skill]]\nname = 'demo'\nsource = { path = 'skills/demo', mode = 'vendor' }\n"
	const extension = "\n[[extension]]\nid = 'tools'\ncarrier = 'pi-package'\ntargets = ['pi']\nscope = 'project'\nsource = { host_source = 'npm:tools@1.0.0' }\n"
	const config = "\n[[agent_config]]\nid = 'preferences'\npath = 'config.toml'\nsource = 'preferences.toml'\n"
	for _, test := range []struct {
		name     string
		manifest string
		args     []string
	}{
		{"lock", empty + instruction, []string{"lock"}},
		{"new source from add", empty, []string{"add", "instruction", "guide", "source.md"}},
		{"retained source after remove", empty + instruction + skill, []string{"remove", "skill", "demo"}},
		{"removed source", empty + instruction, []string{"remove", "instruction", "guide"}},
		{"retained source after unmanage", empty + instruction + extension, []string{"unmanage", "extension", "tools"}},
		{"retained source after agent-config release", empty + instruction + config, []string{"unmanage", "agent-config", "preferences"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
			testkit.WithWorkingDirectory(t, root)
			manifest := filepath.Join(root, "daem.toml")
			lock := filepath.Join(root, "daem.lock.toml")
			testkit.WriteFile(t, root, "daem.toml", test.manifest)
			testkit.WriteFile(t, root, "source.md", "original instructions\n")
			testkit.WriteFile(t, root, "skills/demo/SKILL.md", "---\nname: demo\ndescription: fixture\n---\n")
			testkit.WriteFile(t, root, "preferences.toml", "model = 'test'\n")

			run := func(args ...string) (int, string) {
				t.Helper()
				var stdout, stderr bytes.Buffer
				code := testkit.RunCLI(append(args, "--manifest", manifest), &stdout, &stderr)
				return code, stdout.String() + stderr.String()
			}
			if code, output := run("lock"); code != 0 {
				t.Fatalf("initial lock: exit %d: %s", code, output)
			}
			original, err := os.ReadFile(lock)
			if err != nil {
				t.Fatal(err)
			}
			testkit.WriteFile(t, root, "source.md", string(original))
			if err := os.Remove(lock); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("source.md", lock); err != nil {
				t.Fatal(err)
			}

			if code, output := run(append(test.args, "--dry-run")...); code != 0 {
				t.Fatalf("read-only preview: exit %d: %s", code, output)
			}
			code, output := run(test.args...)
			if code != 1 || !strings.Contains(output, declarationartifact.ErrLockfileInputOverlap.Error()) {
				t.Errorf("overlapping lock publication: exit %d, want input overlap refusal: %s", code, output)
			}
			if content, err := os.ReadFile(filepath.Join(root, "source.md")); err != nil || !bytes.Equal(content, original) {
				t.Fatalf("protected source changed: %d bytes, read error %v", len(content), err)
			}
			testkit.AssertFileContent(t, manifest, test.manifest)
			if link, err := os.Readlink(lock); err != nil || link != "source.md" {
				t.Fatalf("lock symlink = %q, %v", link, err)
			}
		})
	}
}

func TestLockPublicationDistinguishesListedRootFromSelectedSkill(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	testkit.SetDefaultRootEnv(t, filepath.Join(base, "home"))
	manifest := filepath.Join(root, "daem.toml")
	lock := filepath.Join(root, "daem.lock.toml")
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ['codex']

[[skill_group]]
source = { path = '.', mode = 'vendor' }
include = ['glob:Demo']
`)
	testkit.WriteFile(t, root, "Demo/SKILL.md", "---\nname: Demo\ndescription: fixture\n---\n")
	testkit.WriteFile(t, root, "~draft/seed", "not selected\n")
	var stdout, stderr bytes.Buffer
	run := func() int {
		stdout.Reset()
		stderr.Reset()
		return testkit.RunCLI([]string{"lock", "--manifest", manifest}, &stdout, &stderr)
	}
	if code := run(); code != 0 {
		t.Fatalf("lock in listing root: exit %d: %s", code, &stderr)
	}
	const unselected = "~draft/lock.toml"
	if err := os.Rename(lock, filepath.Join(root, unselected)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(unselected, lock); err != nil {
		t.Fatal(err)
	}
	if code := run(); code != 0 {
		t.Fatalf("lock in unselected directory: exit %d: %s", code, &stderr)
	}
	before := testkit.ReadFile(t, lock)
	inside := filepath.Join(root, "Demo", "lock.toml")
	if err := os.Rename(filepath.Join(root, unselected), inside); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	referenced := "Demo/lock.toml"
	if _, err := os.Stat(filepath.Join(root, "demo", "lock.toml")); err == nil {
		referenced = "demo/lock.toml" // In a case-folding namespace, test the stored spelling used by the selector.
	}
	if err := os.Symlink(referenced, lock); err != nil {
		t.Fatal(err)
	}

	if code := run(); code != 1 || !strings.Contains(stderr.String(), declarationartifact.ErrLockfileInputOverlap.Error()) {
		t.Fatalf("lock inside selected skill: exit %d, want overlap: %s", code, &stderr)
	}
	testkit.AssertFileContent(t, inside, string(before))
}

func TestRemoveAllowsAnUnavailableFormerSource(t *testing.T) {
	for _, source := range []string{"daem.lock.toml", "loop.md"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
			manifest := "version = 1\ntargets = ['codex']\n\n[instructions.guide]\nsource = '" + source + "'\n"
			testkit.WriteFile(t, root, "daem.toml", manifest)
			if source == "loop.md" {
				if err := os.Symlink(source, filepath.Join(root, source)); err != nil {
					t.Fatal(err)
				}
			}

			var stdout, stderr bytes.Buffer
			args := []string{"remove", "instruction", "guide", "--manifest", filepath.Join(root, "daem.toml")}
			if code := testkit.RunCLI(args, &stdout, &stderr); code != 0 {
				t.Fatalf("remove unavailable source: exit %d: %s", code, &stderr)
			}
			if _, err := os.Stat(filepath.Join(root, "daem.lock.toml")); err != nil {
				t.Fatal("remove did not create the lockfile", err)
			}
		})
	}
}
