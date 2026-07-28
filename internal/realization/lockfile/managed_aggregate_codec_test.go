package lockfile

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
)

func TestMarshalRejectsMalformedMCPContributionsAcrossPlacements(t *testing.T) {
	tests := []struct {
		placement aggregate.MCPPlacementID
		canonical string
		reason    string
	}{
		{aggregate.MCPPlacementClaudeProject, `{"type":"stdio","command":"npx","env":{"API_TOKEN":"SECRET_CANARY"}}`, "secret_literal_forbidden"},
		{aggregate.MCPPlacementClaudeGlobal, `{"type":"stdio","command":"npx","env":{"API_TOKEN":"SECRET_CANARY"}}`, "secret_literal_forbidden"},
		{aggregate.MCPPlacementAntigravityGlobal, `{"type":"stdio","command":"npx","args":[]}`, "unsupported_managed_field"},
		{aggregate.MCPPlacementOpenCodeProject, `{"type":"local","command":["npx"],"environment":{"TOKEN":"SECRET_CANARY"}}`, "unsupported_managed_field"},
		{aggregate.MCPPlacementOpenCodeGlobal, `{"type":"local","command":["npx"],"environment":{"TOKEN":"SECRET_CANARY"}}`, "secret_literal_forbidden"},
		{aggregate.MCPPlacementCodexProject, "command = \"npx\"\nenv = { TOKEN = \"SECRET_CANARY\" }\n", "unsupported_managed_field"},
		{aggregate.MCPPlacementCodexGlobal, "command = \"npx\"\nenv = { TOKEN = \"SECRET_CANARY\" }\n", "secret_literal_forbidden"},
	}

	for _, test := range tests {
		t.Run(string(test.placement), func(t *testing.T) {
			contract := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
				PlacementID:         test.placement,
				ServerID:            "context7",
				LauncherCommand:     "npx",
				CanonicalProjection: test.canonical,
			})
			file := snapshottest.File(t, contract)

			_, err := Marshal(file)
			if err == nil || !strings.Contains(err.Error(), "aggregate codec "+test.reason) {
				t.Fatalf("Marshal error = %v, want canonical %s rejection", err, test.reason)
			}
			if strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("Marshal leaked secret canary: %q", err)
			}
		})
	}
}
