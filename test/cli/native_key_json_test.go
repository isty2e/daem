package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
	"github.com/pelletier/go-toml/v2"
)

func TestNativeKeyJSONLifecycleLiteralNames(t *testing.T) {
	for _, name := range []string{"a/b", "a~b", ""} {
		t.Run(strconv.Quote(name), func(t *testing.T) {
			root := t.TempDir()
			for variable, directory := range map[string]string{
				"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data",
				"XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache", "CODEX_HOME": "codex",
			} {
				t.Setenv(variable, filepath.Join(root, directory))
			}
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]

[[host_config]]
id = "literal-names"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
			const foreign = "# Keep this user comment\nunmanaged = \"preserved\"\n"
			testkit.WriteFile(t, workspace, ".codex/config.toml", foreign)
			destination := filepath.Join(workspace, ".codex/config.toml")
			keyPath := []string{"ext", name}

			for _, value := range []bool{true, false} {
				testkit.WriteFile(t, workspace, "preferences.toml", "[ext]\n"+strconv.Quote(name)+" = "+strconv.FormatBool(value)+"\n")
				output := requireNativeJSONCommand(t, "lock", "--manifest", manifest, "--json")
				requireNativeJSONKeyPaths(t, output, "subject_changes", keyPath)
				var lockResult struct {
					LockfilePath string `json:"lockfile_path"`
				}
				if err := json.Unmarshal([]byte(output), &lockResult); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(lockResult.LockfilePath); err != nil {
					t.Fatal("successful JSON lock did not publish its lockfile", err)
				}

				before, err := os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
				for _, command := range [][]string{
					{"status", "--manifest", manifest, "--json"},
					{"apply", "--manifest", manifest, "--dry-run", "--json"},
				} {
					requireNativeJSONKeyPaths(t, requireNativeJSONCommand(t, command...), "actions", keyPath)
				}
				after, err := os.ReadFile(destination)
				if err != nil || string(after) != string(before) {
					t.Fatal("JSON planning changed the host document", err)
				}

				output = requireNativeJSONCommand(t, "apply", "--manifest", manifest, "--yes", "--json")
				requireNativeJSONKeyPaths(t, output, "actions", keyPath)
				content, err := os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
				var document struct {
					Unmanaged string          `toml:"unmanaged"`
					Ext       map[string]bool `toml:"ext"`
				}
				if err := toml.Unmarshal(content, &document); err != nil {
					t.Fatal("successful JSON apply left invalid TOML", err)
				}
				actual, present := document.Ext[name]
				if !present || actual != value || document.Unmanaged != "preserved" {
					t.Fatal("JSON result disagrees with the actual selected/foreign host values")
				}
				nativefixture.RequireLifecycleMetadata(t, manifest, true)
				nativefixture.RequireLifecycleClean(t, requireNativeJSONCommand(t, "status", "--manifest", manifest, "--check", "--json"))
				requireNativeJSONCommand(t, "apply", "--manifest", manifest, "--yes", "--json")
			}
		})
	}
}

func requireNativeJSONCommand(t *testing.T, arguments ...string) string {
	t.Helper()
	code, stdout, stderr := testkit.RunCapturedCLI(arguments...)
	if code != 0 {
		t.Fatalf("%q failed: exit=%d stdout=%s stderr=%s", arguments, code, stdout, stderr)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("%q returned invalid JSON: %s", arguments, stdout)
	}
	return stdout
}

func requireNativeJSONKeyPaths(t *testing.T, output, collection string, expected []string) {
	t.Helper()
	// Decode the producer's actual response, including nested before/after rows.
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(payload[collection], &rows); err != nil || len(rows) == 0 {
		t.Fatalf("response has no %s rows: %s (%v)", collection, output, err)
	}

	var inspect func(json.RawMessage)
	found := false
	inspect = func(raw json.RawMessage) {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return
		}
		if path, present := fields["key_path"]; present {
			var actual []string
			if err := json.Unmarshal(path, &actual); err != nil || !slices.Equal(actual, expected) {
				t.Fatalf("key_path is not the literal name array: %s", path)
			}
			if legacy, present := fields["content_path"]; present && string(legacy) != `""` {
				t.Fatalf("native key was also projected through legacy content_path: %s", legacy)
			}
			found = true
		}
		for _, nested := range fields {
			inspect(nested)
		}
	}
	for _, row := range rows {
		inspect(row)
	}
	if !found {
		t.Fatalf("%s omitted the literal native key path: %s", collection, output)
	}
}
