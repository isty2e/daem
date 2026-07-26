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
	command := stdio.Command().Name()
	args := stdio.Args()
	adapterContract := string(placement.CodecContractID())
	switch placement.ID() {
	case aggregate.MCPPlacementClaudeProject:
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
		return CanonicalClaudeGlobalMCPServerEntry(ClaudeGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			AdapterContract: adapterContract,
		})
	case aggregate.MCPPlacementAntigravityGlobal:
		return CanonicalAntigravityGlobalMCPServerEntry(AntigravityGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			AdapterContract: adapterContract,
		})
	case aggregate.MCPPlacementOpenCodeProject:
		return CanonicalOpenCodeProjectMCPServerEntry(OpenCodeProjectMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			AdapterContract: adapterContract,
		})
	case aggregate.MCPPlacementOpenCodeGlobal:
		return CanonicalOpenCodeGlobalMCPServerEntry(OpenCodeGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			AdapterContract: adapterContract,
		})
	case aggregate.MCPPlacementCodexProject:
		return CanonicalCodexProjectMCPServerEntry(CodexProjectMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			AdapterContract: adapterContract,
		})
	case aggregate.MCPPlacementCodexGlobal:
		return CanonicalCodexGlobalMCPServerEntry(CodexGlobalMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			AdapterContract: adapterContract,
		})
	default:
		return nil, fmt.Errorf("unsupported MCP placement %q", placement.ID())
	}
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
func CanonicalOpenCodeProjectMCPServerEntry(projection OpenCodeProjectMCPServerProjection) ([]byte, error) {
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
func CanonicalCodexProjectMCPServerEntry(projection CodexProjectMCPServerProjection) ([]byte, error) {
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
	if err := validatePortableMCPCommand(projection.Command); err != nil {
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
	if projection.AdapterContract != aggregate.ClaudeGlobalMCPStdioAdapterV1 {
		return ClaudeGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			"unsupported Claude Code user/global MCP adapter contract",
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return ClaudeGlobalMCPServerEntry{}, err
	}
	if err := validatePortableMCPCommand(projection.Command); err != nil {
		return ClaudeGlobalMCPServerEntry{}, err
	}
	return ClaudeGlobalMCPServerEntry{
		Type:    claudeProjectMCPTransportStdio,
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
		Env:     map[string]string{},
	}, nil
}

func canonicalAntigravityGlobalMCPServerEntry(projection AntigravityGlobalMCPServerProjection) (AntigravityGlobalMCPServerEntry, error) {
	if projection.AdapterContract != aggregate.AntigravityGlobalMCPCommandAdapterV1 {
		return AntigravityGlobalMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			"unsupported Antigravity CLI global MCP adapter contract",
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return AntigravityGlobalMCPServerEntry{}, err
	}
	if err := validatePortableMCPCommand(projection.Command); err != nil {
		return AntigravityGlobalMCPServerEntry{}, err
	}
	return AntigravityGlobalMCPServerEntry{
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
	}, nil
}

func canonicalOpenCodeProjectMCPServerEntry(projection OpenCodeProjectMCPServerProjection) (OpenCodeMCPServerEntry, error) {
	return canonicalOpenCodeMCPServerEntry(
		projection.ServerID,
		projection.Command,
		projection.Args,
		projection.AdapterContract,
		aggregate.OpenCodeProjectMCPLocalCommandV1,
		"unsupported OpenCode project MCP adapter contract",
	)
}

func canonicalOpenCodeGlobalMCPServerEntry(projection OpenCodeGlobalMCPServerProjection) (OpenCodeMCPServerEntry, error) {
	return canonicalOpenCodeMCPServerEntry(
		projection.ServerID,
		projection.Command,
		projection.Args,
		projection.AdapterContract,
		aggregate.OpenCodeGlobalMCPLocalCommandV1,
		"unsupported OpenCode global MCP adapter contract",
	)
}

func canonicalOpenCodeMCPServerEntry(
	serverID string,
	commandName string,
	args []string,
	adapterContract string,
	expectedAdapterContract string,
	staleAdapterMessage string,
) (OpenCodeMCPServerEntry, error) {
	if adapterContract != expectedAdapterContract {
		return OpenCodeMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			adapterContract,
			staleAdapterMessage,
		)
	}
	if err := validateServerID(serverID); err != nil {
		return OpenCodeMCPServerEntry{}, err
	}
	if err := validatePortableMCPCommand(commandName); err != nil {
		return OpenCodeMCPServerEntry{}, err
	}
	command := make([]string, 0, 1+len(args))
	command = append(command, commandName)
	command = append(command, args...)
	return OpenCodeMCPServerEntry{
		Type:    openCodeProjectMCPTypeLocal,
		Command: command,
	}, nil
}

func canonicalCodexProjectMCPServerEntry(projection CodexProjectMCPServerProjection) (CodexMCPServerEntry, error) {
	if projection.AdapterContract != aggregate.CodexProjectMCPStdioCommandV1 {
		return CodexMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			"unsupported Codex project MCP adapter contract",
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return CodexMCPServerEntry{}, err
	}
	if err := validatePortableMCPCommand(projection.Command); err != nil {
		return CodexMCPServerEntry{}, err
	}
	return CodexMCPServerEntry{
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
	}, nil
}

func canonicalCodexGlobalMCPServerEntry(projection CodexGlobalMCPServerProjection) (CodexMCPServerEntry, error) {
	if projection.AdapterContract != aggregate.CodexGlobalMCPStdioCommandV1 {
		return CodexMCPServerEntry{}, newMCPProjectionError(
			MCPProjectionReasonStaleAdapterContract,
			projection.AdapterContract,
			"unsupported Codex global MCP adapter contract",
		)
	}
	if err := validateServerID(projection.ServerID); err != nil {
		return CodexMCPServerEntry{}, err
	}
	if err := validatePortableMCPCommand(projection.Command); err != nil {
		return CodexMCPServerEntry{}, err
	}
	return CodexMCPServerEntry{
		Command: projection.Command,
		Args:    append([]string{}, projection.Args...),
	}, nil
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

func validatePortableMCPCommand(command string) error {
	if strings.TrimSpace(command) == "" || strings.TrimSpace(command) != command {
		return newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			command,
			"command is required and must not contain surrounding whitespace",
		)
	}
	if filepath.IsAbs(command) || strings.ContainsAny(command, "/\\ \t\n\r;&|$`") || !isStableMCPToken(command) {
		return newMCPProjectionError(
			MCPProjectionReasonProjectionEquivalenceUndefined,
			command,
			"command must be a portable command token",
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
