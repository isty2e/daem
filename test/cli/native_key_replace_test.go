package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
	"github.com/pelletier/go-toml/v2"
)

func TestNativeKeyReplacementCLI(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	workspace := filepath.Join(root, "workspace")
	manifest := filepath.Join(workspace, "daem.toml")
	destination := filepath.Join(workspace, ".codex", "config.toml")
	testkit.WriteFile(t, workspace, "daem.toml", `version = 1
targets = ["codex"]

[[host_config]]
id = "preferences"
path = "config.toml"
source = "./preferences.toml"
ownership = "keys"
`)
	testkit.WriteFile(t, workspace, "preferences.toml", "model = \"declared\"\n[ tui ]\nnotifications = false\n")
	testkit.WriteFile(t, workspace, ".codex/config.toml", "# keep me\nmodel = \"local\"\nunmanaged = 1\n")
	if code, stdout, stderr := runOwnershipCLI("lock", "--manifest", manifest); code != 0 {
		t.Fatalf("lock failed: %d %s %s", code, stdout, stderr)
	}
	read := func() []byte {
		t.Helper()
		content, err := os.ReadFile(destination)
		if err != nil {
			t.Fatal(err)
		}
		return content
	}
	preview := func() string {
		t.Helper()
		code, stdout, stderr := runOwnershipCLI("replace", "host-config", "preferences", "--manifest", manifest, "--key", "/model", "--dry-run", "--json")
		if code != 0 {
			t.Fatalf("replacement preview failed: %d %s %s", code, stdout, stderr)
		}
		var result struct {
			Fingerprint string `json:"fingerprint"`
		}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil || !strings.HasPrefix(result.Fingerprint, "v1:") {
			t.Fatalf("replacement preview has no versioned approval fingerprint: %s %v", stdout, err)
		}
		return result.Fingerprint
	}
	before := string(read())
	fingerprint := preview()
	if string(read()) != before {
		t.Fatal("preview modified the host document")
	}
	if code, _, _ := runOwnershipCLI("replace", "host-config", "preferences", "--manifest", manifest, "--key", "/model", "--yes"); code == 0 || string(read()) != before {
		t.Fatal("bare yes authorized an unreviewed replacement")
	}
	// Fresh unrelated edits must survive and must not invalidate selected consent.
	testkit.WriteFile(t, workspace, ".codex/config.toml", "# fresh comment\nmodel = \"local\"\nunmanaged = 2\n")
	if fresh := preview(); fresh != fingerprint {
		t.Fatal("unrelated content changed replacement consent")
	}
	if code, stdout, stderr := runOwnershipCLI("replace", "host-config", "preferences", "--manifest", manifest, "--key", "/model", "--yes", "--expect-plan", fingerprint, "--json"); code != 0 {
		t.Fatalf("explicit takeover failed: %d %s %s", code, stdout, stderr)
	}
	var values struct {
		Model     string
		Unmanaged int
		TUI       map[string]any `toml:"tui"`
	}
	if err := toml.Unmarshal(read(), &values); err != nil || values.Model != "declared" || values.Unmanaged != 2 || len(values.TUI) != 0 || !strings.Contains(string(read()), "# fresh comment") {
		t.Fatalf("replacement exceeded scope or lost fresh unrelated content: %s %v", read(), err)
	}
	testkit.WriteFile(t, workspace, ".codex/config.toml", "# fresh comment\nmodel = \"later-local\"\nunmanaged = 2\n")
	if code, _, _ := runOwnershipCLI("apply", "--manifest", manifest, "--yes"); code == 0 {
		t.Fatal("ordinary apply overwrote managed drift")
	}
	fingerprint = preview()
	testkit.WriteFile(t, workspace, ".codex/config.toml", "model = \"third\"\nunmanaged = 2\n")
	if code, _, _ := runOwnershipCLI("replace", "host-config", "preferences", "--manifest", manifest, "--key", "/model", "--yes", "--expect-plan", fingerprint); code == 0 {
		t.Fatal("changed selected value accepted stale consent")
	}
	fingerprint = preview()
	if code, stdout, stderr := runOwnershipCLI("replace", "host-config", "preferences", "--manifest", manifest, "--key", "/model", "--yes", "--expect-plan", fingerprint); code != 0 {
		t.Fatalf("managed drift replacement failed: %d %s %s", code, stdout, stderr)
	}
	if err := toml.Unmarshal(read(), &values); err != nil || values.Model != "declared" {
		t.Fatal("managed replacement did not restore declared value", err)
	}
}
