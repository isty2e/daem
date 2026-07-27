package mcpcodec

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

const (
	mcpManagedServersField         = "mcpServers"
	codexProjectMCPManagedField    = "mcp_servers"
	openCodeProjectMCPManagedField = "mcp"
	claudeProjectMCPTransportStdio = "stdio"
	openCodeProjectMCPTypeLocal    = "local"
)

// MCPProjectionReasonCode is a stable machine-readable reason for MCP projection adapter failures.
type MCPProjectionReasonCode string

const (
	MCPProjectionReasonNone                           MCPProjectionReasonCode = ""
	MCPProjectionReasonConfigMalformed                MCPProjectionReasonCode = "CONFIG_MALFORMED"
	MCPProjectionReasonUnsupportedTransport           MCPProjectionReasonCode = "UNSUPPORTED_TRANSPORT"
	MCPProjectionReasonUnsupportedManagedField        MCPProjectionReasonCode = "UNSUPPORTED_MANAGED_FIELD"
	MCPProjectionReasonSecretLiteralForbidden         MCPProjectionReasonCode = "SECRET_LITERAL_FORBIDDEN"
	MCPProjectionReasonProjectionEquivalenceUndefined MCPProjectionReasonCode = "PROJECTION_EQUIVALENCE_UNDEFINED"
	MCPProjectionReasonStaleAdapterContract           MCPProjectionReasonCode = "STALE_ADAPTER_CONTRACT"
)

// MCPProjectionRejection reports one host MCP entry that exists but cannot be
// represented by the current standalone projection adapter.
type MCPProjectionRejection struct {
	ContentPath string
	Reason      MCPProjectionReasonCode
}

// MCPProjectionError reports why an MCP projection cannot be canonicalized.
type MCPProjectionError struct {
	reason  MCPProjectionReasonCode
	subject string
	message string
}

func newMCPProjectionError(reason MCPProjectionReasonCode, subject string, message string) *MCPProjectionError {
	return &MCPProjectionError{reason: reason, subject: subject, message: message}
}

func (err *MCPProjectionError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.subject == "" {
		return fmt.Sprintf("%s: %s", err.reason, err.message)
	}
	return fmt.Sprintf("%s: %s: %s", err.reason, err.subject, err.message)
}

// Code returns the stable projection adapter reason code.
func (err *MCPProjectionError) Code() MCPProjectionReasonCode {
	if err == nil {
		return MCPProjectionReasonNone
	}
	return err.reason
}

// Subject returns the invalid projection subject when one is available.
func (err *MCPProjectionError) Subject() string {
	if err == nil {
		return ""
	}
	return err.subject
}

// MCPProjectionReasonCodeOf extracts a stable projection reason from err.
func MCPProjectionReasonCodeOf(err error) (MCPProjectionReasonCode, bool) {
	var projectionError *MCPProjectionError
	if errors.As(err, &projectionError) {
		return projectionError.Code(), true
	}
	return MCPProjectionReasonNone, false
}

// ClaudeProjectMCPServerProjection is the surface-owned canonical host projection input.
type ClaudeProjectMCPServerProjection struct {
	ServerID        string
	Command         string
	Args            []string
	Env             map[string]string
	AdapterContract string
}

// ClaudeProjectMCPServerEntry is the canonical managed server entry inside .mcp.json.
type ClaudeProjectMCPServerEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// MCPNoEnvServerProjection is the normalized input shared by MCP host surfaces
// whose wire entry cannot carry environment bindings.
type MCPNoEnvServerProjection struct {
	ServerID        string
	Command         string
	Args            []string
	AdapterContract string
}

// ClaudeGlobalMCPServerEntry is the canonical managed server entry inside ~/.claude.json.
type ClaudeGlobalMCPServerEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// AntigravityGlobalMCPServerEntry is the canonical managed server entry inside ~/.gemini/config/mcp_config.json.
type AntigravityGlobalMCPServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// OpenCodeMCPServerEntry is the canonical managed server entry inside OpenCode config JSON.
type OpenCodeMCPServerEntry struct {
	Type    string   `json:"type"`
	Command []string `json:"command"`
}

// CodexMCPServerEntry is the canonical managed server entry inside Codex config TOML.
type CodexMCPServerEntry struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}

// ClaudeProjectMCPContentPath returns the managed JSON pointer for one server id.
func ClaudeProjectMCPContentPath(serverID string) string {
	return mcpProjectionSubject(aggregate.MCPPlacementClaudeProject, serverID)
}

// ClaudeGlobalMCPContentPath returns the managed JSON pointer for one server id.
func ClaudeGlobalMCPContentPath(serverID string) string {
	return mcpProjectionSubject(aggregate.MCPPlacementClaudeGlobal, serverID)
}

// AntigravityGlobalMCPContentPath returns the managed JSON pointer for one server id.
func AntigravityGlobalMCPContentPath(serverID string) string {
	return mcpProjectionSubject(aggregate.MCPPlacementAntigravityGlobal, serverID)
}

// OpenCodeProjectMCPContentPath returns the managed JSON pointer for one server id.
func OpenCodeProjectMCPContentPath(serverID string) string {
	return mcpProjectionSubject(aggregate.MCPPlacementOpenCodeProject, serverID)
}

// OpenCodeGlobalMCPContentPath returns the managed JSON pointer for one server id.
func OpenCodeGlobalMCPContentPath(serverID string) string {
	return mcpProjectionSubject(aggregate.MCPPlacementOpenCodeGlobal, serverID)
}

// CodexProjectMCPContentPath returns the managed TOML path for one server id.
func CodexProjectMCPContentPath(serverID string) string {
	return mcpProjectionSubject(aggregate.MCPPlacementCodexProject, serverID)
}

// CodexGlobalMCPContentPath returns the managed TOML path for one server id.
func CodexGlobalMCPContentPath(serverID string) string {
	return mcpProjectionSubject(aggregate.MCPPlacementCodexGlobal, serverID)
}

func mcpProjectionSubject(placementID aggregate.MCPPlacementID, serverID string) string {
	placement, ok := aggregate.MCPPlacementForID(placementID)
	if !ok {
		panic(fmt.Sprintf("MCP placement %q is not registered", placementID))
	}
	contentPath, err := placement.ContentPath(serverID)
	if err == nil {
		return string(contentPath)
	}
	return string(placement.ContentPathPrefix()) + "/" + serverID
}
