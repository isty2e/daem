package listworkflow

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

// LocationKind is the closed set of path-inventory row variants.
type LocationKind string

const (
	LocationPath        LocationKind = "path"
	LocationRoute       LocationKind = "route"
	LocationUnsupported LocationKind = "unsupported"
)

// LocationRole distinguishes what authority or lookup role one row carries.
type LocationRole string

const (
	LocationRoleWrite       LocationRole = "write"
	LocationRoleDiscovery   LocationRole = "discovery"
	LocationRoleRuntime     LocationRole = "runtime"
	LocationRoleConfig      LocationRole = "config"
	LocationRoleInternal    LocationRole = "internal"
	LocationRoleDelegated   LocationRole = "delegated"
	LocationRoleUnsupported LocationRole = "unsupported"
)

// LocationRealization is the user-facing realization classification.
type LocationRealization string

const (
	LocationManagedPath        LocationRealization = "managed-path"
	LocationConfigContribution LocationRealization = "config-contribution"
	LocationHostDiscovery      LocationRealization = "host-discovery"
	LocationHostRuntime        LocationRealization = "host-runtime"
	LocationInternalStore      LocationRealization = "internal-store"
	LocationDelegatedRoute     LocationRealization = "delegated-route"
	LocationUnavailable        LocationRealization = "unsupported"
)

// LocationSelectionSource explains why a write row is or is not selected.
type LocationSelectionSource string

const (
	LocationSelectionManifestDefault  LocationSelectionSource = "manifest-default"
	LocationSelectionManifestExplicit LocationSelectionSource = "manifest-explicit"
	LocationSelectionProfileDefault   LocationSelectionSource = "profile-default"
	LocationSelectionAlternative      LocationSelectionSource = "alternative"
	LocationSelectionNotApplicable    LocationSelectionSource = "not-applicable"
)

// LocationSource identifies the canonical catalog that owns one row.
type LocationSource string

const (
	LocationSourceProfile          LocationSource = "profile"
	LocationSourceAggregate        LocationSource = "aggregate"
	LocationSourceDelegatedProfile LocationSource = "delegated-profile"
	LocationSourceManifest         LocationSource = "manifest"
)

type locationEntryInput struct {
	kind            LocationKind
	selectedTarget  target.Target
	scope           target.Scope
	resourceKind    entity.Kind
	variant         string
	realization     LocationRealization
	role            LocationRole
	path            string
	route           string
	operation       profile.Operation
	selected        bool
	requested       bool
	defaultChoice   bool
	selectionSource LocationSelectionSource
	source          LocationSource
	reason          string
	detail          string
}

// LocationEntry is one validated flat path, route, or unsupported inventory row.
type LocationEntry struct {
	kind            LocationKind
	selectedTarget  target.Target
	scope           target.Scope
	resourceKind    entity.Kind
	variant         string
	realization     LocationRealization
	role            LocationRole
	path            string
	route           string
	operation       profile.Operation
	selected        bool
	requested       bool
	defaultChoice   bool
	selectionSource LocationSelectionSource
	source          LocationSource
	reason          string
	detail          string
}

func newLocationEntry(input locationEntryInput) (LocationEntry, error) {
	entry := LocationEntry{
		kind: input.kind, selectedTarget: input.selectedTarget, scope: input.scope,
		resourceKind: input.resourceKind, variant: input.variant, realization: input.realization,
		role: input.role, path: input.path, route: input.route, operation: input.operation,
		selected: input.selected, requested: input.requested, defaultChoice: input.defaultChoice,
		selectionSource: input.selectionSource, source: input.source,
		reason: input.reason, detail: input.detail,
	}
	if err := entry.validate(); err != nil {
		return LocationEntry{}, err
	}
	return entry, nil
}

