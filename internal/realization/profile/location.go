package profile

import (
	"fmt"
	"path"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

// ImportPolicy is the discovery owner's closed import classification.
type ImportPolicy string

const (
	ImportPolicyInclude  ImportPolicy = "include"
	ImportPolicyClassify ImportPolicy = "classify"
)

// DiscoveryLocation is one host-visible import/discovery location.
type DiscoveryLocation struct {
	selectedTarget target.Target
	resourceKind   entity.Kind
	scope          target.Scope
	path           string
	priority       int
	importPolicy   ImportPolicy
}

// NewDiscoveryLocation validates one discovery fact.
func NewDiscoveryLocation(
	selectedTarget target.Target,
	resourceKind entity.Kind,
	scope target.Scope,
	locationPath string,
	priority int,
	importPolicy ImportPolicy,
) (DiscoveryLocation, error) {
	location := DiscoveryLocation{
		selectedTarget: selectedTarget,
		resourceKind:   resourceKind,
		scope:          scope,
		path:           locationPath,
		priority:       priority,
		importPolicy:   importPolicy,
	}
	if err := location.Validate(); err != nil {
		return DiscoveryLocation{}, err
	}
	return location, nil
}

func (location DiscoveryLocation) Validate() error {
	if err := validateLocationIdentity(location.selectedTarget, location.resourceKind, location.scope, location.path); err != nil {
		return fmt.Errorf("discovery location: %w", err)
	}
	switch location.importPolicy {
	case ImportPolicyInclude, ImportPolicyClassify:
		return nil
	default:
		return fmt.Errorf("discovery location import policy %q is unsupported", location.importPolicy)
	}
}

func (location DiscoveryLocation) Target() target.Target      { return location.selectedTarget }
func (location DiscoveryLocation) ResourceKind() entity.Kind  { return location.resourceKind }
func (location DiscoveryLocation) Scope() target.Scope        { return location.scope }
func (location DiscoveryLocation) Path() string               { return location.path }
func (location DiscoveryLocation) Priority() int              { return location.priority }
func (location DiscoveryLocation) ImportPolicy() ImportPolicy { return location.importPolicy }

// RuntimeLocation is one host runtime lookup location. It carries no import policy.
type RuntimeLocation struct {
	selectedTarget target.Target
	resourceKind   entity.Kind
	scope          target.Scope
	path           string
}

func NewRuntimeLocation(
	selectedTarget target.Target,
	resourceKind entity.Kind,
	scope target.Scope,
	locationPath string,
) (RuntimeLocation, error) {
	location := RuntimeLocation{selectedTarget: selectedTarget, resourceKind: resourceKind, scope: scope, path: locationPath}
	if err := location.Validate(); err != nil {
		return RuntimeLocation{}, err
	}
	return location, nil
}

func (location RuntimeLocation) Validate() error {
	if err := validateLocationIdentity(location.selectedTarget, location.resourceKind, location.scope, location.path); err != nil {
		return fmt.Errorf("runtime location: %w", err)
	}
	return nil
}

func (location RuntimeLocation) Target() target.Target     { return location.selectedTarget }
func (location RuntimeLocation) ResourceKind() entity.Kind { return location.resourceKind }
func (location RuntimeLocation) Scope() target.Scope       { return location.scope }
func (location RuntimeLocation) Path() string              { return location.path }

func validateLocationIdentity(
	selectedTarget target.Target,
	resourceKind entity.Kind,
	scope target.Scope,
	locationPath string,
) error {
	if _, err := target.ParseTarget(string(selectedTarget)); err != nil {
		return err
	}
	if _, err := entity.ParseKind(string(resourceKind)); err != nil {
		return err
	}
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return err
	}
	if strings.TrimSpace(locationPath) == "" || strings.TrimSpace(locationPath) != locationPath || strings.Contains(locationPath, "\\") {
		return fmt.Errorf("path %q must be a non-empty canonical slash path", locationPath)
	}
	if path.Clean(locationPath) != locationPath {
		return fmt.Errorf("path %q is not canonical", locationPath)
	}
	if parsedScope == target.ScopeProject && (strings.HasPrefix(locationPath, "~/") || strings.HasPrefix(locationPath, "/")) {
		return fmt.Errorf("project path %q must be relative", locationPath)
	}
	return nil
}
