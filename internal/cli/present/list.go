package clipresent

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

const listResourcesJSONSchemaVersion = 1

type ListRow struct {
	Kind        string `json:"kind"`
	Key         string `json:"key"`
	InstallName string `json:"install_name"`
	Source      string `json:"source"`
	Targets     string `json:"targets"`
	Scope       string `json:"scope"`
	Group       string `json:"group,omitempty"`
}

// ListRows maps selected canonical desired resources to the stable public list
// projection. Skill-group labels remain syntax-only presentation metadata.
func ListRows(
	environment desired.Environment,
	groupMembership map[string]string,
	selection targetselection.Selection,
) []ListRow {
	instructionsValues := environment.Instructions()
	skills := environment.Skills()
	hooks := environment.Hooks()
	mcpServers := environment.MCPServers()
	extensions := environment.Extensions()
	rows := make([]ListRow, 0, len(instructionsValues)+len(skills)+len(hooks)+len(mcpServers)+len(extensions))
	for _, instruction := range instructionsValues {
		if !listTargetsSelected(instruction.Targets(), selection) {
			continue
		}
		rows = append(rows, ListRow{
			Kind:        string(entity.KindInstructions),
			Key:         instruction.ID().Name(),
			InstallName: "-",
			Source:      listSourceSummary(instruction.Source()),
			Targets:     listTargetSummary(instruction.Targets()),
			Scope:       string(instruction.Scope()),
			Group:       "-",
		})
	}
	for _, skill := range skills {
		if !listTargetsSelected(skill.Targets(), selection) {
			continue
		}
		group := groupMembership[skill.ID().Name()]
		if group == "" {
			group = "-"
		}
		rows = append(rows, ListRow{
			Kind:        string(entity.KindSkill),
			Key:         skill.ID().Name(),
			InstallName: skill.InstallName(),
			Source:      listSourceSummary(skill.Source()),
			Targets:     listTargetSummary(skill.Targets()),
			Scope:       string(skill.Scope()),
			Group:       group,
		})
	}
	for _, hook := range hooks {
		if !listTargetsSelected(hook.Targets(), selection) {
			continue
		}
		rows = append(rows, ListRow{
			Kind:        string(entity.KindHook),
			Key:         hook.ID().Name(),
			InstallName: "-",
			Source:      "command-hook",
			Targets:     listTargetSummary(hook.Targets()),
			Scope:       string(hook.Scope()),
			Group:       "-",
		})
	}
	for _, server := range mcpServers {
		for _, binding := range server.Bindings() {
			if !selection.Includes(binding.Target()) {
				continue
			}
			rows = append(rows, ListRow{
				Kind:        string(entity.KindMCPServer),
				Key:         server.ID().Name(),
				InstallName: server.ID().Name(),
				Source:      listMCPTransportSummary(binding.Transport()),
				Targets:     string(binding.Target()),
				Scope:       string(binding.Scope()),
				Group:       "-",
			})
		}
	}
	for _, extension := range extensions {
		if !selection.Includes(extension.Target()) {
			continue
		}
		rows = append(rows, ListRow{
			Kind:        string(entity.KindExtension),
			Key:         extension.ID().Name(),
			InstallName: extension.ID().Name(),
			Source:      string(extension.Source().Kind()) + ":" + extension.Source().Ref(),
			Targets:     string(extension.Target()),
			Scope:       string(extension.Scope()),
			Group:       "-",
		})
	}
	sort.Slice(rows, func(leftIndex int, rightIndex int) bool {
		left := rows[leftIndex]
		right := rows[rightIndex]
		if listKindRank(left.Kind) != listKindRank(right.Kind) {
			return listKindRank(left.Kind) < listKindRank(right.Kind)
		}
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		if left.InstallName != right.InstallName {
			return left.InstallName < right.InstallName
		}
		if left.Scope != right.Scope {
			return left.Scope < right.Scope
		}
		return left.Targets < right.Targets
	})
	return rows
}

func PrintListRowsWithOptions(output io.Writer, manifestPath string, rows []ListRow, options HumanOptions) {
	fmt.Fprintf(output, "manifest: %s\n", Escape(manifestPath))
	fmt.Fprintf(output, "resources: %d\n", len(rows))
	for _, row := range rows {
		if options.Verbose {
			fmt.Fprintf(
				output,
				"resource kind=%s key=%s install=%s source=%s targets=%s scope=%s group=%s\n",
				row.Kind,
				strconv.Quote(row.Key),
				strconv.Quote(row.InstallName),
				strconv.Quote(row.Source),
				strconv.Quote(row.Targets),
				strconv.Quote(row.Scope),
				strconv.Quote(row.Group),
			)
			continue
		}
		fmt.Fprintf(
			output, "resource kind=%s key=%s install=%s targets=%s scope=%s",
			row.Kind,
			strconv.Quote(row.Key),
			strconv.Quote(row.InstallName),
			strconv.Quote(row.Targets),
			strconv.Quote(row.Scope),
		)
		fmt.Fprintln(output)
	}
}

type listResourcesJSONOutput struct {
	SchemaVersion int       `json:"schema_version"`
	Command       string    `json:"command"`
	ManifestPath  string    `json:"manifest_path"`
	ResourceCount int       `json:"resource_count"`
	Resources     []ListRow `json:"resources"`
}

func PrintListResourcesJSON(output io.Writer, manifestPath string, rows []ListRow) error {
	resources := make([]ListRow, len(rows))
	copy(resources, rows)
	payload := listResourcesJSONOutput{
		SchemaVersion: listResourcesJSONSchemaVersion,
		Command:       "list resources",
		ManifestPath:  manifestPath,
		ResourceCount: len(rows),
		Resources:     resources,
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func listMCPTransportSummary(transport desiredmcp.Transport) string {
	if stdio, ok := transport.Stdio(); ok {
		return stdio.Command().Name()
	}
	return ""
}

func listSourceSummary(resourceSource sourcepkg.Source) string {
	sourceID, err := sourcepkg.SourceIDFor(resourceSource)
	if err != nil {
		return string(resourceSource.Kind()) + ":<invalid>"
	}
	return string(sourceID)
}

func listTargetSummary(targets []target.Target) string {
	values := make([]string, 0, len(targets))
	for _, selectedTarget := range targets {
		values = append(values, string(selectedTarget))
	}
	return strings.Join(values, ",")
}

func listTargetsSelected(targets []target.Target, selection targetselection.Selection) bool {
	return slices.ContainsFunc(targets, selection.Includes)
}

func listKindRank(kind string) int {
	switch entity.Kind(kind) {
	case entity.KindInstructions:
		return 0
	case entity.KindSkill:
		return 1
	case entity.KindHook:
		return 2
	case entity.KindMCPServer:
		return 3
	case entity.KindExtension:
		return 4
	default:
		return 3
	}
}
