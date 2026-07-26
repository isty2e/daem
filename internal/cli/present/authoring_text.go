package clipresent

import (
	"fmt"
	"io"
	"strings"

	authoringworkflow "github.com/isty2e/daem/internal/workflow/authoring"
)

// AuthoringOperation identifies the public command grammar axis for one manifest edit.
type AuthoringOperation string

const (
	AuthoringOperationAdd    AuthoringOperation = "add"
	AuthoringOperationRemove AuthoringOperation = "remove"
)

// AuthoringResourceKind identifies the public manifest grammar family being edited.
type AuthoringResourceKind string

const (
	AuthoringResourceExtension    AuthoringResourceKind = "extension"
	AuthoringResourceHook         AuthoringResourceKind = "hook"
	AuthoringResourceInstructions AuthoringResourceKind = "instructions"
	AuthoringResourceMCPServer    AuthoringResourceKind = "mcp_server"
	AuthoringResourceSkill        AuthoringResourceKind = "skill"
	AuthoringResourceSkillGroup   AuthoringResourceKind = "skill_group"
)

type AuthoringChangeSummary struct {
	Label        string
	ResourceID   string
	ChangeKind   string
	ManifestPath string
	Lockfile     AuthoringLockfile
	PlannedBlock string
	Warnings     []string
	NextStepNote string
	DryRun       bool
}

// AuthoringChangeFrom projects one canonical authoring result into the human output contract.
func AuthoringChangeFrom(
	label string,
	operation AuthoringOperation,
	resourceKind AuthoringResourceKind,
	result authoringworkflow.OperationResult,
) AuthoringChangeSummary {
	manifestBlock, warnings := authoringAddedContent(operation, result)
	return AuthoringChangeSummary{
		Label:        label,
		ResourceID:   string(resourceKind) + "/" + result.ResourceID,
		ChangeKind:   result.ChangeKind,
		ManifestPath: result.ManifestPath,
		Lockfile:     authoringLockfileFrom(result),
		PlannedBlock: manifestBlock,
		Warnings:     warnings,
		NextStepNote: authoringNextStepNote(operation, resourceKind),
		DryRun:       result.Mode == authoringworkflow.AuthoringModeDryRun,
	}
}

// ManifestAuthoringJSONFrom projects one canonical authoring result into schema v2.
func ManifestAuthoringJSONFrom(
	operation AuthoringOperation,
	resourceKind AuthoringResourceKind,
	result authoringworkflow.OperationResult,
) ManifestAuthoringJSONOutput {
	manifestBlock, warnings := authoringAddedContent(operation, result)
	return ManifestAuthoringResourceJSON(ManifestAuthoringResourceJSONInput{
		Command:       string(operation),
		Mode:          string(result.Mode),
		Operation:     string(operation),
		ManifestPath:  result.ManifestPath,
		Lockfile:      authoringLockfileFrom(result),
		ResourceKind:  string(resourceKind),
		ResourceName:  result.ResourceID,
		ChangeKind:    result.ChangeKind,
		ManifestBlock: manifestBlock,
		Warnings:      warnings,
	})
}

func authoringLockfileFrom(result authoringworkflow.OperationResult) AuthoringLockfile {
	return AuthoringLockfile{
		Path:   result.Lockfile.Path(),
		Status: string(result.Lockfile.Status()),
	}
}

func authoringAddedContent(
	operation AuthoringOperation,
	result authoringworkflow.OperationResult,
) (string, []string) {
	if operation != AuthoringOperationAdd {
		return "", nil
	}
	return result.ManifestBlock, append([]string(nil), result.Warnings...)
}

func authoringNextStepNote(operation AuthoringOperation, resourceKind AuthoringResourceKind) string {
	switch operation {
	case AuthoringOperationAdd:
		switch resourceKind {
		case AuthoringResourceHook:
			return "add updates the manifest and lockfile only; host hook config changes only when apply reconciles managed hook aggregates"
		case AuthoringResourceMCPServer:
			return "add updates the manifest and lockfile only; MCP config changes only when apply reconciles the locked projection"
		case AuthoringResourceExtension:
			return "add updates the manifest and lockfile only; carrier lifecycle changes require a separately admitted host route"
		default:
			return "add updates the manifest and lockfile only; host files are written only by apply"
		}
	case AuthoringOperationRemove:
		switch resourceKind {
		case AuthoringResourceHook:
			return "remove updates the manifest and lockfile only; host hook config changes only when apply reconciles managed hook aggregates"
		case AuthoringResourceMCPServer:
			return "remove updates the manifest and lockfile only; MCP config changes only when apply removes the managed projection"
		case AuthoringResourceExtension:
			return "remove updates the manifest and lockfile only; carrier uninstall, package cleanup, and bundled contribution deletion are not performed"
		default:
			return "remove updates the manifest and lockfile only; host files are deleted only when apply reconciles managed state"
		}
	default:
		return ""
	}
}

