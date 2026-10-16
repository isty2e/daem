package normalize

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPTransportRejectionsOmitValues(t *testing.T) {
	for _, values := range [][2]string{
		{"to\u200bken=opaque-one", "to\u200bken=opaque-two"},
		{`to\u200bken=opaque-one`, `to\u200bken=opaque-two`},
		{"custom=opaque-one", "custom=opaque-two"},
	} {
		var manifestReason, explicitReason string
		for index, transport := range values {
			raw := baseManifest(target.TargetCodex)
			raw.MCPServers = []declaration.MCPServer{{Name: "tools", Transport: transport, Command: declaration.NewMCPAmbientCommand("npx")}}
			_, manifestErr := Manifest(raw)
			_, _, explicitErr := ExplicitMCPServer(declarationcodec.MCPServer{
				Name: "tools", Targets: []string{"codex"}, Scope: "project", Transport: transport, Command: declaration.NewMCPAmbientCommand("npx"),
			})
			if manifestErr == nil || explicitErr == nil {
				t.Fatal("unsupported transport admitted by a declaration producer")
			}
			if index == 0 {
				manifestReason, explicitReason = manifestErr.Error(), explicitErr.Error()
			} else if manifestErr.Error() != manifestReason || explicitErr.Error() != explicitReason {
				t.Error("transport rejection reason depends on the rejected value")
			}
			for _, message := range []string{manifestErr.Error(), explicitErr.Error()} {
				if strings.Contains(message, "opaque-one") || strings.Contains(message, "opaque-two") {
					t.Error("transport diagnostic echoes rejected payload")
				}
			}
		}
		if !strings.Contains(manifestReason, "mcp_server[0].transport") || !strings.Contains(explicitReason, "mcp-server transport") {
			t.Error("transport rejection lost its declaration context")
		}
	}
}
