package clipresent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
	listworkflow "github.com/isty2e/daem/internal/workflow/list"
)

const listPathsJSONSchemaVersion = 1

type ListPathRow struct {
	Target          string `json:"target"`
	Scope           string `json:"scope"`
	Resource        string `json:"resource"`
	Variant         string `json:"variant,omitempty"`
	Kind            string `json:"kind"`
	Realization     string `json:"realization"`
	Role            string `json:"role"`
	Path            string `json:"path,omitempty"`
	Route           string `json:"route,omitempty"`
	Operation       string `json:"operation,omitempty"`
	Selected        bool   `json:"selected"`
	Requested       bool   `json:"requested"`
	Default         bool   `json:"default"`
	SelectionSource string `json:"selection_source"`
	Source          string `json:"source"`
	Reason          string `json:"reason,omitempty"`
	Detail          string `json:"detail,omitempty"`
}

func ListPathRows(inventory listworkflow.LocationInventory) []ListPathRow {
	entries := inventory.Entries()
	rows := make([]ListPathRow, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, ListPathRow{
			Target: string(entry.Target()), Scope: string(entry.Scope()),
			Resource: string(entry.ResourceKind()), Variant: entry.Variant(),
			Kind: string(entry.Kind()), Realization: string(entry.Realization()), Role: string(entry.Role()),
			Path: entry.Path(), Route: entry.Route(), Operation: string(entry.Operation()),
			Selected: entry.Selected(), Requested: entry.Requested(), Default: entry.Default(),
			SelectionSource: string(entry.SelectionSource()), Source: string(entry.Source()),
			Reason: entry.Reason(), Detail: entry.Detail(),
		})
	}
	return rows
}

func PrintListPathsWithOptions(
	output io.Writer,
	manifestPath string,
	inventory listworkflow.LocationInventory,
	options HumanOptions,
) {
	entries := inventory.Entries()
	fmt.Fprintf(output, "manifest: %s\n", Escape(manifestPath))
	fmt.Fprintf(output, "locations: %d\n", len(entries))

	var previousTarget target.Target
	var previousScope target.Scope
	var previousResource string
	for _, entry := range entries {
		if entry.Target() != previousTarget {
			fmt.Fprintln(output, entry.Target())
			previousTarget = entry.Target()
			previousScope = ""
			previousResource = ""
		}
		if entry.Scope() != previousScope {
			fmt.Fprintf(output, "  %s\n", entry.Scope())
			previousScope = entry.Scope()
			previousResource = ""
		}
		resource := locationResourceLabel(entry.ResourceKind(), entry.Variant())
		if resource != previousResource {
			fmt.Fprintf(output, "    %s\n", resource)
			previousResource = resource
		}
		fmt.Fprintf(output, "      %s\n", locationEntrySummary(entry))
		if options.Verbose {
			fmt.Fprintf(
				output,
				"        realization=%s source=%s selection=%s requested=%t\n",
				entry.Realization(),
				entry.Source(),
				entry.SelectionSource(),
				entry.Requested(),
			)
		}
	}
}

func locationResourceLabel(kind entity.Kind, variant string) string {
	var label string
	switch kind {
	case entity.KindInstructions:
		label = "instructions"
	case entity.KindSkill:
		label = "skills"
	case entity.KindHook:
		label = "hooks"
	case entity.KindHookAsset:
		label = "hook assets"
	case entity.KindMCPServer:
		label = "MCP servers"
	case entity.KindExtension:
		label = "extensions"
	default:
		label = string(kind)
	}
	if variant != "" {
		return fmt.Sprintf("%s (%s)", label, Escape(variant))
	}
	return label
}

func locationEntrySummary(entry listworkflow.LocationEntry) string {
	tags := make([]string, 0, 3)
	if entry.Selected() {
		tags = append(tags, "selected")
	}
	if entry.Default() {
		tags = append(tags, "default")
	}
	if entry.Kind() == listworkflow.LocationUnsupported && entry.Requested() {
		tags = append(tags, "requested")
	}
	suffix := ""
	if len(tags) != 0 {
		suffix = " [" + strings.Join(tags, ", ") + "]"
	}

	switch entry.Kind() {
	case listworkflow.LocationPath:
		return fmt.Sprintf("%s: %s%s", locationRoleLabel(entry.Role()), Escape(entry.Path()), suffix)
	case listworkflow.LocationRoute:
		return fmt.Sprintf("%s: %s%s", entry.Operation(), Escape(entry.Route()), suffix)
	case listworkflow.LocationUnsupported:
		detail := ""
		if entry.Detail() != "" {
			detail = ": " + Escape(entry.Detail())
		}
		return fmt.Sprintf("unsupported: %s%s%s", humanReason(entry.Reason()), detail, suffix)
	default:
		return "unsupported: invalid location row"
	}
}

func locationRoleLabel(role listworkflow.LocationRole) string {
	switch role {
	case listworkflow.LocationRoleWrite:
		return "write"
	case listworkflow.LocationRoleDiscovery:
		return "discover"
	case listworkflow.LocationRoleRuntime:
		return "runtime"
	case listworkflow.LocationRoleConfig:
		return "config"
	case listworkflow.LocationRoleInternal:
		return "internal"
	default:
		return string(role)
	}
}

func humanReason(reason string) string {
	return strings.ReplaceAll(reason, "-", " ")
}

type listPathsJSONOutput struct {
	SchemaVersion int           `json:"schema_version"`
	Command       string        `json:"command"`
	ManifestPath  string        `json:"manifest_path"`
	LocationCount int           `json:"location_count"`
	Locations     []ListPathRow `json:"locations"`
}

func PrintListPathsJSON(
	output io.Writer,
	manifestPath string,
	inventory listworkflow.LocationInventory,
) error {
	rows := ListPathRows(inventory)
	payload := listPathsJSONOutput{
		SchemaVersion: listPathsJSONSchemaVersion,
		Command:       "list paths",
		ManifestPath:  manifestPath,
		LocationCount: len(rows),
		Locations:     rows,
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
