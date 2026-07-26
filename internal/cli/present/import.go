package clipresent

import (
	"fmt"
	"io"
	"path/filepath"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/target"
)

type ImportPlan struct {
	Label         string
	DryRun        bool
	HasErrors     bool
	ResourceCount int
	ManifestPath  string
	SourceDir     string
	Summary       ImportSummary
	Scans         []ImportScan
	Resources     []ImportResource
	MergeResults  []ImportMergeResult
	Skipped       []ImportSkipped
}

type ImportSummary struct {
	Instructions int
	Skills       int
	Hooks        int
	MCPServers   int
	Skipped      int
	Scans        int
	Rows         []ImportSummaryRow
}

type ImportSummaryRow struct {
	Target       string
	Scope        string
	Instructions int
	Skills       int
	Hooks        int
	MCPServers   int
}

type ImportScan struct {
	ResourceName string
	Target       string
	Scope        string
	LivePath     string
	Status       string
	Entries      int
	Imported     int
	Skipped      int
}

type ImportResource struct {
	ResourceID string
	Target     string
	Scope      string
	Source     string
	LivePath   string
	RenderTo   string
	Event      string
	Command    string
	Hook       bool
	MCPServer  bool
}

type ImportMergeResult struct {
	Resource string
	Status   string
	Detail   string
}

type ImportSkipped struct {
	LivePath string
	Reason   string
}

func PrintImportPlanWithOptions(output io.Writer, plan ImportPlan, options HumanOptions) {
	fmt.Fprintf(output, "%s: %d resources\n", plan.Label, plan.ResourceCount)
	printImportSummary(output, plan)
	if options.Verbose {
		printImportEvidence(output, plan)
	}
	for _, skipped := range plan.Skipped {
		fmt.Fprintf(output, "skip live=%q reason=%s\n", skipped.LivePath, skipped.Reason)
	}
	fmt.Fprintf(output, "manifest: %s\n", Escape(plan.ManifestPath))
	if options.Verbose {
		fmt.Fprintf(output, "source-dir: %s\n", Escape(plan.SourceDir))
	}
	printImportNextAction(output, plan)
	fmt.Fprintln(output, "note: import writes or merges the manifest and copied source files only; host files are written only by apply")
}

func printImportEvidence(output io.Writer, plan ImportPlan) {
	for _, scan := range plan.Scans {
		fmt.Fprintf(
			output,
			"scan resource=%q target=%s scope=%s live=%q status=%s entries=%d imported=%d skipped=%d\n",
			scan.ResourceName,
			scan.Target,
			scan.Scope,
			scan.LivePath,
			scan.Status,
			scan.Entries,
			scan.Imported,
			scan.Skipped,
		)
	}
	for _, resource := range plan.Resources {
		if resource.MCPServer {
			fmt.Fprintf(
				output,
				"import resource=%q target=%s scope=%s live=%q command=%q\n",
				resource.ResourceID,
				resource.Target,
				resource.Scope,
				resource.LivePath,
				resource.Command,
			)
			continue
		}
		if resource.Hook {
			fmt.Fprintf(
				output,
				"import resource=%q target=%s scope=%s live=%q event=%q command=%q\n",
				resource.ResourceID,
				resource.Target,
				resource.Scope,
				resource.LivePath,
				resource.Event,
				resource.Command,
			)
			continue
		}
		fmt.Fprintf(
			output,
			"import resource=%q target=%s scope=%s source=%q live=%q",
			resource.ResourceID,
			resource.Target,
			resource.Scope,
			resource.Source,
			resource.LivePath,
		)
		if resource.RenderTo != "" {
			fmt.Fprintf(output, " render_to=%q", resource.RenderTo)
		}
		fmt.Fprintln(output)
	}
	for _, result := range plan.MergeResults {
		fmt.Fprintf(output, "merge resource=%q status=%s detail=%q\n", result.Resource, result.Status, result.Detail)
	}
}

func printImportNextAction(output io.Writer, plan ImportPlan) {
	if plan.HasErrors {
		fmt.Fprintln(output, "next: resolve reported import conflicts before rerunning import without --dry-run")
		return
	}
	if plan.DryRun {
		fmt.Fprintln(output, "next: rerun daem import without --dry-run")
		return
	}
	PrintShellCommand(output, "next: run ", "daem", "lock", "--manifest", plan.ManifestPath, "--dry-run")
	if plan.ResourceCount != 0 {
		fmt.Fprintln(output, "note: after writing the lockfile, eligible exact-match imported outputs can be previewed for ownership recording without rewriting them")
		PrintShellCommand(output, "next: run ", "daem", "apply", "--manifest", plan.ManifestPath, "--manage-existing", "--dry-run")
	}
}

func printImportSummary(output io.Writer, plan ImportPlan) {
	fmt.Fprintf(
		output,
		"summary: instructions=%d skills=%d hooks=%d mcp_servers=%d skipped=%d scans=%d\n",
		plan.Summary.Instructions,
		plan.Summary.Skills,
		plan.Summary.Hooks,
		plan.Summary.MCPServers,
		plan.Summary.Skipped,
		plan.Summary.Scans,
	)
	for _, row := range plan.Summary.Rows {
		fmt.Fprintf(
			output,
			"summary target=%s scope=%s instructions=%d skills=%d hooks=%d mcp_servers=%d\n",
			row.Target,
			row.Scope,
			row.Instructions,
			row.Skills,
			row.Hooks,
			row.MCPServers,
		)
	}
	fmt.Fprintf(output, "destination: manifest=%s source-dir=%s\n", Escape(plan.ManifestPath), Escape(plan.SourceDir))
}

