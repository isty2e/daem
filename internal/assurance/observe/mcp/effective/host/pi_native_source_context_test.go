package host

import (
	"fmt"
	"path/filepath"
	"testing"

	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativeSourceContextRefusesAliasedAndWrongRoleLocators(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
		for _, retiring := range []bool{false, true} {
			for _, alias := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/retiring=%t/alias=%t", scope, retiring, alias), func(t *testing.T) {
					root := t.TempDir()
					workDir := filepath.Join(root, "project")
					agentRoot := filepath.Join(root, "agent")
					if alias {
						agentRoot = filepath.Join(workDir, ".pi")
					}
					globalPath := filepath.Join(agentRoot, "mcp.json")
					projectPath := filepath.Join(workDir, ".pi", "mcp.json")
					writeEffectiveConfig(t, globalPath, `{"mcpServers":{"context7":{"command":"node"}}}`)
					writeEffectiveConfig(t, projectPath, `{"mcpServers":{"context7":{"command":"node"}}}`)

					wrongRole := globalPath
					if scope == target.ScopeGlobal {
						wrongRole = projectPath
					}
					observation, err := ObservePiNative(PiNativeInput{
						Projection: piNativeProjection(t, scope), Codecs: aggregatecodec.Catalog(),
						WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: wrongRole, Retiring: retiring,
					})
					if err == nil || len(observation.Sources()) != 0 {
						t.Fatalf("ambiguous or wrong-role context returned source evidence: %v, %v", observation.Sources(), err)
					}
				})
			}
		}
	}
}
