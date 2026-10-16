package cli_test

import (
	"bytes"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestUnknownManifestKeyInputGuidanceHasSourcePosition(t *testing.T) {
	position := regexp.MustCompile(`\(line ([0-9]+), column ([0-9]+)\)`)
	for _, command := range []struct {
		name string
		args []string
	}{
		{name: "lock", args: []string{"lock", "--dry-run"}},
		{name: "status", args: []string{"status"}},
		{name: "apply", args: []string{"apply", "--dry-run"}},
		{name: "add", args: []string{"add", "instruction", "added", "notes.md", "--target", "codex", "--scope", "project", "--dry-run"}},
		{name: "remove", args: []string{"remove", "instruction", "kept", "--dry-run"}},
	} {
		t.Run(command.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "xdg"))
			testkit.WithWorkingDirectory(t, root)
			manifest := "version=1\ntargets=[\"codex\"]\n  bogus=true\n[instructions.kept]\nsource=\"notes.md\"\n"
			testkit.WriteFile(t, root, "daem.toml", manifest)
			testkit.WriteFile(t, root, "notes.md", "Project instructions.\n")
			for _, jsonOutput := range []bool{false, true} {
				args := append([]string{}, command.args...)
				args = append(args, "--manifest", filepath.Join(root, "daem.toml"))
				if jsonOutput {
					args = append(args, "--json")
				}
				before := snapshotAuthoringFixture(t, root)
				var stdout, stderr bytes.Buffer
				if code := testkit.RunCLI(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
					t.Fatalf("code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
				}
				got := position.FindStringSubmatch(stderr.String())
				if len(got) != 3 || got[1] != "3" || got[2] != "3" {
					t.Errorf("position=%q, want line3 column3 in %q", got, &stderr)
				}
				if !strings.Contains(stderr.String(), "next: correct or remove") || strings.Contains(stderr.String(), "check TOML brackets") {
					t.Errorf("unknown key lost its repair guidance: %q", &stderr)
				}
				if !reflect.DeepEqual(before, snapshotAuthoringFixture(t, root)) {
					t.Fatal("invalid manifest command changed fixture files")
				}
			}
		})
	}
}