func PrintAuthoringChangeWithOptions(output io.Writer, summary AuthoringChangeSummary, options HumanOptions) {
	fmt.Fprintf(output, "%s: %s\n", summary.Label, summary.ResourceID)
	fmt.Fprintf(output, "change: %s\n", summary.ChangeKind)
	fmt.Fprintf(output, "manifest: %s\n", Escape(summary.ManifestPath))
	printAuthoringLockfileLine(output, summary.Lockfile)
	if options.Verbose && summary.DryRun && summary.PlannedBlock != "" {
		fmt.Fprintln(output, "planned:")
		fmt.Fprintln(output, summary.PlannedBlock)
	}
	for _, warning := range summary.Warnings {
		fmt.Fprintf(output, "warning: %s\n", Escape(warning))
	}
	if summary.DryRun {
		fmt.Fprintln(output, "next: rerun this authoring command without --dry-run")
	} else {
		PrintShellCommand(output, "next: run ", "daem", "apply", "--manifest", summary.ManifestPath, "--dry-run")
	}
	if strings.TrimSpace(summary.NextStepNote) != "" {
		fmt.Fprintf(output, "note: %s\n", Escape(summary.NextStepNote))
	}
}

func printAuthoringLockfileLine(output io.Writer, lockfile AuthoringLockfile) {
	if lockfile.Path == "" {
		return
	}
	switch lockfile.Status {
	case "would_write":
		fmt.Fprintf(output, "lockfile: would write %s\n", Escape(lockfile.Path))
	case "written":
		fmt.Fprintf(output, "lockfile: wrote %s\n", Escape(lockfile.Path))
	default:
		fmt.Fprintf(output, "lockfile: unchanged %s\n", Escape(lockfile.Path))
	}
}

// PrintUnmanageExtensionWithOptions writes the bounded human contract for one
// host-preserving management release.
func PrintUnmanageExtensionWithOptions(
	output io.Writer,
	result authoringworkflow.UnmanageExtensionResult,
	options HumanOptions,
) {
	fmt.Fprintf(output, "unmanage: extension/%s\n", Escape(result.ResourceID))
	fmt.Fprintf(
		output,
		"manifest: %s %s\n",
		unmanageManifestVerb(result.ManifestStatus),
		Escape(result.ManifestPath),
	)
	printAuthoringLockfileLine(output, AuthoringLockfile{
		Path:   result.LockfilePath,
		Status: string(result.LockfileStatus),
	})
	fmt.Fprintf(output, "management: %s\n", unmanageManagementText(result.ManagementStatus))
	fmt.Fprintln(output, "host: retained")
	if result.AmbientConsumersUnobservable {
		fmt.Fprintln(output, "ambient consumers: unobservable")
	}
	if options.Verbose {
		fmt.Fprintf(
			output,
			"statefile: %s %s\n",
			result.StatefileStatus,
			Escape(result.StatefilePath),
		)
		fmt.Fprintf(
			output,
			"registry: %s %s\n",
			result.RegistryStatus,
			Escape(result.RegistryPath),
		)
	}
	if result.Mode == authoringworkflow.UnmanageModeDryRun {
		fmt.Fprintln(output, "next: rerun this unmanage command without --dry-run")
		return
	}
	fmt.Fprintln(output, "note: daem management ended; host state was retained")
}

func unmanageManifestVerb(status authoringworkflow.UnmanageManifestStatus) string {
	switch status {
	case authoringworkflow.UnmanageManifestStatusWouldRemove:
		return "would remove"
	case authoringworkflow.UnmanageManifestStatusRemoved:
		return "removed"
	default:
		return "unchanged"
	}
}

func unmanageManagementText(status authoringworkflow.UnmanageManagementStatus) string {
	switch status {
	case authoringworkflow.UnmanageManagementStatusWouldRelease:
		return "would release"
	case authoringworkflow.UnmanageManagementStatusReleased:
		return "released"
	default:
		return "not present"
	}
}
