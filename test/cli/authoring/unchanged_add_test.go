package cli_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRepeatedAddReportsUnchangedAcrossAuthoringFamilies(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		args   []string
		remove []string
	}{
		{name: "skill", kind: "skill", args: []string{"skill", "skills/oracle", "--target", "codex"}, remove: []string{"skill", "oracle", "--target", "codex"}},
		{name: "instruction", kind: "instructions", args: []string{"instruction", "guide", "notes.md", "--target", "codex"}, remove: []string{"instruction", "guide", "--target", "codex"}},
		{name: "hook", kind: "hook", args: []string{"hook", "lint", "PreToolUse", "true", "--target", "claude-code"}, remove: []string{"hook", "lint", "--target", "claude-code"}},
		{name: "mcp", kind: "mcp_server", args: []string{"mcp-server", "local", "true", "--target", "codex"}, remove: []string{"mcp-server", "local", "--target", "codex"}},
		{name: "extension", kind: "extension", args: []string{"extension", "context7", "context7@market", "--target", "claude-code"}, remove: []string{"extension", "context7", "--target", "claude-code"}},
		{name: "group", kind: "skill_group", args: []string{"skill-group", "skills", "--member", "oracle", "--member", "review", "--target", "codex"}, remove: []string{"skill", "oracle", "--target", "codex"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "xdg"))
			testkit.WriteFile(t, root, "daem.toml", "# retained\nversion = 1\ntargets = [\"codex\", \"claude-code\"]\n")
			testkit.WriteFile(t, root, "notes.md", "Guidance.\n")
			for _, name := range []string{"oracle", "review"} {
				testkit.WriteFile(t, root, "skills/"+name+"/SKILL.md", "---\nname: "+name+"\ndescription: Review work.\n---\n")
			}
			testkit.WithWorkingDirectory(t, root)
			manifest := filepath.Join(root, "daem.toml")
			lock := filepath.Join(root, "daem.lock.toml")
			args := append([]string{"add"}, test.args...)
			args = append(args, "--manifest", manifest, "--scope", "project")
			first := runAuthoringJSON(t, args)
			if first.ChangeCount != 1 || len(first.Unchanged) != 0 {
				t.Fatalf("initial add = %#v", first)
			}
			manifestBefore := readAuthoringFile(t, manifest)
			lockBefore := readAuthoringFile(t, lock)
			beforePreview := snapshotAuthoringFixture(t, root)
			preview := runAuthoringJSON(t, append(append([]string{}, args...), "--dry-run"))
			assertUnchangedAdd(t, preview, test.kind, "unchanged")
			if got := snapshotAuthoringFixture(t, root); !reflect.DeepEqual(beforePreview, got) {
				t.Fatalf("unchanged preview mutated fixture: before=%v after=%v", beforePreview, got)
			}
			var stdout, stderr bytes.Buffer
			diffArgs := append(append([]string{}, args...), "--dry-run", "--diff")
			if code := testkit.RunCLI(diffArgs, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "manifest diff: unchanged\n") || strings.Contains(stdout.String(), "\n@@") || stderr.Len() != 0 {
				t.Fatalf("unchanged diff: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if got := snapshotAuthoringFixture(t, root); !reflect.DeepEqual(beforePreview, got) {
				t.Fatal("unchanged diff mutated fixture")
			}
			written := runAuthoringJSON(t, args)
			assertUnchangedAdd(t, written, test.kind, "unchanged")
			if !bytes.Equal(manifestBefore, readAuthoringFile(t, manifest)) || !bytes.Equal(lockBefore, readAuthoringFile(t, lock)) {
				t.Fatal("repeat changed manifest or lock bytes")
			}
			stdout.Reset()
			stderr.Reset()
			if code := testkit.RunCLI(args, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "change: unchanged\n") || strings.Contains(stdout.String(), "warning:") || stderr.Len() != 0 {
				t.Fatalf("human unchanged: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			remove := append([]string{"remove"}, test.remove...)
			remove = append(remove, "--manifest", manifest)
			stdout.Reset()
			stderr.Reset()
			wrong := append(append([]string{}, remove...), "--scope", "global", "--json")
			if code := testkit.RunCLI(wrong, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "not found") || !strings.Contains(stderr.String(), "hint: available selection:") || !strings.Contains(stderr.String(), "--scope project") {
				t.Fatalf("wrong selection: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !bytes.Equal(manifestBefore, readAuthoringFile(t, manifest)) || !bytes.Equal(lockBefore, readAuthoringFile(t, lock)) {
				t.Fatal("missing remove changed manifest or lock")
			}
			runAuthoringJSON(t, append(remove, "--scope", "project"))
			beforeMissing := snapshotAuthoringFixture(t, root)
			stdout.Reset()
			stderr.Reset()
			if code := testkit.RunCLI(remove, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "not found") {
				t.Fatalf("repeat remove: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if got := snapshotAuthoringFixture(t, root); !reflect.DeepEqual(beforeMissing, got) {
				t.Fatal("repeat remove mutated fixture")
			}
		})
	}
}

func TestUnchangedAddStillCreatesRefreshesAndValidatesLock(t *testing.T) {
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "xdg"))
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, root, "notes.md", "First guidance.\n")
	testkit.WriteFile(t, root, "skills/other/SKILL.md", "---\nname: other\ndescription: Other skill.\n---\n")
	manifest := filepath.Join(root, "daem.toml")
	lock := filepath.Join(root, "daem.lock.toml")
	args := []string{"add", "instruction", "guide", filepath.Join(root, "notes.md"), "--manifest", manifest, "--scope", "project", "--target", "codex"}
	runAuthoringJSON(t, args)
	original := readAuthoringFile(t, manifest)
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	beforePreview := snapshotAuthoringFixture(t, root)
	assertUnchangedAdd(t, runAuthoringJSON(t, append(append([]string{}, args...), "--dry-run")), "instructions", "would_write")
	if !reflect.DeepEqual(beforePreview, snapshotAuthoringFixture(t, root)) {
		t.Fatal("missing-lock preview wrote state")
	}
	assertUnchangedAdd(t, runAuthoringJSON(t, args), "instructions", "written")
	oldLock := readAuthoringFile(t, lock)
	testkit.WriteFile(t, root, "notes.md", "Updated guidance.\n")
	assertUnchangedAdd(t, runAuthoringJSON(t, args), "instructions", "written")
	if bytes.Equal(oldLock, readAuthoringFile(t, lock)) || !bytes.Equal(original, readAuthoringFile(t, manifest)) {
		t.Fatal("source refresh did not update only lock content")
	}
	runAuthoringJSON(t, []string{"add", "skill", filepath.Join(root, "skills/other"), "--manifest", manifest, "--scope", "project", "--target", "codex"})
	original, oldLock = readAuthoringFile(t, manifest), readAuthoringFile(t, lock)
	if err := os.Remove(filepath.Join(root, "skills/other/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := testkit.RunCLI(args, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "lock prospective manifest") {
		t.Fatalf("missing unrelated source: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !bytes.Equal(original, readAuthoringFile(t, manifest)) || !bytes.Equal(oldLock, readAuthoringFile(t, lock)) {
		t.Fatal("failed validation advanced manifest or lock")
	}
}

func runAuthoringJSON(t *testing.T, args []string) clipresent.ManifestAuthoringJSONOutput {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := testkit.RunCLI(append(append([]string{}, args...), "--json"), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
	}
	return clijson.DecodeManifestAuthoring(t, stdout.Bytes())
}

func assertUnchangedAdd(t *testing.T, result clipresent.ManifestAuthoringJSONOutput, kind string, lockStatus string) {
	t.Helper()
	if result.SchemaVersion != 6 || result.ResourceCount != 1 || result.ChangeCount != 0 || result.Changes == nil || len(result.Changes) != 0 || result.HasErrors || len(result.Unchanged) != 1 || result.Unchanged[0].Kind != kind || result.Unchanged[0].Name == "" || result.Lockfile == nil || result.Lockfile.Status != lockStatus {
		t.Fatalf("unchanged result = %#v, want kind=%s lock=%s", result, kind, lockStatus)
	}
}

func readAuthoringFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func snapshotAuthoringFixture(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := info.Mode().String()
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += fmt.Sprintf(":%x", sha256.Sum256(content))
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += ":" + target
		}
		result[path] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
