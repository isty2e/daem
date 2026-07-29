package mcpcodec

import (
	"fmt"
	"path/filepath"
	"strings"

	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// CanonicalMCPBindingContribution encodes one admitted binding through the
// private host codec selected by its canonical placement row.
func CanonicalMCPBindingContribution(
	server desiredmcp.Server,
	binding desiredmcp.Binding,
	placement aggregate.MCPPlacement,
) ([]byte, error) {
	if err := server.Validate(); err != nil {
		return nil, err
	}
	if !server.OwnsBinding(binding) {
		return nil, fmt.Errorf("MCP binding is not owned by server %q", server.ID().Name())
	}
	selected, err := aggregate.MCPPlacementForBinding(binding)
	if err != nil {
		return nil, err
	}
	if selected.ID() != placement.ID() {
		return nil, fmt.Errorf("MCP placement %q does not match selected placement %q", placement.ID(), selected.ID())
	}
	stdio, ok := binding.Transport().Stdio()
	if !ok {
		return nil, fmt.Errorf("unsupported MCP transport %q", binding.Transport().Kind())
	}
	serverID := server.ID().Name()
	command := stdio.Command().Executable()
	args := stdio.Args()
	adapterContract := string(placement.CodecContractID())
	noEnvProjection := MCPNoEnvServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            args,
		AdapterContract: adapterContract,
	}
	switch placement.ID() {
	case aggregate.MCPPlacementClaudeProject:
		if err := requireMCPEnvReferenceCodecContract(
			placement,
			aggregate.MCPEnvMappingAliased,
			aggregate.MCPEnvResolutionHostRuntime,
		); err != nil {
			return nil, err
		}
		env, err := canonicalMCPBindingEnv(stdio.Env())
		if err != nil {
			return nil, err
		}
		return CanonicalClaudeProjectMCPServerEntry(ClaudeProjectMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			Env:             env,
			AdapterContract: adapterContract,
		})
	case aggregate.MCPPlacementClaudeGlobal:
		if err := requireMCPEnvReferenceCodecContract(
			placement,
			aggregate.MCPEnvMappingAliased,
			aggregate.MCPEnvResolutionHostRuntime,
		); err != nil {
			return nil, err
		}
		env, err := canonicalMCPBindingEnv(stdio.Env())
		if err != nil {
			return nil, err
		}
		return CanonicalClaudeGlobalMCPServerEntry(ClaudeGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			Env:             env,
			AdapterContract: adapterContract,
		})
	case aggregate.MCPPlacementAntigravityGlobal:
		if err := requireMCPEnvReferenceCodecContract(
			placement,
			aggregate.MCPEnvMappingSameName,
			aggregate.MCPEnvResolutionHostRuntime,
		); err != nil {
			return nil, err
		}
		return CanonicalAntigravityGlobalMCPServerEntry(AntigravityGlobalMCPServerProjection{
			ServerID:         serverID,
			Command:          command,
			Args:             args,
			EnvironmentNames: stdio.EnvironmentSourceNames(),
			AdapterContract:  adapterContract,
		})
	case aggregate.MCPPlacementOpenCodeProject:
		if err := requireMCPNoEnvReferenceCodecContract(placement); err != nil {
			return nil, err
		}
		return CanonicalOpenCodeProjectMCPServerEntry(noEnvProjection)
	case aggregate.MCPPlacementOpenCodeGlobal:
		if err := requireMCPEnvReferenceCodecContract(
			placement,
			aggregate.MCPEnvMappingAliased,
			aggregate.MCPEnvResolutionHostRuntime,
		); err != nil {
			return nil, err
		}
		environment, err := canonicalOpenCodeMCPBindingEnvironment(stdio.Env())
		if err != nil {
			return nil, err
		}
		return CanonicalOpenCodeGlobalMCPServerEntry(OpenCodeGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			Environment:     environment,
			AdapterContract: adapterContract,
		})
	case aggregate.MCPPlacementCodexProject:
		if err := requireMCPNoEnvReferenceCodecContract(placement); err != nil {
			return nil, err
		}
		return CanonicalCodexProjectMCPServerEntry(noEnvProjection)
	case aggregate.MCPPlacementCodexGlobal:
		if err := requireMCPEnvReferenceCodecContract(
			placement,
			aggregate.MCPEnvMappingSameName,
			aggregate.MCPEnvResolutionHostRuntime,
		); err != nil {
			return nil, err
		}
		return CanonicalCodexGlobalMCPServerEntry(CodexGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			EnvVars:         stdio.EnvironmentSourceNames(),
			AdapterContract: adapterContract,
		})
	case aggregate.MCPPlacementPiProject, aggregate.MCPPlacementPiGlobal:
		if err := requireMCPEnvReferenceCodecContract(
			placement,
			aggregate.MCPEnvMappingAliased,
			aggregate.MCPEnvResolutionHostRuntime,
		); err != nil {
			return nil, err
		}
		env, err := canonicalMCPBindingEnv(stdio.Env())
		if err != nil {
			return nil, err
		}
		return CanonicalPiMCPAdapterServerEntry(PiMCPAdapterServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			Env:             env,
			AdapterContract: adapterContract,
		})
	default:
		return nil, fmt.Errorf("unsupported MCP placement %q", placement.ID())
	}
}