func (entry LocationEntry) validate() error {
	if _, err := target.ParseTarget(string(entry.selectedTarget)); err != nil {
		return err
	}
	if _, err := target.ParseScope(string(entry.scope)); err != nil {
		return err
	}
	if _, err := entity.ParseKind(string(entry.resourceKind)); err != nil {
		return err
	}
	for _, value := range []struct {
		label string
		value string
	}{
		{label: "variant", value: entry.variant},
		{label: "path", value: entry.path},
		{label: "route", value: entry.route},
		{label: "reason", value: entry.reason},
		{label: "detail", value: entry.detail},
	} {
		if value.value != strings.TrimSpace(value.value) {
			return fmt.Errorf("location %s must be trimmed", value.label)
		}
	}
	switch entry.source {
	case LocationSourceProfile, LocationSourceAggregate, LocationSourceDelegatedProfile, LocationSourceManifest:
	default:
		return fmt.Errorf("location source %q is unsupported", entry.source)
	}
	switch entry.selectionSource {
	case LocationSelectionManifestDefault,
		LocationSelectionManifestExplicit,
		LocationSelectionProfileDefault,
		LocationSelectionAlternative,
		LocationSelectionNotApplicable:
	default:
		return fmt.Errorf("location selection source %q is unsupported", entry.selectionSource)
	}

	switch entry.kind {
	case LocationPath:
		if entry.path == "" || entry.route != "" || entry.reason != "" {
			return fmt.Errorf("path location requires only a path value")
		}
		if entry.operation != "" {
			return fmt.Errorf("path location must not carry an operation")
		}
	case LocationRoute:
		if entry.route == "" || entry.path != "" || entry.reason != "" {
			return fmt.Errorf("route location requires only a route value")
		}
		switch entry.operation {
		case profile.OperationInstall, profile.OperationRefresh, profile.OperationRemove:
		default:
			return fmt.Errorf("route location operation %q is unsupported", entry.operation)
		}
	case LocationUnsupported:
		if entry.reason == "" || entry.path != "" || entry.route != "" || entry.operation != "" {
			return fmt.Errorf("unsupported location requires only a reason")
		}
		if entry.selected || entry.defaultChoice {
			return fmt.Errorf("unsupported location cannot be selected or default")
		}
	default:
		return fmt.Errorf("location kind %q is unsupported", entry.kind)
	}

	switch entry.role {
	case LocationRoleWrite:
		if entry.kind != LocationPath || entry.realization != LocationManagedPath {
			return fmt.Errorf("write location requires a managed path")
		}
		if entry.selectionSource == LocationSelectionNotApplicable {
			return fmt.Errorf("write location requires a selection source")
		}
	case LocationRoleDiscovery:
		if entry.kind != LocationPath || entry.realization != LocationHostDiscovery {
			return fmt.Errorf("discovery location requires a host-discovery path")
		}
	case LocationRoleRuntime:
		if entry.kind != LocationPath || entry.realization != LocationHostRuntime {
			return fmt.Errorf("runtime location requires a host-runtime path")
		}
	case LocationRoleConfig:
		if entry.kind != LocationPath || entry.realization != LocationConfigContribution {
			return fmt.Errorf("config location requires a config-contribution path")
		}
	case LocationRoleInternal:
		if entry.kind != LocationPath || entry.realization != LocationInternalStore {
			return fmt.Errorf("internal location requires an internal-store path")
		}
	case LocationRoleDelegated:
		if entry.kind != LocationRoute || entry.realization != LocationDelegatedRoute {
			return fmt.Errorf("delegated location requires a delegated route")
		}
	case LocationRoleUnsupported:
		if entry.kind != LocationUnsupported || entry.realization != LocationUnavailable {
			return fmt.Errorf("unsupported role requires an unsupported location")
		}
	default:
		return fmt.Errorf("location role %q is unsupported", entry.role)
	}
	if entry.role != LocationRoleWrite {
		if entry.defaultChoice || entry.selectionSource != LocationSelectionNotApplicable {
			return fmt.Errorf("only write locations carry default and selection-source facts")
		}
	}
	if entry.selected && !entry.requested {
		return fmt.Errorf("selected location requires a manifest request")
	}
	return nil
}

func (entry LocationEntry) Kind() LocationKind                       { return entry.kind }
func (entry LocationEntry) Target() target.Target                    { return entry.selectedTarget }
func (entry LocationEntry) Scope() target.Scope                      { return entry.scope }
func (entry LocationEntry) ResourceKind() entity.Kind                { return entry.resourceKind }
func (entry LocationEntry) Variant() string                          { return entry.variant }
func (entry LocationEntry) Realization() LocationRealization         { return entry.realization }
func (entry LocationEntry) Role() LocationRole                       { return entry.role }
func (entry LocationEntry) Path() string                             { return entry.path }
func (entry LocationEntry) Route() string                            { return entry.route }
func (entry LocationEntry) Operation() profile.Operation             { return entry.operation }
func (entry LocationEntry) Selected() bool                           { return entry.selected }
func (entry LocationEntry) Requested() bool                          { return entry.requested }
func (entry LocationEntry) Default() bool                            { return entry.defaultChoice }
func (entry LocationEntry) SelectionSource() LocationSelectionSource { return entry.selectionSource }
func (entry LocationEntry) Source() LocationSource                   { return entry.source }
func (entry LocationEntry) Reason() string                           { return entry.reason }
func (entry LocationEntry) Detail() string                           { return entry.detail }
