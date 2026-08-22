package mcpcodec

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

const (
	mcpManagedServersField           = "mcpServers"
	codexProjectMCPManagedField      = "mcp_servers"
	openCodeProjectMCPManagedField   = "mcp"
	claudeProjectMCPTransportStdio   = "stdio"
	openCodeProjectMCPTypeLocal      = "local"
	maximumMCPRejectionServerIDBytes = 4096
)

// MCPProjectionReasonCode is a stable machine-readable reason for MCP projection adapter failures.
type MCPProjectionReasonCode string

const (
	MCPProjectionReasonNone                           MCPProjectionReasonCode = ""
	MCPProjectionReasonConfigMalformed                MCPProjectionReasonCode = "CONFIG_MALFORMED"
	MCPProjectionReasonDuplicateKey                   MCPProjectionReasonCode = "DUPLICATE_KEY"
	MCPProjectionReasonUnsupportedTransport           MCPProjectionReasonCode = "UNSUPPORTED_TRANSPORT"
	MCPProjectionReasonUnsupportedManagedField        MCPProjectionReasonCode = "UNSUPPORTED_MANAGED_FIELD"
	MCPProjectionReasonSecretLiteralForbidden         MCPProjectionReasonCode = "SECRET_LITERAL_FORBIDDEN"
	MCPProjectionReasonProjectionEquivalenceUndefined MCPProjectionReasonCode = "PROJECTION_EQUIVALENCE_UNDEFINED"
	MCPProjectionReasonCanonicalInvalid               MCPProjectionReasonCode = "CANONICAL_INVALID"
	MCPProjectionReasonStaleAdapterContract           MCPProjectionReasonCode = "STALE_ADAPTER_CONTRACT"
	MCPProjectionReasonProviderDocumentLossy          MCPProjectionReasonCode = "PROVIDER_DOCUMENT_LOSSY"
)

// MCPProjectionRejection reports one host MCP entry that exists but cannot be
// represented by the current standalone projection adapter.
type MCPProjectionRejection struct {
	contentPath aggregate.ContentPath
	reason      MCPProjectionReasonCode
}

// ContentPath returns the greatest canonical location established for the
// rejected row without retaining an unsafe host identifier.
func (rejection MCPProjectionRejection) ContentPath() aggregate.ContentPath {
	return rejection.contentPath
}

// Reason returns the stable row-rejection classification.
func (rejection MCPProjectionRejection) Reason() MCPProjectionReasonCode {
	return rejection.reason
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

// ClaudeGlobalMCPServerProjection is the normalized Claude Code user/global
// stdio projection. Env contains exact child-to-host-source references.
type ClaudeGlobalMCPServerProjection struct {
	ServerID        string
	Command         string
	Args            []string
	Env             map[string]string
	AdapterContract string
}

// AntigravityGlobalMCPServerProjection is the normalized Antigravity CLI
// default-global stdio projection. EnvironmentNames are same-name ambient
// runtime prerequisites and are intentionally absent from native config bytes.
type AntigravityGlobalMCPServerProjection struct {
	ServerID         string
	Command          string
	Args             []string
	EnvironmentNames []string
	AdapterContract  string
}

// OpenCodeGlobalMCPServerProjection is the normalized OpenCode default-global
// local-server projection. Environment contains exact child-to-host-source references.
type OpenCodeGlobalMCPServerProjection struct {
	ServerID        string
	Command         string
	Args            []string
	Environment     map[string]string
	AdapterContract string
}

// MCPNoEnvServerProjection is the normalized input shared by MCP host surfaces
// whose wire entry cannot carry environment bindings.
type MCPNoEnvServerProjection struct {
	ServerID        string
	Command         string
	Args            []string
	AdapterContract string
}

// CodexGlobalMCPServerProjection is the normalized Codex global stdio
// projection. EnvVars contains same-name host environment references only.
type CodexGlobalMCPServerProjection struct {
	ServerID        string
	Command         string
	Args            []string
	EnvVars         []string
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

// OpenCodeProjectMCPServerEntry is the canonical managed project server entry inside opencode.json.
type OpenCodeProjectMCPServerEntry struct {
	Type    string   `json:"type"`
	Command []string `json:"command"`
}

// OpenCodeGlobalMCPServerEntry is the canonical managed default-global server entry.
type OpenCodeGlobalMCPServerEntry struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment,omitempty"`
}

// CodexProjectMCPServerEntry is the canonical managed project server entry.
type CodexProjectMCPServerEntry struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}

// CodexGlobalMCPServerEntry is the canonical managed global server entry.
type CodexGlobalMCPServerEntry struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
	EnvVars []string `toml:"env_vars,omitempty"`
}

// PiMCPAdapterServerProjection is the normalized project/global projection for
// the admitted pi-mcp-adapter stdio profile.
type PiMCPAdapterServerProjection struct {
	ServerID        string
	Command         string
	Args            []string
	Env             map[string]string
	AdapterContract string
}

// PiMCPAdapterServerEntry is the canonical managed entry in a
// pi-mcp-adapter-owned mcp.json document.
type PiMCPAdapterServerEntry struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env,omitempty"`
	Lifecycle string            `json:"lifecycle"`
	Disabled  bool              `json:"disabled"`
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

// PiProjectMCPContentPath returns the project adapter JSON pointer for one server id.
func PiProjectMCPContentPath(serverID string) string {
	return mcpProjectionSubject(aggregate.MCPPlacementPiProject, serverID)
}

// PiGlobalMCPContentPath returns the global adapter JSON pointer for one server id.
func PiGlobalMCPContentPath(serverID string) string {
	return mcpProjectionSubject(aggregate.MCPPlacementPiGlobal, serverID)
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
	return string(placement.ContentPathPrefix())
}

func mcpProjectionRejection(
	placementID aggregate.MCPPlacementID,
	serverID string,
	err error,
) MCPProjectionRejection {
	reason, ok := MCPProjectionReasonCodeOf(err)
	if !ok {
		reason = MCPProjectionReasonProjectionEquivalenceUndefined
	}
	return MCPProjectionRejection{
		contentPath: mcpRejectionContentPath(placementID, serverID),
		reason:      reason,
	}
}

func mcpRejectionContentPath(
	placementID aggregate.MCPPlacementID,
	serverID string,
) aggregate.ContentPath {
	placement, ok := aggregate.MCPPlacementForID(placementID)
	if !ok {
		panic(fmt.Sprintf("MCP placement %q is not registered", placementID))
	}
	if len(serverID) > maximumMCPRejectionServerIDBytes {
		return aggregate.ContentPath(placement.ContentPathPrefix())
	}
	contentPath, err := placement.ContentPath(serverID)
	if err == nil {
		return contentPath
	}
	return aggregate.ContentPath(placement.ContentPathPrefix())
}
