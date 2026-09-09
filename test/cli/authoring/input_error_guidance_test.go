package cli_test

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestCommonInputErrorsGiveActionableGuidance(t *testing.T) {
	const minimal = "version = 1\ntargets = [\"codex\"]\n"
	cases := []struct {
		name     string
		manifest string
		args     []string
		want     []string
	}{
		{"unknown key", minimal + "unknown_field = true\n", []string{"lock"}, []string{"unknown manifest key", "next: correct or remove", "docs/manifest.md"}},
		{"malformed TOML", "version = 1\ntargets = [\n", []string{"lock"}, []string{"unclosed array", "line 3, column 1", "next: check TOML"}},
		{"missing source add", minimal, []string{"add", "instruction", "guide", "missing.md"}, []string{"source path", "missing.md", "next: check that the source exists"}},
		{"missing source lock", minimal + "[instructions.guide]\nsource = { path = \"missing.md\", mode = \"vendor\" }\n", []string{"lock"}, []string{"source path", "next: check that the source exists", "next: run daem lock --manifest"}},
		{"missing Git ref", minimal, []string{"add", "skill", "https://example.invalid/skills.git", "--name", "review"}, []string{"--ref is required", "branch, tag, or commit", "local paths do not need --ref"}},
		{"missing group Git ref", minimal, []string{"add", "skill-group", "https://example.invalid/skills.git", "--member", "review"}, []string{"--ref is required", "branch, tag, or commit"}},
		{"missing frontmatter", minimal, []string{"add", "skill", "invalid", "--name", "review"}, []string{"SKILL.md frontmatter is required", "next: edit the source SKILL.md"}},
		{"mechanical repair", minimal, []string{"add", "skill", "repairable", "--name", "review"}, []string{"compat_repair = true", "repair actions:"}},
		{"explicit repair still needs manual edit", minimal + "[[skill]]\nname = \"review\"\nsource = { path = \"invalid\", mode = \"vendor\" }\ncompat_repair = true\n", []string{"lock"}, []string{"manual skill compatibility repair required", "next: fix the reported source issues manually"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "xdg"))
			testkit.WithWorkingDirectory(t, root)
			testkit.WriteFile(t, root, "daem.toml", test.manifest)
			testkit.WriteFile(t, root, "invalid/SKILL.md", "# Review\nCheck changes.\n")
			testkit.WriteFile(t, root, "repairable/skill.md", "---\ndescription: Review changes.\n---\n")
			for _, json := range []bool{false, true} {
				args := append([]string{}, test.args...)
				args = append(args, "--manifest", filepath.Join(root, "daem.toml"), "--dry-run")
				if json {
					args = append(args, "--json")
				}
				before := snapshotAuthoringFixture(t, root)
				var stdout, stderr bytes.Buffer
				code := testkit.RunCLI(args, &stdout, &stderr)
				if code != 1 || stdout.Len() != 0 {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				for _, want := range test.want {
					if !strings.Contains(stderr.String(), want) {
						t.Errorf("stderr=%q, want %q", stderr.String(), want)
					}
				}
				if strings.Contains(stderr.String(), "lock prospective manifest") || strings.Contains(stderr.String(), "repairability=") {
					t.Errorf("internal diagnostic noise: %q", stderr.String())
				}
				if test.name == "missing frontmatter" && strings.Count(stderr.String(), "SKILL.md frontmatter is required") != 1 {
					t.Errorf("duplicated cause: %q", stderr.String())
				}
				if test.name == "missing source add" && strings.Contains(stderr.String(), "next: run daem lock") {
					t.Errorf("failed add must not be replaced by locking the old manifest: %q", stderr.String())
				}
				if !reflect.DeepEqual(before, snapshotAuthoringFixture(t, root)) {
					t.Fatal("failed preview changed fixture")
				}
			}
		})
	}
}

func TestCorrectedLocalInputsNeedNoRemoteReference(t *testing.T) {
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "xdg"))
	testkit.WithWorkingDirectory(t, root)
	testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	testkit.WriteFile(t, root, "notes.md", "Project instructions.\n")
	testkit.WriteFile(t, root, "skills/review/SKILL.md", "---\nname: review\ndescription: Review changes.\n---\n# Review\nCheck changes.\n")
	for _, args := range [][]string{
		{"lock"},
		{"add", "instruction", "guide", "notes.md"},
		{"add", "skill", "skills/review", "--name", "review"},
		{"add", "skill-group", "skills", "--member", "review"},
	} {
		before := snapshotAuthoringFixture(t, root)
		var stdout, stderr bytes.Buffer
		args = append(args, "--manifest", filepath.Join(root, "daem.toml"), "--dry-run", "--json")
		if code := testkit.RunCLI(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 || stdout.Len() == 0 {
			t.Fatalf("args=%v exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
		if !reflect.DeepEqual(before, snapshotAuthoringFixture(t, root)) {
			t.Fatal("successful preview changed fixture")
		}
	}
}
