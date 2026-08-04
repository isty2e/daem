package clijson

import (
	"encoding/json"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/contractversion"
)

func DecodeManifestAuthoring(t testing.TB, content []byte) clipresent.ManifestAuthoringJSONOutput {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatalf("Unmarshal fields returned error: %v\n%s", err, content)
	}
	if _, exists := fields["next_steps"]; exists {
		t.Fatalf("authoring JSON contains removed human next_steps field: %s", content)
	}
	var payload clipresent.ManifestAuthoringJSONOutput
	if err := json.Unmarshal(content, &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v\n%s", err, content)
	}
	requireSchemaVersion(t, "manifest authoring", payload.SchemaVersion, contractversion.ManifestAuthoringJSON)
	return payload
}