func requireMCPNoEnvReferenceCodecContract(placement aggregate.MCPPlacement) error {
	return requireMCPEnvReferenceCodecContract(
		placement,
		aggregate.MCPEnvMappingUnsupported,
		aggregate.MCPEnvResolutionUnavailable,
	)
}

func requireMCPEnvReferenceCodecContract(
	placement aggregate.MCPPlacement,
	mapping aggregate.MCPEnvReferenceMapping,
	resolution aggregate.MCPEnvReferenceResolution,
) error {
	contract := placement.EnvReferenceContract()
	if contract.Mapping() == mapping && contract.Resolution() == resolution {
		return nil
	}
	return fmt.Errorf(
		"MCP placement %q environment-reference contract %q/%q does not match codec capability %q/%q",
		placement.ID(),
		contract.Mapping(),
		contract.Resolution(),
		mapping,
		resolution,
	)
}

// CanonicalClaudeProjectMCPServerEntry returns the canonical managed server entry bytes.
func CanonicalClaudeProjectMCPServerEntry(projection ClaudeProjectMCPServerProjection) ([]byte, error) {
	entry, err := canonicalServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return canonicalJSON(entry)
}

// CanonicalClaudeGlobalMCPServerEntry returns the canonical managed server entry bytes.
func CanonicalClaudeGlobalMCPServerEntry(projection ClaudeGlobalMCPServerProjection) ([]byte, error) {
	entry, err := canonicalClaudeGlobalMCPServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return canonicalJSON(entry)
}

// CanonicalAntigravityGlobalMCPServerEntry returns the canonical managed server entry bytes.
func CanonicalAntigravityGlobalMCPServerEntry(projection AntigravityGlobalMCPServerProjection) ([]byte, error) {
	entry, err := canonicalAntigravityGlobalMCPServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return canonicalJSON(entry)
}

// CanonicalOpenCodeProjectMCPServerEntry returns the canonical managed server entry bytes.
func CanonicalOpenCodeProjectMCPServerEntry(projection MCPNoEnvServerProjection) ([]byte, error) {
	entry, err := canonicalOpenCodeProjectMCPServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return canonicalJSON(entry)
}

// CanonicalOpenCodeGlobalMCPServerEntry returns the canonical managed server entry bytes.
func CanonicalOpenCodeGlobalMCPServerEntry(projection OpenCodeGlobalMCPServerProjection) ([]byte, error) {
	entry, err := canonicalOpenCodeGlobalMCPServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return canonicalJSON(entry)
}

