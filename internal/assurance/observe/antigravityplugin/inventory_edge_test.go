package antigravityplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventoryRejectsMalformedAndSymlinkedHostState(t *testing.T) {
	t.Run("malformed imports", func(t *testing.T) {
		paths, _, _ := antigravityInventoryFixture(t, "guidance@google")
		writeAntigravityImportManifest(t, paths, `{"imports":[{"source":"antigravity"}]}`)
		if _, err := ReadInventory(paths); err == nil {
			t.Fatal("malformed import row was accepted")
		}
	})

	t.Run("symlinked import manifest", func(t *testing.T) {
		paths, _, _ := antigravityInventoryFixture(t, "guidance@google")
		targetPath := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(targetPath, []byte(`{"imports":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(paths.ImportManifestPath()), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(targetPath, paths.ImportManifestPath()); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadInventory(paths); err == nil {
			t.Fatal("symlinked import manifest was accepted")
		}
	})

	t.Run("symlinked plugin directory", func(t *testing.T) {
		paths, key, carrier := antigravityInventoryFixture(t, "guidance@google")
		writeAntigravityImportManifest(t, paths, `{"imports":[{"name":"guidance"}]}`)
		targetDirectory := t.TempDir()
		if err := os.WriteFile(
			filepath.Join(targetDirectory, "plugin.json"),
			[]byte(`{"name":"guidance"}`),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
		pluginDirectory, err := paths.PluginDirectoryPath("guidance")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(pluginDirectory), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(targetDirectory, pluginDirectory); err != nil {
			t.Fatal(err)
		}
		inventory := mustReadInventory(t, paths)
		if _, err := inventory.CorrelateDesired(key, carrier); err == nil {
			t.Fatal("symlinked plugin directory was accepted")
		}
	})
}

func TestInventoryFingerprintDetectsManifestDrift(t *testing.T) {
	paths, _, _ := antigravityInventoryFixture(t, "guidance@google")
	writeAntigravityImportManifest(t, paths, `{"imports":[]}`)
	before := mustReadInventory(t, paths)
	writeAntigravityImportManifest(t, paths, `{"imports":null}`)
	after := mustReadInventory(t, paths)
	if before.Equal(after) {
		t.Fatal("different Antigravity import manifests compared equal")
	}
}

func TestInventoryRejectsResourceAndStructureBombs(t *testing.T) {
	t.Run("oversized manifest", func(t *testing.T) {
		paths, _, _ := antigravityInventoryFixture(t, "guidance@google")
		content := make([]byte, maximumInventoryBytes+1)
		for index := range content {
			content[index] = ' '
		}
		if err := os.MkdirAll(filepath.Dir(paths.ImportManifestPath()), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths.ImportManifestPath(), content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadInventory(paths); err == nil {
			t.Fatal("oversized Antigravity manifest was accepted")
		}
	})

	t.Run("excessive nesting", func(t *testing.T) {
		paths, _, _ := antigravityInventoryFixture(t, "guidance@google")
		content := `{"imports":[],"future":` +
			strings.Repeat("[", maximumInventoryDepth+1) +
			`0` +
			strings.Repeat("]", maximumInventoryDepth+1) +
			`}`
		writeAntigravityImportManifest(t, paths, content)
		if _, err := ReadInventory(paths); err == nil {
			t.Fatal("overly deep Antigravity manifest was accepted")
		}
	})

	t.Run("duplicate imports field", func(t *testing.T) {
		paths, _, _ := antigravityInventoryFixture(t, "guidance@google")
		writeAntigravityImportManifest(
			t,
			paths,
			`{"imports":[],"imports":[{"name":"guidance"}]}`,
		)
		if _, err := ReadInventory(paths); err == nil {
			t.Fatal("duplicate Antigravity imports field was accepted")
		}
	})
}
