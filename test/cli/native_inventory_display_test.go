package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestNativeOutputInventoryDisplaysDeclarationAndLiteralKey(t *testing.T) {
	fixture := nativefixture.NewPresentation(t, "model='native-test'\n[ext]\n'a/b'='retained'\n", "foreign=true\n")
	nativefixture.RunLifecycleCommand(t, "apply", "--manifest", fixture.Manifest, "--yes")
	human := nativefixture.RunLifecycleCommand(t, "list", "outputs", "--manifest", fixture.Manifest)
	if !strings.Contains(human, `resource="agent-config/preferences"`) || !strings.Contains(human, "key: model") || !strings.Contains(human, `key: ext."a/b"`) || strings.Contains(human, "key-v1.") {
		t.Fatalf("output inventory did not use readable key identity: %s", human)
	}
	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Managed       []struct {
			ResourceID string `json:"resource_id"`
			Subject    string `json:"subject"`
			Resource   struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"resource"`
			KeyPath []string `json:"key_path"`
		} `json:"managed"`
	}
	content := nativefixture.RunLifecycleCommand(t, "list", "outputs", "--manifest", fixture.Manifest, "--json")
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != contractversion.OutputInventoryJSON {
		t.Fatalf("output inventory schema_version = %d, want %d", payload.SchemaVersion, contractversion.OutputInventoryJSON)
	}
	if len(payload.Managed) != 2 {
		t.Fatalf("key rows were grouped or dropped in JSON: %s", content)
	}
	for _, row := range payload.Managed {
		if row.Resource.Kind != "agent-config" || row.Resource.Name != "preferences" || row.ResourceID != "" || !strings.Contains(row.Subject, "key-v1.") {
			t.Fatalf("decoded identity replaced or omitted existing machine identity: %s", content)
		}
		if !slices.Equal(row.KeyPath, []string{"model"}) && !slices.Equal(row.KeyPath, []string{"ext", "a/b"}) {
			t.Fatalf("literal key path changed: %#v", row.KeyPath)
		}
	}
}

func TestNativeInventoryClassifiesUnmanagedAndForeignKeys(t *testing.T) {
	const desired = `{"ext":{"a/b":1,"a":{"b":2}},"":3,"model":4,"~0":5}`
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			root := t.TempDir()
			nativefixture.SetRecoveryEnvironment(t, root)
			writeManifest := func(workspace, values string) string {
				testkit.WriteFile(t, workspace, "prefs.json", values)
				testkit.WriteFile(t, workspace, "daem.toml", "version=1\ntargets=['claude-code']\n[[agent_config]]\nid='prefs'\nscope='"+scope+"'\npath='settings.json'\nsource='"+filepath.Join(workspace, "prefs.json")+"'\n")
				manifest := filepath.Join(workspace, "daem.toml")
				nativefixture.RunLifecycleCommand(t, "lock", "--manifest", manifest)
				return manifest
			}
			workspace := filepath.Join(root, "consumer")
			manifest := writeManifest(workspace, desired)
			category, reason, owner := "unmanaged", "unmanaged_output_exists", ""
			if scope == "global" {
				owner = writeManifest(filepath.Join(root, "owner"), desired)
				nativefixture.RunLifecycleCommand(t, "apply", "--manifest", owner, "--yes")
				category, reason = "blocked", "ownership_conflict"
			} else {
				dir := filepath.Join(workspace, ".claude")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				testkit.WriteFile(t, dir, "settings.json", strings.Replace(desired, `"model":4`, `"model":99`, 1))
			}

			content := nativefixture.RunLifecycleCommand(t, "list", "outputs", "--manifest", manifest, "--target", "claude-code", "--json")
			var payload map[string]json.RawMessage
			if err := json.Unmarshal([]byte(content), &payload); err != nil {
				t.Fatal(err)
			}
			var rows []struct {
				KeyPath                       []string        `json:"key_path"`
				ContentPath                   json.RawMessage `json:"content_path"`
				ResourceID                    json.RawMessage `json:"resource_id"`
				Resource                      struct{ Kind, Name string }
				Subject, Reason, Detail, Hash string
			}
			var count int
			if err := json.Unmarshal(payload[category], &rows); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(payload[category+"_count"], &count); err != nil {
				t.Fatal(err)
			}
			want := [][]string{{"ext", "a/b"}, {"ext", "a", "b"}, {""}, {"~0"}}
			if scope == "global" {
				want = append(want, []string{"model"})
			}
			if len(rows) != len(want) || count != len(want) {
				t.Fatalf("unexpected %s count: %s", category, content)
			}
			subjects := map[string]bool{}
			for _, row := range rows {
				i := slices.IndexFunc(want, func(key []string) bool { return slices.Equal(key, row.KeyPath) })
				if i < 0 || row.ContentPath != nil || row.ResourceID != nil || row.Resource.Kind != "agent-config" || row.Resource.Name != "prefs" || row.Subject == "" || subjects[row.Subject] || row.Hash != "" || row.Reason != reason || !strings.Contains(row.Detail, owner) {
					t.Fatalf("unexpected %s row: %+v", category, row)
				}
				want = slices.Delete(want, i, i+1)
				subjects[row.Subject] = true
			}
		})
	}
}
