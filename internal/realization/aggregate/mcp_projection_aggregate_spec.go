package aggregate

import (
	"fmt"
	"strings"
)

// MCPAggregateRoot identifies the host config aggregate root for one exact projection.
type MCPAggregateRoot string

// MCPContentPathPrefix identifies the managed parent path inside a config aggregate.
type MCPContentPathPrefix string

// MCPMergeUnit identifies the config subtree unit that an exact projection owns.
type MCPMergeUnit string

const (
	MCPMergeUnitServerEntry MCPMergeUnit = "mcp-server-entry"
)

// MCPSiblingRetentionPolicy identifies how unmanaged sibling config data is treated.
type MCPSiblingRetentionPolicy string

const (
	MCPSiblingRetentionPreserveUnmanaged MCPSiblingRetentionPolicy = "preserve_unmanaged_siblings"
)

// MCPConfigAggregateSpecInput carries pure config aggregate semantics for a placement row.
type MCPConfigAggregateSpecInput struct {
	Root              MCPAggregateRoot
	MergeUnit         MCPMergeUnit
	ContentPathPrefix MCPContentPathPrefix
	SiblingRetention  MCPSiblingRetentionPolicy
}

// MCPConfigAggregateSpec is the canonical aggregate ownership row for one exact projection.
type MCPConfigAggregateSpec struct {
	root              MCPAggregateRoot
	mergeUnit         MCPMergeUnit
	contentPathPrefix MCPContentPathPrefix
	siblingRetention  MCPSiblingRetentionPolicy
}

// NewMCPConfigAggregateSpec constructs a validated aggregate ownership spec row.
func NewMCPConfigAggregateSpec(input MCPConfigAggregateSpecInput) (MCPConfigAggregateSpec, error) {
	spec := MCPConfigAggregateSpec{
		root:              MCPAggregateRoot(strings.TrimSpace(string(input.Root))),
		mergeUnit:         input.MergeUnit,
		contentPathPrefix: MCPContentPathPrefix(strings.TrimSpace(string(input.ContentPathPrefix))),
		siblingRetention:  input.SiblingRetention,
	}
	if err := spec.Validate(); err != nil {
		return MCPConfigAggregateSpec{}, err
	}
	return spec, nil
}

// Root returns the host config aggregate root for this spec.
func (spec MCPConfigAggregateSpec) Root() MCPAggregateRoot {
	return spec.root
}

// MergeUnit returns the managed subtree unit for this spec.
func (spec MCPConfigAggregateSpec) MergeUnit() MCPMergeUnit {
	return spec.mergeUnit
}

// ContentPathPrefix returns the managed parent path inside the aggregate.
func (spec MCPConfigAggregateSpec) ContentPathPrefix() MCPContentPathPrefix {
	return spec.contentPathPrefix
}

// SiblingRetention returns the row-local unmanaged sibling policy.
func (spec MCPConfigAggregateSpec) SiblingRetention() MCPSiblingRetentionPolicy {
	return spec.siblingRetention
}

// ContentPath returns the managed entry path for serverID inside this aggregate.
func (spec MCPConfigAggregateSpec) ContentPath(serverID string) (ContentPath, error) {
	if err := validateToken("MCP server id", serverID); err != nil {
		return "", err
	}
	return ParseContentPath(string(spec.contentPathPrefix) + "/" + serverID)
}

// ServerIDFromContentPath returns the stable server id represented by contentPath.
func (spec MCPConfigAggregateSpec) ServerIDFromContentPath(contentPath ContentPath) (string, bool) {
	value := string(contentPath)
	canonical, err := ParseContentPath(value)
	if err != nil || canonical != contentPath {
		return "", false
	}
	prefix := string(spec.contentPathPrefix) + "/"
	if len(value) <= len(prefix) || value[:len(prefix)] != prefix {
		return "", false
	}
	serverID := value[len(prefix):]
	if err := validateToken("MCP server id", serverID); err != nil {
		return "", false
	}
	expected, err := spec.ContentPath(serverID)
	if err != nil {
		return "", false
	}
	if expected != contentPath {
		return "", false
	}
	return serverID, true
}

// Validate rejects malformed aggregate ownership specs.
func (spec MCPConfigAggregateSpec) Validate() error {
	if strings.TrimSpace(string(spec.root)) == "" {
		return fmt.Errorf("MCP aggregate root is required")
	}
	if spec.mergeUnit != MCPMergeUnitServerEntry {
		return fmt.Errorf("MCP aggregate root %q merge unit %q is unsupported", spec.root, spec.mergeUnit)
	}
	if _, err := ParseContentPath(string(spec.contentPathPrefix)); err != nil {
		return fmt.Errorf("MCP aggregate root %q content path prefix must be a canonical absolute parent path without trailing slash", spec.root)
	}
	if spec.siblingRetention != MCPSiblingRetentionPreserveUnmanaged {
		return fmt.Errorf("MCP aggregate root %q sibling retention policy %q is unsupported", spec.root, spec.siblingRetention)
	}
	return nil
}
