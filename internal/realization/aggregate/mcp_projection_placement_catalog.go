package aggregate

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

// ImplementedMCPPlacements returns implemented standalone MCP projection placements in stable order.
func ImplementedMCPPlacements() []MCPPlacement {
	return append([]MCPPlacement(nil), implementedMCPPlacements...)
}

// MCPPlacementForSubject returns the implemented placement named by a topology
// projection subject. Unknown namespaces and non-projection subjects are not
// placements.
func MCPPlacementForSubject(subject topology.SubjectID) (MCPPlacement, bool) {
	for _, placement := range implementedMCPPlacements {
		if topologymcp.IsProjectionFor(placement.Target(), placement.Scope(), subject) {
			return placement, true
		}
	}
	return MCPPlacement{}, false
}

// MCPPlacementForID returns the implemented placement row named by id.
func MCPPlacementForID(id MCPPlacementID) (MCPPlacement, bool) {
	for _, placement := range implementedMCPPlacements {
		if placement.id == id {
			return placement, true
		}
	}
	return MCPPlacement{}, false
}

// MCPPlacementForCodec returns the static placement row owning contractID.
func MCPPlacementForCodec(contractID CodecContractID) (MCPPlacement, bool) {
	for _, placement := range implementedMCPPlacements {
		if placement.CodecContractID() == contractID {
			return placement, true
		}
	}
	return MCPPlacement{}, false
}

// ImplementedMCPPlacement returns the implemented placement for target/scope.
func ImplementedMCPPlacement(selectedTarget target.Target, selectedScope target.Scope) (MCPPlacement, bool) {
	for _, placement := range implementedMCPPlacements {
		if placement.target == selectedTarget && placement.scope == selectedScope {
			return placement, true
		}
	}
	return MCPPlacement{}, false
}

// TargetHasImplementedMCPPlacement reports whether any implemented MCP row exists for target.
func TargetHasImplementedMCPPlacement(selectedTarget target.Target) bool {
	for _, placement := range implementedMCPPlacements {
		if placement.target == selectedTarget {
			return true
		}
	}
	return false
}

func mustMCPPlacement(input MCPPlacementInput) MCPPlacement {
	placement, err := NewMCPPlacement(input)
	if err != nil {
		panic(err)
	}
	return placement
}

var claudeMCPComparedFields = []string{
	"adapter_contract", "args", "command", "config_path", "content_path",
	"env", "scope", "server_id", "target", "type",
}

var antigravityMCPComparedFields = []string{
	"adapter_contract", "args", "command", "config_path", "content_path",
	"scope", "server_id", "target",
}

var openCodeMCPComparedFields = []string{
	"adapter_contract", "command_argv", "config_path", "content_path",
	"scope", "server_id", "target", "type",
}

var codexProjectMCPComparedFields = []string{
	"adapter_contract", "args", "command", "config_path", "content_path",
	"scope", "server_id", "target",
}

var codexGlobalMCPComparedFields = []string{
	"adapter_contract", "args", "command", "config_path", "content_path",
	"env_vars", "scope", "server_id", "target",
}

