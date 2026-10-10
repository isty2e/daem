package host

import (
	"path/filepath"
	"testing"

	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativeRetirementRetainsOverrideDependencyAndOpaqueEvidence(t *testing.T) {
	for _, test := range []struct {
		name, entry string
		state       mcpeffective.State
		dependent   bool
		higher      bool
	}{
		{"empty override", `{}`, mcpeffective.StateExact, true, false},
		{"enabled override", `{"enabled":true}`, mcpeffective.StateExact, true, false},
		{"disabled override", `{"enabled":false}`, mcpeffective.StateExact, true, false},
		{"exposure override", `{"exposure":"direct"}`, mcpeffective.StateExact, true, false},
		{"tool override", `{"toolExposure":{"x":"direct"}}`, mcpeffective.StateExact, true, false},
		{"invalid enabled", `{"enabled":"yes"}`, mcpeffective.StateUnobservable, false, false},
		{"unsupported partial field", `{"args":[]}`, mcpeffective.StateUnobservable, false, false},
		{"full replacement", `{"command":"echo"}`, mcpeffective.StateConflicting, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			workDir, agentRoot := filepath.Join(root, "project"), filepath.Join(root, "agent")
			globalPath := filepath.Join(agentRoot, "mcp.json")
			writeEffectiveConfig(t, globalPath, `{"mcpServers":{"context7":{"command":"node"}}}`)
			writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "mcp.json"), `{"mcpServers":{"context7":`+test.entry+`}}`)
			observation, err := ObservePiNative(PiNativeInput{
				Projection: piNativeProjection(t, target.ScopeGlobal), Codecs: aggregatecodec.Catalog(),
				WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: globalPath, Retiring: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if observation.State() != test.state || (len(observation.DependentOverrideSources()) > 0) != test.dependent || observation.HigherConflictPresent() != test.higher {
				t.Fatalf("retirement evidence = %#v; dependent=%t higher=%t", observation, test.dependent, test.higher)
			}
		})
	}
}
