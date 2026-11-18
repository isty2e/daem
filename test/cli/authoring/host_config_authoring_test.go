package cli_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestHostConfigRemoveGuidanceSelectsLiteralID(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	const original = `version = 1
targets = ['codex']
host_config = [{id = '--help', path = 'config.toml', source = 'prefs.toml'}]
`
	testkit.WriteFile(t, root, "daem.toml", original)
	exit, stdout, stderr := testkit.RunCapturedCLI("remove", "host-config", "--manifest", manifest, "--scope", "global", "--json", "--", "--help")
	if exit != 1 || stderr != "" {
		t.Fatalf("mismatched scope: %d %s %s", exit, stdout, stderr)
	}
	result := clijson.DecodeFailure(t, stdout, "remove host-config", "operation")
	for _, guidance := range result.Guidance {
		if len(guidance.Argv) >= 3 && slices.Equal(guidance.Argv[:3], []string{"daem", "remove", "host-config"}) {
			exit, stdout, stderr := testkit.RunCapturedCLI(guidance.Argv[1:]...)
			if exit != 0 {
				t.Fatalf("suggested remove failed: %d %s %s", exit, stdout, stderr)
			}
			exit, stdout, stderr = testkit.RunCapturedCLI("remove", "host-config", "--manifest", manifest, "--json", "--", "--help")
			if exit != 1 || stderr != "" {
				t.Fatalf("repeated removal should fail after following advice: %d %s %s", exit, stdout, stderr)
			}
			clijson.DecodeFailure(t, stdout, "remove host-config", "operation")
			return
		}
	}
	t.Fatalf("no runnable selection in %+v", result.Guidance)
}

func TestHostConfigAddValidatesPlacementAndSourceFlagShape(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	const original = "version = 1\ntargets = ['codex']\n"
	testkit.WriteFile(t, root, "daem.toml", original)
	for _, example := range []struct {
		name    string
		options []string
	}{
		{"no implicit placement", []string{"--host-path", "config.toml"}},
		{"Git path does not choose host path", []string{"--path", "config.toml", "--target", "codex", "--scope", "project"}},
		{"explicit empty Git flag", []string{"--ref=", "--host-path", "config.toml", "--target", "codex", "--scope", "project"}},
		{"multiple distinct targets", []string{"--host-path", "config.toml", "--target", "codex", "--scope", "project", "--target", "pi"}},
	} {
		t.Run(example.name, func(t *testing.T) {
			args := append([]string{"add", "host-config", "sample", "./prefs.toml", "--manifest", manifest, "--json"}, example.options...)
			exit, stdout, stderr := testkit.RunCapturedCLI(args...)
			if exit != 2 || stderr != "" {
				t.Fatalf("invalid request not refused at ingress: %d %s %s", exit, stdout, stderr)
			}
			clijson.DecodeFailure(t, stdout, "add host-config", "usage")
			testkit.AssertFileContent(t, manifest, original)
		})
	}
}
