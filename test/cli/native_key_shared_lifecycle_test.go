package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeKeySharedCLILifecycle(t *testing.T) {
	for _, fixture := range []struct {
		name            string
		target          string
		filename        string
		destination     string
		syntax          configdocument.Syntax
		original        string
		source          string
		updated         string
		manualSuffix    string
		manualExpected  string
		preservedBlocks []string
		mode            os.FileMode
		exactReleased   bool
	}{
		{
			name: "Codex existing 0644", target: "codex", filename: "config.toml",
			destination: ".codex/config.toml", syntax: configdocument.TOML,
			original: "# User-owned comment\nunmanaged = true\n", mode: 0o644,
			source: "model = 'managed'\n", updated: "model = 'updated'\n",
			exactReleased: true,
		},
		{
			name: "Codex standard table", target: "codex", filename: "config.toml",
			destination: ".codex/config.toml", syntax: configdocument.TOML,
			original: "# Display settings\n[tui]\n# User theme\ntheme = 'dark'\n", mode: 0o600,
			source: "[tui]\nnotifications = true\n", updated: "[tui]\nnotifications = false\n",
			manualSuffix: "notifications = ['agent-turn-complete']\n", exactReleased: true,
			manualExpected:  "[tui]\nnotifications = ['agent-turn-complete']\n",
			preservedBlocks: []string{"[tui]\n", "# User theme\ntheme = 'dark'\n"},
		},
		{
			name: "Codex nested profiles 0644", target: "codex", filename: "config.toml",
			destination: ".codex/config.toml", syntax: configdocument.TOML,
			original: "# Profiles\n[profiles.work]\n# Model selection\nmodel = 'user-model'\n\n[profiles.personal]\nmodel = 'personal-model'\n", mode: 0o644,
			source:       "[profiles.work]\nmodel_reasoning_effort = 'high'\n",
			updated:      "[profiles.work]\nmodel_reasoning_effort = 'low'\n",
			manualSuffix: "\n[profiles.extra]\nmodel = 'new-model'\n", exactReleased: true,
			manualExpected:  "[profiles.extra]\nmodel = 'new-model'\n",
			preservedBlocks: []string{"[profiles.work]\n", "# Model selection\nmodel = 'user-model'\n", "[profiles.personal]\nmodel = 'personal-model'\n"},
		},
		{
			name: "Codex missing table", target: "codex", filename: "config.toml",
			destination: ".codex/config.toml", syntax: configdocument.TOML, mode: 0o644,
			original: "# Unrelated root setting\nunmanaged = true\n",
			source:   "[features]\nmanaged = true\n", updated: "[features]\nmanaged = false\n",
			preservedBlocks: []string{"# Unrelated root setting\nunmanaged = true\n"},
		},
		{
			name: "Pi existing nested JSON 0644", target: "pi", filename: "settings.json",
			destination: ".pi/settings.json", syntax: configdocument.JSON,
			original: "{\n  \"theme\": \"dark\",\n  \"extensionPreferences\": {\n    \"unmanaged\": true\n  }\n}\n", mode: 0o644,
			source:  "{\"extensionPreferences\":{\"managed\":true}}\n",
			updated: "{\"extensionPreferences\":{\"managed\":false}}\n",
		},
		{
			name: "Pi missing JSON", target: "pi", filename: "settings.json",
			destination: ".pi/settings.json", syntax: configdocument.JSON, mode: 0o600,
			source:  "{\"extensionPreferences\":{\"managed\":true}}\n",
			updated: "{\"extensionPreferences\":{\"managed\":false}}\n",
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			sourceName := "preferences" + filepath.Ext(fixture.filename)
			manifestContent := fmt.Sprintf(`version = 1
targets = [%q]
[[host_config]]
id = "preferences"
path = %q
source = %q
ownership = "keys"
`, fixture.target, fixture.filename, "./"+sourceName)
			testkit.WriteFile(t, workspace, "daem.toml", manifestContent)
			destination := filepath.Join(workspace, filepath.FromSlash(fixture.destination))
			if fixture.original != "" {
				testkit.WriteFile(t, workspace, fixture.destination, fixture.original)
				if err := os.Chmod(destination, fixture.mode); err != nil {
					t.Fatal(err)
				}
			}

			for _, source := range []string{fixture.source, fixture.updated} {
				testkit.WriteFile(t, workspace, sourceName, source)
				nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
				content, err := os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
				nativefixture.RequireSharedValues(t, fixture.syntax, content, source, true)
				for _, block := range fixture.preservedBlocks {
					if !bytes.Contains(content, []byte(block)) {
						t.Fatalf("native key edit changed an unrelated table/comment block %q:\n%s", block, content)
					}
				}
				if fixture.original != "" {
					nativefixture.RequireSharedValues(t, fixture.syntax, content, fixture.original, true)
				}
				nativefixture.RequireSharedMode(t, destination, fixture.mode)
				nativefixture.RequireLifecycleMetadata(t, manifest, true)
				nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))

				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
				repeated, err := os.ReadFile(destination)
				if err != nil || !bytes.Equal(repeated, content) {
					t.Fatal("unchanged repeat Apply rewrote the shared document", err)
				}
				nativefixture.RequireSharedMode(t, destination, fixture.mode)
				nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
			}

			testkit.WriteFile(t, workspace, "daem.toml", fmt.Sprintf("version = 1\ntargets = [%q]\n", fixture.target))
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			content, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.RequireSharedValues(t, fixture.syntax, content, fixture.updated, false)
			if fixture.original != "" {
				nativefixture.RequireSharedValues(t, fixture.syntax, content, fixture.original, true)
			}
			if fixture.exactReleased && !bytes.Equal(content, []byte(fixture.original)) {
				t.Fatalf("key add/remove changed the original table or comments:\n%s", content)
			}
			nativefixture.RequireSharedMode(t, destination, fixture.mode)
			nativefixture.RequireLifecycleMetadata(t, manifest, false)
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

			if fixture.manualSuffix != "" {
				manual := append(bytes.Clone(content), fixture.manualSuffix...)
				if _, err := configdocument.Parse(t.Context(), manual, fixture.syntax, nativefixture.LifecycleLimits()); err != nil {
					t.Fatal("ordinary manual table edit became invalid after releasing managed keys", err)
				}
				nativefixture.RequireSharedValues(t, fixture.syntax, manual, fixture.manualExpected, true)
			}
		})
	}
}

