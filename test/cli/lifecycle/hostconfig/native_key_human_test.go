package cli_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeKeyHumanDeclarationAndLocation(t *testing.T) {
	for _, test := range []struct {
		name, source, display string
	}{
		{"nested", "[tui]\ntheme = 'not-a-public-value'\n", "tui.theme"},
		{"slash", "[ext]\n\"a/b\" = true\n", `ext."a/b"`},
		{"literal dot", "\"a.b\" = true\n", `"a.b"`},
		{"empty", "\"\" = true\n", `""`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]
[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
			testkit.WriteFile(t, workspace, "preferences.toml", test.source)
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			for _, args := range [][]string{
				{"status", "--manifest", manifest},
				{"apply", "--manifest", manifest, "--dry-run"},
				{"apply", "--manifest", manifest, "--yes"},
			} {
				output := nativefixture.RunLifecycleCommand(t, args...)
				for _, expected := range []string{`resource="host_config/preferences"`, `.codex/config.toml`, "key: " + test.display} {
					if !strings.Contains(output, expected) {
						t.Fatalf("%q omitted native declaration/location %q: %s", args, expected, output)
					}
				}
				if strings.Contains(output, "not-a-public-value") || strings.Contains(output, "key-v1.") {
					t.Fatalf("basic output exposed a value or encoded internal identity: %s", output)
				}
			}

			status := clijson.DecodePlan(t, []byte(requireNativeJSONCommand(t, "status", "--manifest", manifest, "--json")))
			if len(status.Actions) != 1 || status.Actions[0].ResourceID != "host_config/preferences" {
				t.Fatal("native JSON row omitted the readable declaration identity")
			}
		})
	}
}
