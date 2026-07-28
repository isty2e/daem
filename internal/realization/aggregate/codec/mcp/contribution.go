package mcpcodec

import (
	"fmt"
	"path/filepath"
	"sort"
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
		if err := requireMCPNoEnvReferenceCodecContract(placement); err != nil {
			return nil, err
		}
		return CanonicalClaudeGlobalMCPServerEntry(noEnvProjection)
	case aggregate.MCPPlacementAntigravityGlobal:
		if err := requireMCPNoEnvReferenceCodecContract(placement); err != nil {
			return nil, err
		}
		return CanonicalAntigravityGlobalMCPServerEntry(noEnvProjection)
	case aggregate.MCPPlacementOpenCodeProject:
		if err := requireMCPNoEnvReferenceCodecContract(placement); err != nil {
			return nil, err
		}
		return CanonicalOpenCodeProjectMCPServerEntry(noEnvProjection)
	case aggregate.MCPPlacementOpenCodeGlobal:
		if err := requireMCPNoEnvReferenceCodecContract(placement); err != nil {
			return nil, err
		}
		return CanonicalOpenCodeGlobalMCPServerEntry(noEnvProjection)
	case aggregate.MCPPlacementCodexProject:
		if err := requireMCPNoEnvReferenceCodecContract(placement); err != nil {
			return nil, err
		}
		return CanonicalCodexProjectMCPServerEntry(noEnvProjection)
	case aggregate.MCPPlacementCodexGlobal:
		if err := requireMCPNoEnvReferenceCodecContract(placement); err != nil {
			return nil, err
		}
		return CanonicalCodexGlobalMCPServerEntry(noEnvProjection)
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

func canonicalMCPBindingEnv(values map[string]desiredmcp.EnvReference) (map[string]string, error) {
	env := make(map[string]string, len(values))
	for name, reference := range values {
		fromEnv := strings.TrimSpace(reference.FromEnv())
		if fromEnv == "" {
			return nil, fmt.Errorf("env.%s.from_env: required", name)
		}
		env[name] = "${" + fromEnv + "}"
	}
	return env, nil
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
func CanonicalClaudeGlobalMCPServerEntry(projection MCPNoEnvServerProjection) ([]byte, error) {
	entry, err := canonicalClaudeGlobalMCPServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return canonicalJSON(entry)
}

// CanonicalAntigravityGlobalMCPServerEntry returns the canonical managed server entry bytes.
func CanonicalAntigravityGlobalMCPServerEntry(projection MCPNoEnvServerProjection) ([]byte, error) {
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
func CanonicalOpenCodeGlobalMCPServerEntry(projection MCPNoEnvServerProjection) ([]byte, error) {
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
func CanonicalCodexGlobalMCPServerEntry(projection MCPNoEnvServerProjection) ([]byte, error) {
	entry, err := canonicalCodexGlobalMCPServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return encodeCodexProjectMCPServerEntry(entry)
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

func canonicalClaudeGlobalMCPServerEntry(projection MCPNoEnvServerProjection) (ClaudeGlobalMCPServerEntry, error) {
	if err := validateNoEnvMCPServerProjection(
		projection,
		aggregate.ClaudeGlobalMCPStdioAdapterV1,
		"unsupported Claude Code user/global MCP adapter contract",
	); err != nil {
		return ClaudeGlobalMCPServerEntry{}, err
	}
	return ClaudeGlobalMCPServerEntry{
		Type:    claudeProjectMCPTransportStdio,
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
		Env:     map[string]string{},
	}, nil
}

func canonicalAntigravityGlobalMCPServerEntry(projection MCPNoEnvServerProjection) (AntigravityGlobalMCPServerEntry, error) {
	if err := validateNoEnvMCPServerProjection(
		projection,
		aggregate.AntigravityGlobalMCPCommandAdapterV1,
		"unsupported Antigravity CLI global MCP adapter contract",
	); err != nil {
		return AntigravityGlobalMCPServerEntry{}, err
	}
	return AntigravityGlobalMCPServerEntry{
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
	}, nil
}

func canonicalOpenCodeProjectMCPServerEntry(projection MCPNoEnvServerProjection) (OpenCodeMCPServerEntry, error) {
	return canonicalOpenCodeMCPServerEntry(
		projection,
		aggregate.OpenCodeProjectMCPLocalCommandV1,
		"unsupported OpenCode project MCP adapter contract",
	)
}

func canonicalOpenCodeGlobalMCPServerEntry(projection MCPNoEnvServerProjection) (OpenCodeMCPServerEntry, error) {
	return canonicalOpenCodeMCPServerEntry(
		projection,
		aggregate.OpenCodeGlobalMCPLocalCommandV1,
		"unsupported OpenCode global MCP adapter contract",
	)
}

func canonicalOpenCodeMCPServerEntry(
	projection MCPNoEnvServerProjection,
	expectedAdapterContract string,
	staleAdapterMessage string,
) (OpenCodeMCPServerEntry, error) {
	if err := validateNoEnvMCPServerProjection(
		projection,
		expectedAdapterContract,
		staleAdapterMessage,
	); err != nil {
		return OpenCodeMCPServerEntry{}, err
	}
	command := make([]string, 0, 1+len(projection.Args))
	command = append(command, projection.Command)
	command = append(command, projection.Args...)
	return OpenCodeMCPServerEntry{
		Type:    openCodeProjectMCPTypeLocal,
		Command: command,
	}, nil
}

func canonicalCodexProjectMCPServerEntry(projection MCPNoEnvServerProjection) (CodexMCPServerEntry, error) {
	if err := validateNoEnvMCPServerProjection(
		projection,
		aggregate.CodexProjectMCPStdioCommandV1,
		"unsupported Codex project MCP adapter contract",
	); err != nil {
		return CodexMCPServerEntry{}, err
	}
	return CodexMCPServerEntry{
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
	}, nil
}

func canonicalCodexGlobalMCPServerEntry(projection MCPNoEnvServerProjection) (CodexMCPServerEntry, error) {
	if err := validateNoEnvMCPServerProjection(
		projection,
		aggregate.CodexGlobalMCPStdioCommandV1,
		"unsupported Codex global MCP adapter contract",
	); err != nil {
		return CodexMCPServerEntry{}, err
	}
	return CodexMCPServerEntry{
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
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

func canonicalMCPEnv(values map[string]string) (map[string]string, error) {
	env := make(map[string]string, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := validateEnvName(key, "env."+key); err != nil {
			return nil, err
		}
		value := values[key]
		if err := validateHostEnvReference(value, "env."+key); err != nil {
			return nil, err
		}
		env[key] = value
	}
	return env, nil
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

func validateEnvName(value string, subject string) error {
	if value == "" || value[0] >= '0' && value[0] <= '9' {
		return newMCPProjectionError(MCPProjectionReasonProjectionEquivalenceUndefined, subject, "env name is invalid")
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '_' {
			continue
		}
		return newMCPProjectionError(MCPProjectionReasonProjectionEquivalenceUndefined, subject, "env name is invalid")
	}
	return nil
}

func validateHostEnvReference(value string, subject string) error {
	if !strings.HasPrefix(value, "${") || !strings.HasSuffix(value, "}") {
		return newMCPProjectionError(MCPProjectionReasonSecretLiteralForbidden, subject, "env value must be a host env reference")
	}
	name := strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
	if err := validateEnvName(name, subject); err != nil {
		return newMCPProjectionError(MCPProjectionReasonSecretLiteralForbidden, subject, "env value must reference a valid host env name")
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
