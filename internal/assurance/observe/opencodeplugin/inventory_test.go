package opencodeplugin_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	observeopencode "github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/target"
)

func TestReadInventoryPreservesPhysicalRowsAndLoadIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configDirectory := filepath.Join(root, ".opencode")
	if err := os.MkdirAll(configDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	serverPath := filepath.Join(configDirectory, "opencode.jsonc")
	if err := os.WriteFile(serverPath, []byte(`{
  "plugin": ["@acme/tool@1.2.3", "./plugins/local.ts"]
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	inventory, err := observeopencode.ReadInventory(observeopencode.InventoryInput{
		ManifestRoot: root,
		Scope:        target.ScopeProject,
	})
	if err != nil {
		t.Fatalf("ReadInventory: %v", err)
	}
	documents := inventory.Documents()
	if len(documents) != 2 {
		t.Fatalf("Documents = %#v", documents)
	}
	if documents[0].Kind() != opencodeconfig.ConfigServer ||
		documents[0].Path() != serverPath ||
		!documents[0].Exists() {
		t.Fatalf("server document = %#v", documents[0])
	}
	entries := documents[0].Entries()
	if len(entries) != 2 {
		t.Fatalf("server entries = %#v", entries)
	}
	if entries[0].Source() != "@acme/tool@1.2.3" ||
		entries[0].HostLoadIdentity() != "@acme/tool" {
		t.Fatalf("package entry = %#v", entries[0])
	}
	wantLocal := "file://" + filepath.ToSlash(
		filepath.Join(configDirectory, "plugins", "local.ts"),
	)
	if entries[1].Source() != "./plugins/local.ts" ||
		entries[1].HostLoadIdentity() != wantLocal {
		t.Fatalf("local entry = (%q, %q)", entries[1].Source(), entries[1].HostLoadIdentity())
	}
	if documents[1].Kind() != opencodeconfig.ConfigTUI ||
		documents[1].Exists() ||
		len(documents[1].Entries()) != 0 {
		t.Fatalf("TUI document = %#v", documents[1])
	}
	if documents[0].Revision() == documents[1].Revision() {
		t.Fatal("present and missing document revisions unexpectedly match")
	}
}

func TestReadInventoryObservesEveryExistingJSONAndJSONCCandidate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configDirectory := filepath.Join(root, ".opencode")
	for name, content := range map[string]string{
		"opencode.json":  `{"plugin":["server-json"]}`,
		"opencode.jsonc": `{"plugin":["server-jsonc"]}`,
		"tui.json":       `{"plugin":["tui-json"]}`,
		"tui.jsonc":      `{"plugin":["tui-jsonc"]}`,
	} {
		if err := os.MkdirAll(configDirectory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(configDirectory, name),
			[]byte(content),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}

	inventory, err := observeopencode.ReadInventory(observeopencode.InventoryInput{
		ManifestRoot: root,
		Scope:        target.ScopeProject,
	})
	if err != nil {
		t.Fatalf("ReadInventory: %v", err)
	}
	documents := inventory.Documents()
	if len(documents) != 4 {
		t.Fatalf("Documents = %#v, want four loaded candidates", documents)
	}
	for index, name := range []string{
		"opencode.json",
		"opencode.jsonc",
		"tui.json",
		"tui.jsonc",
	} {
		if documents[index].Path() != filepath.Join(configDirectory, name) ||
			!documents[index].Exists() ||
			len(documents[index].Entries()) != 1 {
			t.Fatalf("document[%d] = %#v", index, documents[index])
		}
	}
}

func TestReadInventoryFailsClosedForSelectedSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configDirectory := filepath.Join(root, ".opencode")
	if err := os.MkdirAll(configDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(root, "real.json")
	if err := os.WriteFile(targetPath, []byte(`{"plugin":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, filepath.Join(configDirectory, "opencode.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err := observeopencode.ReadInventory(observeopencode.InventoryInput{
		ManifestRoot: root,
		Scope:        target.ScopeProject,
	})
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("ReadInventory error = %v", err)
	}
}

func TestReadInventoryRejectsMalformedSelectedDocument(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configDirectory := filepath.Join(root, ".opencode")
	if err := os.MkdirAll(configDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(configDirectory, "opencode.json"),
		[]byte(""),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	_, err := observeopencode.ReadInventory(observeopencode.InventoryInput{
		ManifestRoot: root,
		Scope:        target.ScopeProject,
	})
	if err == nil || !strings.Contains(err.Error(), "parse OpenCode config JSONC") {
		t.Fatalf("ReadInventory error = %v", err)
	}
}
