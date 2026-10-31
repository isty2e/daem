package host

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func qualifyPiNativeSettings(ctx context.Context, contract profile.PiMCPContract, scope target.Scope, workDir, agentRoot string, version profile.PiMCPVersion) error {
	return configFileReader(nil).qualifyPiNativeSettings(ctx, contract, scope, workDir, agentRoot, version)
}

func observePiNativeSettings(ctx context.Context, path string) (piNativeSettings, error) {
	return configFileReader(nil).observePiNativeSettings(ctx, path)
}

func TestPiNativeObservationUsesSelectedConfigReader(t *testing.T) {
	workDir, agentRoot := t.TempDir(), t.TempDir()
	projectSettings := filepath.Join(workDir, ".pi", "settings.json")
	projectMCP := filepath.Join(workDir, ".pi", "mcp.json")
	writeEffectiveConfig(t, projectSettings, `{"extensions":["-builtin:mcp"]}`)
	writeEffectiveConfig(t, projectMCP, `{"mcpServers":{"context7":{"command":"other"}}}`)
	reads := make(map[string]int)
	reader := configFileReader(func(ctx context.Context, path string, maximumBytes int64) ([]byte, bool, error) {
		reads[path]++
		switch path {
		case projectSettings:
			return []byte(`{}`), true, nil
		case projectMCP:
			return []byte(`{"mcpServers":{"context7":{"command":"node"}}}`), true, nil
		default:
			return filesnapshot.ReadRegularFileContext(ctx, path, maximumBytes)
		}
	})
	contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
	if err := reader.qualifyPiNativeSettings(t.Context(), contract, target.ScopeProject, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2")); err != nil {
		t.Fatal("qualification reread live settings instead of selected bytes", err)
	}
	observation, err := ObservePiNative(PiNativeInput{
		Context: t.Context(), ReadFile: reader, Projection: piNativeProjection(t, target.ScopeProject),
		Codecs: aggregatecodec.Catalog(), WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: projectMCP,
	})
	if err != nil || observation.State() != mcpeffective.StateExact || reads[projectSettings] != 1 || reads[projectMCP] != 1 {
		t.Fatal("native observation did not use its selected config reader", observation.State(), err, reads)
	}
	for _, failure := range []error{filesnapshot.ErrLimitExceeded, context.Canceled} {
		input := PiNativeInput{
			Context: t.Context(), Projection: piNativeProjection(t, target.ScopeProject), Codecs: aggregatecodec.Catalog(),
			WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: projectMCP,
			ReadFile: func(context.Context, string, int64) ([]byte, bool, error) { return nil, false, failure },
		}
		observation, err := ObservePiNative(input)
		if errors.Is(failure, context.Canceled) {
			if !errors.Is(err, failure) {
				t.Fatal("cancelled MCP read was converted into usable evidence", err)
			}
		} else if err != nil || observation.State() != mcpeffective.StateUnobservable {
			t.Fatal("failed MCP read was treated as absence", observation.State(), err)
		}
	}
}
