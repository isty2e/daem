package cli_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestNativeOutputInventoryDisplaysDeclarationAndLiteralKey(t *testing.T) {
	fixture := nativePresentationFixture(t, "model='native-test'\n[ext]\n'a/b'='retained'\n", "foreign=true\n")
	runNativeLifecycleCommand(t, "apply", "--manifest", fixture.manifest, "--yes")
	human := runNativeLifecycleCommand(t, "list", "outputs", "--manifest", fixture.manifest)
	if !strings.Contains(human, `resource="host_config/preferences"`) || !strings.Contains(human, "key: model") || !strings.Contains(human, `key: ext."a/b"`) || strings.Contains(human, "key-v1.") {
		t.Fatalf("output inventory did not use readable key identity: %s", human)
	}
	var payload struct {
		Managed []struct {
			ResourceID string `json:"resource_id"`
			Subject    string `json:"subject"`
			Resource   struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"resource"`
			KeyPath []string `json:"key_path"`
		} `json:"managed"`
	}
	content := runNativeLifecycleCommand(t, "list", "outputs", "--manifest", fixture.manifest, "--json")
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Managed) != 2 {
		t.Fatalf("key rows were grouped or dropped in JSON: %s", content)
	}
	for _, row := range payload.Managed {
		if row.Resource.Kind != "host_config" || row.Resource.Name != "preferences" || row.ResourceID != "" || !strings.Contains(row.Subject, "key-v1.") {
			t.Fatalf("decoded identity replaced or omitted existing machine identity: %s", content)
		}
		if !slices.Equal(row.KeyPath, []string{"model"}) && !slices.Equal(row.KeyPath, []string{"ext", "a/b"}) {
			t.Fatalf("literal key path changed: %#v", row.KeyPath)
		}
	}
}
