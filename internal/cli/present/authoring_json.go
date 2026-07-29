package clipresent

import (
	"encoding/json"
	"io"
	"path/filepath"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/target"
	authoringworkflow "github.com/isty2e/daem/internal/workflow/authoring"
)

const manifestAuthoringJSONSchemaVersion = 2

type AuthoringLockfile struct {
	Path   string
	Status string
}

type ManifestAuthoringJSONOutput struct {
	SchemaVersion int                            `json:"schema_version"`
	Command       string                         `json:"command"`
	Mode          string                         `json:"mode"`
	Operation     string                         `json:"operation"`
	ManifestPath  string                         `json:"manifest_path"`
	Lockfile      *ManifestAuthoringJSONLockfile `json:"lockfile,omitempty"`
	SourceDir     string                         `json:"source_dir,omitempty"`
	ResourceCount int                            `json:"resource_count"`
	ChangeCount   int                            `json:"change_count"`
	HasErrors     bool                           `json:"has_errors"`
	Changes       []ManifestAuthoringJSONChange  `json:"changes"`
	Management    *UnmanageManagementJSON        `json:"management,omitempty"`
	Host          *UnmanageHostJSON              `json:"host,omitempty"`
	Warnings      []string                       `json:"warnings,omitempty"`
	Summary       []ImportAuthoringJSONSummary   `json:"summary,omitempty"`
	Scans         []ImportAuthoringJSONScan      `json:"scans,omitempty"`
	Skipped       []ImportAuthoringJSONSkipped   `json:"skipped,omitempty"`
	MergeResults  []ImportAuthoringJSONMerge     `json:"merge_results,omitempty"`
}

type ManifestAuthoringJSONChange struct {
	Operation     string                      `json:"operation"`
	ChangeKind    string                      `json:"change_kind,omitempty"`
	Status        string                      `json:"status,omitempty"`
	ResourceID    string                      `json:"resource_id"`
	Resource      AuthoringJSONResourceObject `json:"resource"`
	Target        string                      `json:"target,omitempty"`
	Targets       []string                    `json:"targets,omitempty"`
	Scope         string                      `json:"scope,omitempty"`
	Source        string                      `json:"source,omitempty"`
	Carrier       string                      `json:"carrier,omitempty"`
	LivePath      string                      `json:"live_path,omitempty"`
	RenderTo      string                      `json:"render_to,omitempty"`
	Detail        string                      `json:"detail,omitempty"`
	ManifestBlock string                      `json:"manifest_block,omitempty"`
}

type ManifestAuthoringJSONLockfile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type UnmanageManagementJSON struct {
	Status    string                  `json:"status"`
	Statefile UnmanageManagedFileJSON `json:"statefile"`
	Registry  UnmanageManagedFileJSON `json:"registry"`
}

type UnmanageManagedFileJSON struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type UnmanageHostJSON struct {
	State            string `json:"state"`
	AmbientConsumers string `json:"ambient_consumers,omitempty"`
}

type ImportAuthoringJSONSummary struct {
	Target       string `json:"target"`
	Scope        string `json:"scope"`
	Instructions int    `json:"instructions"`
	Skills       int    `json:"skills"`
	Hooks        int    `json:"hooks"`
	MCPServers   int    `json:"mcp_servers"`
	Extensions   int    `json:"extensions"`
}

type ImportAuthoringJSONScan struct {
	ResourceID string                      `json:"resource_id"`
	Resource   AuthoringJSONResourceObject `json:"resource"`
	Target     string                      `json:"target"`
	Scope      string                      `json:"scope"`
	LivePath   string                      `json:"live_path"`
	Status     string                      `json:"status"`
	Entries    int                         `json:"entries"`
	Imported   int                         `json:"imported"`
	Skipped    int                         `json:"skipped"`
}

type ImportAuthoringJSONSkipped struct {
	LivePath string `json:"live_path"`
	Reason   string `json:"reason"`
}

