package mcpcodec

import (
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestPiNativeDocumentPreservesUnsupportedUnmanagedNames(t *testing.T) {
	operations, ok := ImplementedMCPPlacementOperationsForContract(aggregate.MCPPlacementPiProject, aggregate.MCPCodecPiNativeStdio)
	if !ok {
		t.Fatal("native operations unavailable")
	}
	content := []byte(`{"mcpServers":{"unsupported.name":{"command":"echo"},"context7":{"command":"node"}}}`)
	observed, err := operations.observeCanonical(content, []string{"context7"})
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := observed.CanonicalEntry("context7"); err != nil || !present {
		t.Fatalf("supported managed entry was not observed: present=%t, err=%v", present, err)
	}
	removed, err := operations.removeProjection(content, "context7")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Servers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(removed, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Servers) != 1 || config.Servers["unsupported.name"].Command != "echo" {
		t.Fatalf("removal changed unmanaged entry: %s", removed)
	}
}
