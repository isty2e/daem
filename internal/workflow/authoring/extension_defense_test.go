package authoring

import (
	"strings"
	"testing"
)

func TestExtensionRemoveBehaviorDefensivelyRejectsAmbiguousUnvalidatedBytes(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[extension]]
id = "duplicate"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "project"
source = { marketplace = "plugin@market" }

[[extension]]
id = "duplicate"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { host_source = "@acme/plugin" }
`)

	_, _, err := ApplyRemoveExtensionToManifest(original, RemoveExtensionRequest{ID: "duplicate"})
	if err == nil || !strings.Contains(err.Error(), `extension resource "duplicate" is ambiguous`) {
		t.Fatalf("err = %v, want defensive ambiguity error", err)
	}
}

func TestExtensionRemoveBehaviorRejectsInvalidExistingCarrierRowBeforeEditing(t *testing.T) {
	original := []byte(`version = 1
targets = ["opencode"]

[[extension]]
id = "formatter"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { marketplace = "plugin@market" }
`)

	_, _, err := ApplyRemoveExtensionToManifest(original, RemoveExtensionRequest{ID: "formatter"})
	if err == nil || !strings.Contains(err.Error(), "supports source.host_source, not source.marketplace") {
		t.Fatalf("err = %v, want invalid carrier row error", err)
	}
}
