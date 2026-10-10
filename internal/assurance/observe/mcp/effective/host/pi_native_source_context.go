package host

import (
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/target"
)

type piNativeSourceContext struct {
	sources       [2]normalSourceSpec
	selectedIndex int
}

func newPiNativeSourceContext(scope target.Scope, workDir, agentRoot, selectedPath string) (piNativeSourceContext, error) {
	selected := 0
	switch scope {
	case target.ScopeGlobal:
	case target.ScopeProject:
		selected = 1
	default:
		return piNativeSourceContext{}, fmt.Errorf("unsupported native Pi MCP scope %q", scope)
	}
	sources := [2]normalSourceSpec{
		{id: "pi-native-global", path: filepath.Join(agentRoot, "mcp.json"), shared: true},
		{id: "pi-native-project", path: filepath.Join(workDir, ".pi", "mcp.json")},
	}
	globalKey, err := mutation.CanonicalDirectoryEntryKey(sources[0].path)
	if err != nil {
		return piNativeSourceContext{}, fmt.Errorf("native Pi global config address cannot be observed: %w", err)
	}
	projectKey, err := mutation.CanonicalDirectoryEntryKey(sources[1].path)
	if err != nil {
		return piNativeSourceContext{}, fmt.Errorf("native Pi project config address cannot be observed: %w", err)
	}
	if globalKey == projectKey {
		return piNativeSourceContext{}, fmt.Errorf("native Pi global and project config paths must be distinct")
	}
	if selectedPath != sources[selected].path {
		return piNativeSourceContext{}, fmt.Errorf("managed native Pi path differs from its recorded scope's agent/project root")
	}
	return piNativeSourceContext{sources: sources, selectedIndex: selected}, nil
}
