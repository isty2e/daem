package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestNativeNamedCodexProfileCanBeCreatedAndRetiredInOneApplyEach(t *testing.T) {
	for _, scenario := range []struct {
		literalParent bool
		profile       string
	}{
		{false, "work.config.toml"}, {true, "work.config.toml"}, {false, "Work.config.toml"},
	} {
		t.Run(fmt.Sprint("literal-parent=", scenario.literalParent, "/", scenario.profile), func(t *testing.T) {
			requireNativeNamedCodexProfileLifecycle(t, scenario.literalParent, scenario.profile)
		})
	}
}

func requireNativeNamedCodexProfileLifecycle(t *testing.T, literalParent bool, profile string) {
	t.Helper()
	if literalParent && runtime.GOOS == "windows" {
		t.Skip("Win32 parent components are lexical before reparse traversal")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("CODEX_HOME", "")
	selectedRoot := filepath.Join(home, ".codex")
	if literalParent {
		selectedRoot = filepath.Join(home, "actual", "codex")
		for _, directory := range []string{selectedRoot, filepath.Join(home, "actual", "inner"), filepath.Join(home, "codex")} {
			if err := os.MkdirAll(directory, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		link := filepath.Join(home, "root-link")
		if err := os.Symlink(filepath.Join(home, "actual", "inner"), link); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
		t.Setenv("CODEX_HOME", link+"/../codex")
	}
	for variable, directory := range map[string]string{
		"XDG_DATA_HOME": "data", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache",
	} {
		t.Setenv(variable, filepath.Join(home, directory))
	}
	manifest := filepath.Join(home, "daem.toml")
	source := filepath.Join(home, "profile.toml")
	if err := os.WriteFile(source, []byte("model = 'fixture-model'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("version = 1\ntargets = [\"codex\"]\n[[host_config]]\nid = \"work-profile\"\nscope = \"global\"\npath = %q\nsource = %q\nownership = \"keys\"\n", profile, filepath.ToSlash(source))
	if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(command string) {
		t.Helper()
		args := []string{command, "--manifest", manifest}
		if command == "apply" {
			args = append(args, "--yes")
		}
		var stdout, stderr bytes.Buffer
		if code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 0 {
			t.Fatalf("%s did not converge: exit %d\n%s\n%s", command, code, stdout.String(), stderr.String())
		}
	}
	run("lock")
	run("apply")
	path := filepath.Join(selectedRoot, profile)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var created struct{ Model string }
	if err := toml.Unmarshal(content, &created); err != nil || created.Model != "fixture-model" {
		t.Fatal("apply did not create the selected profile value", err)
	}
	if err := os.WriteFile(manifest, []byte("version = 1\ntargets = [\"codex\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("lock")
	run("apply")
	content, err = os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	var retired struct{ Model string }
	if err := toml.Unmarshal(content, &retired); err != nil || retired.Model != "" {
		t.Fatal("retirement left the profile's managed value behind", err)
	}
}

func TestNativePlanningExplainsProjectGlobalRefusalAndKeepsReservedDataFirst(t *testing.T) {
	for _, endpoint := range []string{"settings.json", "auth.json"} {
		t.Run(endpoint, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("PI_CODING_AGENT_DIR", "")
			t.Setenv("CODEX_HOME", "")
			t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			root := filepath.Join(home, ".pi", "agent")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, endpoint)
			original := []byte(`{"theme":"dark","private":"fixture-private-native-value"}`)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "preferences.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(home, "daem.toml")
			body := "version = 1\ntargets = [\"pi\"]\n[[host_config]]\nid = \"preferences\"\nscope = \"project\"\npath = \"agent/" + endpoint + "\"\nsource = \"./preferences.json\"\nownership = \"keys\"\n"
			if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := RunWithOptions([]string{"lock", "--manifest", manifest}, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 0 {
				t.Fatal("fixture lock failed", stdout.String(), stderr.String())
			}
			stdout.Reset()
			stderr.Reset()
			if code := RunWithOptions([]string{"apply", "--manifest", manifest, "--yes"}, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 1 {
				t.Fatal("unsafe native claim was admitted", code, stdout.String(), stderr.String())
			}
			message := stdout.String() + stderr.String()
			if strings.Contains(message, home) || strings.Contains(message, "fixture-private-native-value") || strings.Contains(message, "apply was refused before effects") {
				t.Fatal("native refusal hid its cause or disclosed private evidence", message)
			}
			if endpoint == "auth.json" {
				if !strings.Contains(message, "authentication") || strings.Contains(message, "move existing project management") {
					t.Fatal("project-scope advice overrode reserved authentication", message)
				}
			} else if !strings.Contains(message, "project claims do not grant global write authority") || !strings.Contains(message, "removes its managed values") {
				t.Fatal("project/global refusal omitted its authority cause or migration warning", message)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, original) {
				t.Fatal("refused planning changed the host document", err)
			}
		})
	}
}