func ImportPlanFromAdoption(label string, plan adoptmodel.Plan, dryRun bool) ImportPlan {
	return ImportPlan{
		Label:         label,
		DryRun:        dryRun,
		HasErrors:     plan.HasMergeConflicts(),
		ResourceCount: plan.ResourceCount(),
		ManifestPath:  plan.Output(),
		SourceDir:     plan.SourceDirectory().Root(),
		Summary:       importSummaryFromAdoption(plan),
		Scans:         importScansFromAdoption(plan.Scans()),
		Resources:     importResourcesFromAdoption(plan),
		MergeResults:  importMergeResultsFromAdoption(plan.MergeResults()),
		Skipped:       importSkippedFromAdoption(plan.Skipped()),
	}
}

func importSummaryFromAdoption(plan adoptmodel.Plan) ImportSummary {
	return ImportSummary{
		Instructions: len(plan.Sources()),
		Skills:       len(plan.Skills()),
		Hooks:        len(plan.Hooks()),
		MCPServers:   len(plan.MCPServers()),
		Skipped:      len(plan.Skipped()),
		Scans:        len(plan.Scans()),
		Rows:         importSummaryRowsFromAdoption(plan.SummaryRows()),
	}
}

func importSummaryRowsFromAdoption(rows []adoptmodel.SummaryRow) []ImportSummaryRow {
	result := make([]ImportSummaryRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, ImportSummaryRow{
			Target:       string(row.Target),
			Scope:        string(row.Scope),
			Instructions: row.Instructions,
			Skills:       row.Skills,
			Hooks:        row.Hooks,
			MCPServers:   row.MCPServers,
		})
	}
	return result
}

func importScansFromAdoption(scans []adoptmodel.Scan) []ImportScan {
	result := make([]ImportScan, 0, len(scans))
	for _, scan := range scans {
		result = append(result, ImportScan{
			ResourceName: scan.ResourceName,
			Target:       string(scan.Target),
			Scope:        string(scan.Scope),
			LivePath:     scan.LivePath,
			Status:       scan.Status,
			Entries:      scan.Entries,
			Imported:     scan.Imported,
			Skipped:      scan.Skipped,
		})
	}
	return result
}

func importResourcesFromAdoption(plan adoptmodel.Plan) []ImportResource {
	resources := make([]ImportResource, 0, plan.ResourceCount())
	for _, source := range plan.Sources() {
		resources = append(resources, ImportResource{
			ResourceID: "instructions/" + source.ResourceName,
			Target:     string(source.Target),
			Scope:      string(source.Scope),
			Source:     filepath.ToSlash(source.SourcePath),
			LivePath:   source.LivePath,
			RenderTo:   source.RenderTo,
		})
	}
	for _, skill := range plan.Skills() {
		resources = append(resources, ImportResource{
			ResourceID: "skill/" + skill.ResourceName,
			Target:     string(skill.Target),
			Scope:      string(skill.Scope),
			Source:     filepath.ToSlash(skill.SourcePath),
			LivePath:   skill.LivePath,
		})
	}
	for _, hook := range plan.Hooks() {
		resources = append(resources, ImportResource{
			ResourceID: "hook/" + hook.ResourceName,
			Target:     string(hook.Target),
			Scope:      string(hook.Scope),
			LivePath:   hook.LivePath,
			Event:      hook.Event,
			Command:    hook.Command,
			Hook:       true,
		})
	}
	for _, server := range plan.MCPServers() {
		resources = append(resources, ImportResource{
			ResourceID: "mcp_server/" + server.ResourceName,
			Target:     string(server.Target),
			Scope:      string(server.Scope),
			LivePath:   server.LivePath,
			Command:    server.Command,
			MCPServer:  true,
		})
	}
	return resources
}

func importMergeResultsFromAdoption(results []adoptmodel.MergeResult) []ImportMergeResult {
	presentResults := make([]ImportMergeResult, 0, len(results))
	for _, result := range results {
		presentResults = append(presentResults, ImportMergeResult{
			Resource: result.Resource,
			Status:   string(result.Status),
			Detail:   result.Detail,
		})
	}
	return presentResults
}

func importSkippedFromAdoption(skipped []adoptmodel.Skipped) []ImportSkipped {
	result := make([]ImportSkipped, 0, len(skipped))
	for _, item := range skipped {
		result = append(result, ImportSkipped{
			LivePath: item.LivePath,
			Reason:   item.Reason,
		})
	}
	return result
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
		})
	}
	return summary
}

func importPlanJSONScans(scans []adoptmodel.Scan) []ImportAuthoringJSONScan {
	result := make([]ImportAuthoringJSONScan, 0, len(scans))
	for _, scan := range scans {
		result = append(result, ImportAuthoringJSONScan{
			ResourceID: authoringResourceID("skill", scan.ResourceName),
			Resource:   authoringJSONResource("skill", scan.ResourceName),
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