// CanonicalCodexProjectMCPServerEntry returns the canonical managed server entry bytes.
func CanonicalCodexProjectMCPServerEntry(projection MCPNoEnvServerProjection) ([]byte, error) {
	entry, err := canonicalCodexProjectMCPServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return encodeCodexProjectMCPServerEntry(entry)
}

// CanonicalCodexGlobalMCPServerEntry returns the canonical managed server entry bytes.
func CanonicalCodexGlobalMCPServerEntry(projection CodexGlobalMCPServerProjection) ([]byte, error) {
	entry, err := canonicalCodexGlobalMCPServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return encodeCodexGlobalMCPServerEntry(entry)
}

func canonicalServerEntry(projection ClaudeProjectMCPServerProjection) (ClaudeProjectMCPServerEntry, error) {
	if projection.AdapterContract != aggregate.ClaudeProjectMCPStdioAdapterV1 {
		return ClaudeProjectMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			"unsupported Claude project MCP adapter contract",
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return ClaudeProjectMCPServerEntry{}, err
	}
	if err := validateMCPCommand(projection.Command); err != nil {
		return ClaudeProjectMCPServerEntry{}, err
	}
	args := append([]string{}, projection.Args...)
	env, err := canonicalMCPEnv(projection.Env)
	if err != nil {
		return ClaudeProjectMCPServerEntry{}, err
	}
	return ClaudeProjectMCPServerEntry{
		Type:    claudeProjectMCPTransportStdio,
		Command: projection.Command,
		Args:    args,
		Env:     env,
	}, nil
}

func canonicalClaudeGlobalMCPServerEntry(projection ClaudeGlobalMCPServerProjection) (ClaudeGlobalMCPServerEntry, error) {
	if projection.AdapterContract != aggregate.ClaudeGlobalMCPStdioEnvAdapterV1 {
		return ClaudeGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			"unsupported Claude Code user/global MCP adapter contract",
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return ClaudeGlobalMCPServerEntry{}, err
	}
	if err := validateMCPCommand(projection.Command); err != nil {
		return ClaudeGlobalMCPServerEntry{}, err
	}
	env, err := canonicalMCPEnv(projection.Env)
	if err != nil {
		return ClaudeGlobalMCPServerEntry{}, err
	}
	return ClaudeGlobalMCPServerEntry{
		Type:    claudeProjectMCPTransportStdio,
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
		Env:     env,
	}, nil
}

func canonicalAntigravityGlobalMCPServerEntry(projection AntigravityGlobalMCPServerProjection) (AntigravityGlobalMCPServerEntry, error) {
	if projection.AdapterContract != aggregate.AntigravityGlobalMCPAmbientEnvV1 {
		return AntigravityGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			"unsupported Antigravity CLI global MCP adapter contract",
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return AntigravityGlobalMCPServerEntry{}, err
	}
	if err := validateMCPCommand(projection.Command); err != nil {
		return AntigravityGlobalMCPServerEntry{}, err
	}
	if _, err := canonicalSameNameMCPEnvironmentNames(projection.EnvironmentNames, "ambient_environment"); err != nil {
		return AntigravityGlobalMCPServerEntry{}, err
	}
	return AntigravityGlobalMCPServerEntry{
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
	}, nil
}

func canonicalOpenCodeProjectMCPServerEntry(projection MCPNoEnvServerProjection) (OpenCodeProjectMCPServerEntry, error) {
	return canonicalOpenCodeNoEnvMCPServerEntry(
		projection,
		aggregate.OpenCodeProjectMCPLocalCommandV1,
		"unsupported OpenCode project MCP adapter contract",
	)
}

