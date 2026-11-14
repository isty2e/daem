package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCodexLiteralRootWorksAcrossPlanningPublicationAndInventory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Win32 normalizes parents before following reparse points")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	for variable, directory := range map[string]string{
		"XDG_DATA_HOME": "data", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache",
	} {
		t.Setenv(variable, filepath.Join(home, directory))
	}
	physical, lexical := filepath.Join(home, "physical", "codex"), filepath.Join(home, "codex")
	for _, directory := range []string{physical, lexical, filepath.Join(home, "physical", "inner")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(home, "root-link")
	if err := os.Symlink(filepath.Join(home, "physical", "inner"), link); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}
	t.Setenv("CODEX_HOME", link+"/../codex")
	if err := os.WriteFile(filepath.Join(lexical, "config.toml"), []byte("model = 'lexical-decoy'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(home, "settings.toml")
	if err := os.WriteFile(settings, []byte("model = 'fixture-model'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(home, "daem.toml")
	body := fmt.Sprintf("version=1\ntargets=['codex']\n[[host_config]]\nid='settings'\nscope='global'\npath='config.toml'\nsource=%q\nownership='keys'\n", filepath.ToSlash(settings))
	if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(arguments ...string) {
		t.Helper()
		arguments = append(arguments, "--manifest", manifest)
		var stdout, stderr bytes.Buffer
		if code := RunWithOptions(arguments, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 0 {
			t.Fatalf("%v failed: exit %d\n%s\n%s", arguments, code, stdout.String(), stderr.String())
		}
	}
	run("lock")
	run("apply", "--yes")
	run("status")
	run("list", "outputs")
	run("apply", "--yes")
	content, err := os.ReadFile(filepath.Join(physical, "config.toml"))
	if err != nil || !strings.Contains(string(content), "fixture-model") {
		t.Fatal("planning and publication selected different Codex homes", err)
	}
	decoy, err := os.ReadFile(filepath.Join(lexical, "config.toml"))
	if err != nil || string(decoy) != "model = 'lexical-decoy'\n" {
		t.Fatal("lexical decoy was read as managed authority or modified", err)
	}
}