func TestNativeKeyExactAdoptionPreservesSharedMode(t *testing.T) {
	for _, fixture := range []struct{ target, filename, destination, original, source string }{
		{"codex", "config.toml", ".codex/config.toml", "model = 'managed'\nunmanaged = true\n", "model = 'managed'\n"},
		{"pi", "settings.json", ".pi/settings.json", "{\n  \"managed\": true,\n  \"unmanaged\": true\n}\n", "{\"managed\":true}\n"},
	} {
		t.Run(fixture.target, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
			workspace := filepath.Join(root, "workspace")
			manifest := filepath.Join(workspace, "daem.toml")
			sourceName := "preferences" + filepath.Ext(fixture.filename)
			testkit.WriteFile(t, workspace, "daem.toml", fmt.Sprintf(`version = 1
 targets = [%q]
 [[host_config]]
 id = "preferences"
 path = %q
 source = %q
 ownership = "keys"
 `, fixture.target, fixture.filename, "./"+sourceName))
			testkit.WriteFile(t, workspace, sourceName, fixture.source)
			testkit.WriteFile(t, workspace, fixture.destination, fixture.original)
			destination := filepath.Join(workspace, filepath.FromSlash(fixture.destination))
			if err := os.Chmod(destination, 0o644); err != nil {
				t.Fatal(err)
			}
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--manage-existing", "--yes")
			nativefixture.RequireLifecycleMetadata(t, manifest, true)
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			content, err := os.ReadFile(destination)
			if err != nil || !bytes.Equal(content, []byte(fixture.original)) {
				t.Fatal("exact key adoption rewrote the shared document", err)
			}
			nativefixture.RequireSharedMode(t, destination, 0o644)
		})
	}
}