func canonicalOpenCodeGlobalMCPServerEntry(projection OpenCodeGlobalMCPServerProjection) (OpenCodeGlobalMCPServerEntry, error) {
	if projection.AdapterContract != aggregate.OpenCodeGlobalMCPLocalEnvV1 {
		return OpenCodeGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			"unsupported OpenCode global MCP adapter contract",
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return OpenCodeGlobalMCPServerEntry{}, err
	}
	if err := validateMCPCommand(projection.Command); err != nil {
		return OpenCodeGlobalMCPServerEntry{}, err
	}
	environment, err := canonicalOpenCodeMCPEnvironment(projection.Environment)
	if err != nil {
		return OpenCodeGlobalMCPServerEntry{}, err
	}
	command := make([]string, 0, 1+len(projection.Args))
	command = append(command, projection.Command)
	command = append(command, projection.Args...)
	return OpenCodeGlobalMCPServerEntry{
		Type:        openCodeProjectMCPTypeLocal,
		Command:     command,
		Environment: environment,
	}, nil
}

func canonicalOpenCodeNoEnvMCPServerEntry(
	projection MCPNoEnvServerProjection,
	expectedAdapterContract string,
	staleAdapterMessage string,
) (OpenCodeProjectMCPServerEntry, error) {
	if err := validateNoEnvMCPServerProjection(
		projection,
		expectedAdapterContract,
		staleAdapterMessage,
	); err != nil {
		return OpenCodeProjectMCPServerEntry{}, err
	}
	command := make([]string, 0, 1+len(projection.Args))
	command = append(command, projection.Command)
	command = append(command, projection.Args...)
	return OpenCodeProjectMCPServerEntry{
		Type:    openCodeProjectMCPTypeLocal,
		Command: command,
	}, nil
}

func canonicalCodexProjectMCPServerEntry(projection MCPNoEnvServerProjection) (CodexProjectMCPServerEntry, error) {
	if err := validateNoEnvMCPServerProjection(
		projection,
		aggregate.CodexProjectMCPStdioCommandV1,
		"unsupported Codex project MCP adapter contract",
	); err != nil {
		return CodexProjectMCPServerEntry{}, err
	}
	return CodexProjectMCPServerEntry{
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
	}, nil
}

func canonicalCodexGlobalMCPServerEntry(projection CodexGlobalMCPServerProjection) (CodexGlobalMCPServerEntry, error) {
	if projection.AdapterContract != aggregate.CodexGlobalMCPStdioEnvVarsV1 {
		return CodexGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			"unsupported Codex global MCP adapter contract",
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return CodexGlobalMCPServerEntry{}, err
	}
	if err := validateMCPCommand(projection.Command); err != nil {
		return CodexGlobalMCPServerEntry{}, err
	}
	envVars, err := canonicalSameNameMCPEnvironmentNames(projection.EnvVars, "env_vars")
	if err != nil {
		return CodexGlobalMCPServerEntry{}, err
	}
	return CodexGlobalMCPServerEntry{
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
		EnvVars: envVars,
	}, nil
}

func validateNoEnvMCPServerProjection(
	projection MCPNoEnvServerProjection,
	expectedAdapterContract string,
	staleAdapterMessage string,
) error {
	if projection.AdapterContract != expectedAdapterContract {
		return newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			staleAdapterMessage,
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return err
	}
	return validateMCPCommand(projection.Command)
}

func validateServerID(serverID string) error {
	if strings.TrimSpace(serverID) == "" || strings.TrimSpace(serverID) != serverID || !isStableMCPToken(serverID) {
		return newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			serverID,
			"server id must be a stable token",
		)
	}
	return nil
}

func validateMCPCommand(command string) error {
	var validationErr error
	if filepath.IsAbs(command) {
		_, validationErr = desiredmcp.NewAbsolutePathCommand(command)
	} else {
		_, validationErr = desiredmcp.NewAmbientCommand(command)
	}
	if validationErr != nil {
		return newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			command,
			validationErr.Error(),
		)
	}
	return nil
}

func isStableMCPToken(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' ||
			character == '_' ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}