var implementedMCPPlacements = []MCPPlacement{
	mustMCPPlacement(MCPPlacementInput{
		ID:                     MCPPlacementClaudeProject,
		Target:                 target.TargetClaudeCode,
		Scope:                  target.ScopeProject,
		ConfigLayer:            MCPConfigLayerClaudeProjectFile,
		ConfigPath:             ClaudeProjectMCPConfigPath,
		MergeUnit:              MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcpServers",
		SiblingRetention:       MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        MCPCodecClaudeProjectStdio,
		ComparedFields:         claudeMCPComparedFields,
		Absence:                MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    MCPEnvMappingAliased,
		EnvReferenceResolution: MCPEnvResolutionHostRuntime,
	}),
	mustMCPPlacement(MCPPlacementInput{
		ID:                     MCPPlacementClaudeGlobal,
		Target:                 target.TargetClaudeCode,
		Scope:                  target.ScopeGlobal,
		ConfigLayer:            MCPConfigLayerClaudeUserSharedJSON,
		ConfigPath:             ClaudeGlobalMCPConfigPath,
		MergeUnit:              MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcpServers",
		SiblingRetention:       MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        MCPCodecClaudeGlobalStdio,
		ComparedFields:         claudeMCPComparedFields,
		Absence:                MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    MCPEnvMappingUnsupported,
		EnvReferenceResolution: MCPEnvResolutionUnavailable,
	}),
	mustMCPPlacement(MCPPlacementInput{
		ID:                     MCPPlacementAntigravityGlobal,
		Target:                 target.TargetAntigravityCLI,
		Scope:                  target.ScopeGlobal,
		ConfigLayer:            MCPConfigLayerAntigravityGlobalDefaultFile,
		ConfigPath:             AntigravityGlobalMCPConfigPath,
		MergeUnit:              MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcpServers",
		SiblingRetention:       MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        MCPCodecAntigravityGlobalCommand,
		ComparedFields:         antigravityMCPComparedFields,
		Absence:                MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    MCPEnvMappingUnsupported,
		EnvReferenceResolution: MCPEnvResolutionUnavailable,
	}),
	mustMCPPlacement(MCPPlacementInput{
		ID:                     MCPPlacementOpenCodeProject,
		Target:                 target.TargetOpenCode,
		Scope:                  target.ScopeProject,
		ConfigLayer:            MCPConfigLayerOpenCodeProjectFile,
		ConfigPath:             OpenCodeProjectMCPConfigPath,
		ConflictingConfigPath:  openCodeProjectMCPConflictPath,
		MergeUnit:              MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcp",
		SiblingRetention:       MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        MCPCodecOpenCodeProjectLocal,
		ComparedFields:         openCodeMCPComparedFields,
		Absence:                MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    MCPEnvMappingUnsupported,
		EnvReferenceResolution: MCPEnvResolutionUnavailable,
	}),
	mustMCPPlacement(MCPPlacementInput{
		ID:                     MCPPlacementOpenCodeGlobal,
		Target:                 target.TargetOpenCode,
		Scope:                  target.ScopeGlobal,
		ConfigLayer:            MCPConfigLayerOpenCodeGlobalDefaultJSON,
		ConfigPath:             OpenCodeGlobalMCPConfigPath,
		ConflictingConfigPath:  openCodeGlobalMCPConflictPath,
		MergeUnit:              MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcp",
		SiblingRetention:       MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        MCPCodecOpenCodeGlobalLocal,
		ComparedFields:         openCodeMCPComparedFields,
		Absence:                MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    MCPEnvMappingUnsupported,
		EnvReferenceResolution: MCPEnvResolutionUnavailable,
	}),
	mustMCPPlacement(MCPPlacementInput{
		ID:                     MCPPlacementCodexProject,
		Target:                 target.TargetCodex,
		Scope:                  target.ScopeProject,
		ConfigLayer:            MCPConfigLayerCodexProjectFile,
		ConfigPath:             CodexProjectMCPConfigPath,
		MergeUnit:              MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcp_servers",
		SiblingRetention:       MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        MCPCodecCodexProjectStdioCommand,
		ComparedFields:         codexProjectMCPComparedFields,
		Absence:                MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    MCPEnvMappingUnsupported,
		EnvReferenceResolution: MCPEnvResolutionUnavailable,
	}),
	mustMCPPlacement(MCPPlacementInput{
		ID:                     MCPPlacementCodexGlobal,
		Target:                 target.TargetCodex,
		Scope:                  target.ScopeGlobal,
		ConfigLayer:            MCPConfigLayerCodexGlobalDefaultFile,
		ConfigPath:             CodexGlobalMCPConfigPath,
		MergeUnit:              MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcp_servers",
		SiblingRetention:       MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        MCPCodecCodexGlobalStdioEnvVars,
		ComparedFields:         codexGlobalMCPComparedFields,
		Absence:                MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    MCPEnvMappingSameName,
		EnvReferenceResolution: MCPEnvResolutionHostRuntime,
	}),
}

func init() {
	if err := validateMCPPlacementCatalog(implementedMCPPlacements); err != nil {
		panic(err)
	}
}

func validateMCPPlacementCatalog(placements []MCPPlacement) error {
	ids := make(map[MCPPlacementID]struct{}, len(placements))
	targetScopes := make(map[string]MCPPlacementID, len(placements))
	configPaths := make(map[output.Destination]MCPPlacementID, len(placements))
	conflictingConfigPaths := make(map[output.Destination]MCPPlacementID, len(placements))
	codecContracts := make(map[CodecContractID]MCPPlacementID, len(placements))
	for _, placement := range placements {
		if err := placement.Validate(); err != nil {
			return err
		}
		if _, ok := ids[placement.id]; ok {
			return fmt.Errorf("MCP placements share placement id %q", placement.id)
		}
		ids[placement.id] = struct{}{}
		targetScope := string(placement.target) + "/" + string(placement.scope)
		if existing, ok := targetScopes[targetScope]; ok {
			return fmt.Errorf("MCP placements %q and %q share target/scope %s", existing, placement.id, targetScope)
		}
		targetScopes[targetScope] = placement.id
		configPath := placement.ConfigPath()
		if existing, ok := configPaths[configPath]; ok {
			return fmt.Errorf("MCP placements %q and %q share config path %q", existing, placement.id, configPath)
		}
		configPaths[configPath] = placement.id
		conflictingConfigPath, hasConflict := placement.ConflictingConfigPath()
		if hasConflict {
			if existing, ok := conflictingConfigPaths[conflictingConfigPath]; ok {
				return fmt.Errorf("MCP placements %q and %q share conflicting config path %q", existing, placement.id, conflictingConfigPath)
			}
			conflictingConfigPaths[conflictingConfigPath] = placement.id
		}
		codecContractID := placement.CodecContractID()
		if existing, ok := codecContracts[codecContractID]; ok {
			return fmt.Errorf("MCP placements %q and %q share codec contract %q", existing, placement.id, codecContractID)
		}
		codecContracts[codecContractID] = placement.id
	}
	for _, placement := range placements {
		conflictingConfigPath, hasConflict := placement.ConflictingConfigPath()
		if !hasConflict {
			continue
		}
		if owner, ok := configPaths[conflictingConfigPath]; ok {
			return fmt.Errorf(
				"MCP placement %q conflicting config path %q is owned by placement %q",
				placement.id,
				conflictingConfigPath,
				owner,
			)
		}
	}
	return nil
}