type ImportAuthoringJSONMerge struct {
	ResourceID string `json:"resource_id"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
}

type AuthoringJSONResourceObject struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type ManifestAuthoringResourceJSONInput struct {
	Command       string
	Mode          string
	Operation     string
	ManifestPath  string
	Lockfile      AuthoringLockfile
	ResourceKind  string
	ResourceName  string
	ChangeKind    string
	ManifestBlock string
	Warnings      []string
}

type ImportManifestAuthoringJSONInput struct {
	Mode          string
	ManifestPath  string
	SourceDir     string
	ResourceCount int
	HasErrors     bool
	Changes       []ManifestAuthoringJSONChange
	Summary       []ImportAuthoringJSONSummary
	Scans         []ImportAuthoringJSONScan
	Skipped       []ImportAuthoringJSONSkipped
	MergeResults  []ImportAuthoringJSONMerge
}

func PrintManifestAuthoringJSON(output io.Writer, payload ManifestAuthoringJSONOutput) error {
	payload.SchemaVersion = manifestAuthoringJSONSchemaVersion
	payload.ChangeCount = len(payload.Changes)
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func authoringJSONResource(kind string, name string) AuthoringJSONResourceObject {
	return AuthoringJSONResourceObject{Kind: kind, Name: name}
}

func authoringResourceID(kind string, name string) string {
	return kind + "/" + name
}

func authoringLockfileJSON(lockfile AuthoringLockfile) *ManifestAuthoringJSONLockfile {
	if lockfile.Path == "" {
		return nil
	}
	return &ManifestAuthoringJSONLockfile{
		Path:   lockfile.Path,
		Status: lockfile.Status,
	}
}

func manifestAuthoringJSONChanges(changes []ManifestAuthoringJSONChange) []ManifestAuthoringJSONChange {
	result := make([]ManifestAuthoringJSONChange, len(changes))
	copy(result, changes)
	return result
}

func ManifestAuthoringResourceJSON(input ManifestAuthoringResourceJSONInput) ManifestAuthoringJSONOutput {
	return ManifestAuthoringJSONOutput{
		Command:       input.Command,
		Mode:          input.Mode,
		Operation:     input.Operation,
		ManifestPath:  input.ManifestPath,
		Lockfile:      authoringLockfileJSON(input.Lockfile),
		ResourceCount: 1,
		HasErrors:     false,
		Changes: []ManifestAuthoringJSONChange{{
			Operation:     input.Operation,
			ChangeKind:    input.ChangeKind,
			ResourceID:    authoringResourceID(input.ResourceKind, input.ResourceName),
			Resource:      authoringJSONResource(input.ResourceKind, input.ResourceName),
			ManifestBlock: input.ManifestBlock,
		}},
		Warnings: append([]string(nil), input.Warnings...),
	}
}

// UnmanageExtensionJSONFrom projects one host-preserving unmanage result into
// the manifest-authoring JSON envelope.
func UnmanageExtensionJSONFrom(
	result authoringworkflow.UnmanageExtensionResult,
) ManifestAuthoringJSONOutput {
	change := ManifestAuthoringJSONChange{
		Operation:  "unmanage",
		ChangeKind: string(result.ManifestStatus),
		Status:     string(result.ManagementStatus),
		ResourceID: authoringResourceID(
			string(AuthoringResourceExtension),
			result.ResourceID,
		),
		Resource: authoringJSONResource(
			string(AuthoringResourceExtension),
			result.ResourceID,
		),
		Target: string(result.Target),
		Scope:  string(result.Scope),
	}
	host := &UnmanageHostJSON{State: "retained"}
	if result.AmbientConsumersUnobservable {
		host.AmbientConsumers = "unobservable"
	}
	return ManifestAuthoringJSONOutput{
		Command:       "unmanage",
		Mode:          string(result.Mode),
		Operation:     "unmanage",
		ManifestPath:  result.ManifestPath,
		Lockfile:      authoringLockfileJSON(AuthoringLockfile{Path: result.LockfilePath, Status: string(result.LockfileStatus)}),
		ResourceCount: 1,
		HasErrors:     false,
		Changes:       []ManifestAuthoringJSONChange{change},
		Management: &UnmanageManagementJSON{
			Status: string(result.ManagementStatus),
			Statefile: UnmanageManagedFileJSON{
				Path:   result.StatefilePath,
				Status: string(result.StatefileStatus),
			},
			Registry: UnmanageManagedFileJSON{
				Path:   result.RegistryPath,
				Status: string(result.RegistryStatus),
			},
		},
		Host: host,
	}
}

func ImportManifestAuthoringJSON(input ImportManifestAuthoringJSONInput) ManifestAuthoringJSONOutput {
	return ManifestAuthoringJSONOutput{
		Command:       "import",
		Mode:          input.Mode,
		Operation:     "import",
		ManifestPath:  input.ManifestPath,
		SourceDir:     input.SourceDir,
		ResourceCount: input.ResourceCount,
		HasErrors:     input.HasErrors,
		Changes:       manifestAuthoringJSONChanges(input.Changes),
		Summary:       append([]ImportAuthoringJSONSummary(nil), input.Summary...),
		Scans:         append([]ImportAuthoringJSONScan(nil), input.Scans...),
		Skipped:       append([]ImportAuthoringJSONSkipped(nil), input.Skipped...),
		MergeResults:  append([]ImportAuthoringJSONMerge(nil), input.MergeResults...),
	}
}

func ImportPlanJSONOutput(mode string, plan adoptmodel.Plan) ManifestAuthoringJSONOutput {
	return ImportManifestAuthoringJSON(ImportManifestAuthoringJSONInput{
		Mode:          mode,
		ManifestPath:  plan.Output(),
		SourceDir:     plan.SourceDirectory().Root(),
		ResourceCount: plan.ResourceCount(),
		HasErrors:     plan.HasMergeConflicts(),
		Changes:       importPlanJSONChanges(plan),
		Summary:       importPlanJSONSummary(plan),
		Scans:         importPlanJSONScans(plan.Scans()),
		Skipped:       importPlanJSONSkipped(plan.Skipped()),
		MergeResults:  importPlanJSONMergeResults(plan.MergeResults()),
	})
}

func importPlanJSONChanges(plan adoptmodel.Plan) []ManifestAuthoringJSONChange {
	changes := make([]ManifestAuthoringJSONChange, 0, plan.ResourceCount())
	for _, source := range plan.Sources() {
		changes = append(changes, ManifestAuthoringJSONChange{
			Operation:  "import",
			ResourceID: authoringResourceID("instructions", source.ResourceName),
			Resource:   authoringJSONResource("instructions", source.ResourceName),
			Target:     string(source.Target),
			Scope:      string(source.Scope),
			Source:     filepath.ToSlash(source.SourcePath),
			LivePath:   source.LivePath,
			RenderTo:   source.RenderTo,
		})
	}
	for _, skill := range plan.Skills() {
		changes = append(changes, ManifestAuthoringJSONChange{
			Operation:  "import",
			ResourceID: authoringResourceID("skill", skill.ResourceName),
			Resource:   authoringJSONResource("skill", skill.ResourceName),
			Target:     string(skill.Target),
			Targets:    importTargetStrings(skill.Targets),
			Scope:      string(skill.Scope),
			Source:     filepath.ToSlash(skill.SourcePath),
			LivePath:   skill.LivePath,
		})
	}
	for _, hook := range plan.Hooks() {
		changes = append(changes, ManifestAuthoringJSONChange{
			Operation:  "import",
			ResourceID: authoringResourceID("hook", hook.ResourceName),
			Resource:   authoringJSONResource("hook", hook.ResourceName),
			Target:     string(hook.Target),
			Scope:      string(hook.Scope),
			LivePath:   hook.LivePath,
			Detail:     hook.Event,
		})
	}
	for _, server := range plan.MCPServers() {
		changes = append(changes, ManifestAuthoringJSONChange{
			Operation:  "import",
			ResourceID: authoringResourceID("mcp_server", server.ResourceName),
			Resource:   authoringJSONResource("mcp_server", server.ResourceName),
			Target:     string(server.Target),
			Scope:      string(server.Scope),
			LivePath:   server.LivePath,
			Detail:     server.Command,
		})
	}
	for _, extension := range plan.Extensions() {
		changes = append(changes, ManifestAuthoringJSONChange{
			Operation:  "import",
			ResourceID: authoringResourceID("extension", extension.ID().Name()),
			Resource:   authoringJSONResource("extension", extension.ID().Name()),
			Target:     string(extension.Target()),
			Scope:      string(extension.Scope()),
			Source:     extension.Source().Ref(),
			Carrier:    string(extension.Carrier()),
		})
	}
	return changes
}

func importPlanJSONSummary(plan adoptmodel.Plan) []ImportAuthoringJSONSummary {
	summaryRows := plan.SummaryRows()
	summary := make([]ImportAuthoringJSONSummary, 0, len(summaryRows))
	for _, row := range summaryRows {
		summary = append(summary, ImportAuthoringJSONSummary{
			Target:       string(row.Target),
			Scope:        string(row.Scope),
			Instructions: row.Instructions,
			Skills:       row.Skills,
			Hooks:        row.Hooks,
			MCPServers:   row.MCPServers,
			Extensions:   row.Extensions,
		})
	}
	return summary
}

func importPlanJSONScans(scans []adoptmodel.Scan) []ImportAuthoringJSONScan {
	result := make([]ImportAuthoringJSONScan, 0, len(scans))
	for _, scan := range scans {
		result = append(result, ImportAuthoringJSONScan{
			ResourceID: authoringResourceID(scan.ResourceKind, scan.ResourceName),
			Resource:   authoringJSONResource(scan.ResourceKind, scan.ResourceName),
			Target:     string(scan.Target),
			Scope:      string(scan.Scope),
			LivePath:   scan.LivePath,
			Status:     scan.Status,
			Entries:    scan.Entries,
			Imported:   scan.Imported,
			Skipped:    scan.Skipped,
		})
	}
	return result
}

func importPlanJSONSkipped(skipped []adoptmodel.Skipped) []ImportAuthoringJSONSkipped {
	result := make([]ImportAuthoringJSONSkipped, 0, len(skipped))
	for _, item := range skipped {
		result = append(result, ImportAuthoringJSONSkipped{
			LivePath: item.LivePath,
			Reason:   item.Reason,
		})
	}
	return result
}

func importPlanJSONMergeResults(results []adoptmodel.MergeResult) []ImportAuthoringJSONMerge {
	merges := make([]ImportAuthoringJSONMerge, 0, len(results))
	for _, result := range results {
		merges = append(merges, ImportAuthoringJSONMerge{
			ResourceID: result.Resource,
			Status:     string(result.Status),
			Detail:     result.Detail,
		})
	}
	return merges
}

func importTargetStrings(targets []target.Target) []string {
	if len(targets) == 0 {
		return nil
	}
	values := make([]string, 0, len(targets))
	for _, target := range targets {
		values = append(values, string(target))
	}
	return values
}
