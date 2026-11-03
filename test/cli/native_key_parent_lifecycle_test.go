package cli_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeKeyParentCLILifecycle(t *testing.T) {
	for _, fixture := range []struct {
		name, target, filename, destination string
		original, source                    string
		syntax                              configdocument.Syntax
		retainedParent                      string
		exactReleased                       bool
		manualSuffix, manualExpected        string
	}{
		{
			name: "new TOML parent remains", target: "codex", filename: "config.toml", destination: ".codex/config.toml",
			original: "# User root\noutside = true\n", source: "[features]\nmanaged = true\n", syntax: configdocument.TOML,
			retainedParent: "/features", manualSuffix: "manual = true\n", manualExpected: "[features]\nmanual = true\n",
		},
		{
			name: "implicit TOML parent stays implicit", target: "codex", filename: "config.toml", destination: ".codex/config.toml",
			original: "# User profile\n[profiles.fast]\nmodel = 'user-model'\n", source: "[profiles]\ndefault = 'fast'\n", syntax: configdocument.TOML,
			exactReleased: true, manualSuffix: "\n[profiles]\ndefault = 'manual'\n", manualExpected: "[profiles]\ndefault = 'manual'\n",
		},
		{
			name: "root dotted TOML parent", target: "codex", filename: "config.toml", destination: ".codex/config.toml",
			original: "# User display\ntui.theme = 'dark' # theme\n", source: "tui.notifications = true\n", syntax: configdocument.TOML,
			exactReleased: true, manualSuffix: "tui.foo = 1\n", manualExpected: "tui.foo = 1\n",
		},
		{
			name: "scoped dotted TOML parent", target: "codex", filename: "config.toml", destination: ".codex/config.toml",
			original: "[ui]\n# User display\ntui.theme = 'dark'\n", source: "[ui.tui]\nnotifications = true\n", syntax: configdocument.TOML,
			exactReleased: true, manualSuffix: "tui.foo = 1\n", manualExpected: "[ui]\ntui.foo = 1\n",
		},
		{
			name: "new JSON parent remains", target: "pi", filename: "settings.json", destination: ".pi/settings.json",
			original: "{\"outside\":true}\n", source: "{\"retry\":{\"enabled\":true}}\n", syntax: configdocument.JSON,
			retainedParent: "/retry",
		},
		{
			name: "existing empty JSON parent remains", target: "pi", filename: "settings.json", destination: ".pi/settings.json",
			original: "{\"outside\":true,\"retry\":{}}\n", source: "{\"retry\":{\"enabled\":true}}\n", syntax: configdocument.JSON,
			retainedParent: "/retry",
		},
		{
			name: "explicit empty JSON value is removed", target: "pi", filename: "settings.json", destination: ".pi/settings.json",
			original: "{\"outside\":true}\n", source: "{\"retry\":{}}\n", syntax: configdocument.JSON,
		},
		{
			name: "existing empty TOML parent remains", target: "codex", filename: "config.toml", destination: ".codex/config.toml",
			original: "# User table\n[features]\n", source: "[features]\nmanaged = true\n", syntax: configdocument.TOML,
			retainedParent: "/features", exactReleased: true,
		},
		{
			name: "explicit empty TOML value is removed", target: "codex", filename: "config.toml", destination: ".codex/config.toml",
			original: "# User root\noutside = true\n", source: "retry = {}\n", syntax: configdocument.TOML,
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
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			content, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.RequireSharedValues(t, fixture.syntax, content, fixture.source, true)
			nativefixture.RequireLifecycleMetadata(t, manifest, true)
			nativefixture.RequireSharedMode(t, destination, 0o644)
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

			testkit.WriteFile(t, workspace, "daem.toml", fmt.Sprintf("version = 1\ntargets = [%q]\n", fixture.target))
			nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")
			content, err = os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			nativefixture.RequireSharedValues(t, fixture.syntax, content, fixture.source, false)
			nativefixture.RequireSharedValues(t, fixture.syntax, content, fixture.original, true)
			if fixture.exactReleased && !bytes.Equal(content, []byte(fixture.original)) {
				t.Fatalf("removal changed original parent representation:\n%s", content)
			}
			if fixture.retainedParent != "" {
				requireNativeEmptyParent(t, fixture.syntax, content, fixture.retainedParent)
			}
			nativefixture.RequireLifecycleMetadata(t, manifest, false)
			nativefixture.RequireSharedMode(t, destination, 0o644)
			nativefixture.RequireLifecycleClean(t, nativefixture.RunLifecycleCommand(t, "status", "--manifest", manifest, "--check", "--json"))
			nativefixture.RunLifecycleCommand(t, "apply", "--manifest", manifest, "--yes")

			if fixture.manualSuffix != "" {
				manual := append(bytes.Clone(content), fixture.manualSuffix...)
				nativefixture.RequireSharedValues(t, fixture.syntax, manual, fixture.manualExpected, true)
			}
		})
	}
}

func requireNativeEmptyParent(t *testing.T, syntax configdocument.Syntax, content []byte, pointer string) {
	t.Helper()
	document, err := configdocument.Parse(t.Context(), content, syntax, nativefixture.LifecycleLimits())
	if err != nil {
		t.Fatal(err)
	}
	path, err := output.ParseKeyPath(pointer)
	if err != nil {
		t.Fatal(err)
	}
	value, present, err := document.Lookup(path)
	if err != nil || !present || !value.IsObject() || !value.IsAtomic() {
		t.Fatal("key removal violated empty-ancestor retention", pointer, err)
	}
}
