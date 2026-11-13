package clipresent

import (
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func PrintUnmanageHostConfigWithOptions(output io.Writer, result authoring.UnmanageHostConfigResult, options HumanOptions) {
	fmt.Fprintf(output, "unmanage: %s\n", Escape(ResourceReference(entity.KindHostConfig, result.ResourceID)))
	fmt.Fprintf(output, "manifest: %s %s\n", unmanageManifestVerb(result.ManifestStatus), Escape(result.ManifestPath))
	printAuthoringLockfileLine(output, AuthoringLockfile{Path: result.LockfilePath, Status: string(result.LockfileStatus)})
	fmt.Fprintf(output, "management: %s\nhost: retained\n", unmanageManagementText(result.ManagementStatus))
	if options.Verbose {
		fmt.Fprintf(output, "statefile: %s %s\nregistry: %s %s\n", result.StatefileStatus, Escape(result.StatefilePath), result.RegistryStatus, Escape(result.RegistryPath))
	}
	if result.Mode == authoring.UnmanageModeDryRun {
		fmt.Fprintln(output, "next: rerun this unmanage command without --dry-run")
	}
}

func UnmanageHostConfigJSONFrom(result authoring.UnmanageHostConfigResult) ManifestAuthoringJSONOutput {
	return ManifestAuthoringJSONOutput{
		Command: "unmanage", Mode: string(result.Mode), Operation: "unmanage", ManifestPath: result.ManifestPath,
		Lockfile: authoringLockfileJSON(AuthoringLockfile{Path: result.LockfilePath, Status: string(result.LockfileStatus)}), ResourceCount: 1,
		Changes:    []ManifestAuthoringJSONChange{{Operation: "unmanage", ChangeKind: string(result.ManifestStatus), Status: string(result.ManagementStatus), ResourceID: authoringResourceID("host_config", result.ResourceID), Resource: authoringJSONResource("host_config", result.ResourceID), Target: string(result.Target), Scope: string(result.Scope)}},
		Management: &UnmanageManagementJSON{Status: string(result.ManagementStatus), Statefile: UnmanageManagedFileJSON{Path: result.StatefilePath, Status: string(result.StatefileStatus)}, Registry: UnmanageManagedFileJSON{Path: result.RegistryPath, Status: string(result.RegistryStatus)}},
		Host:       &UnmanageHostJSON{State: "retained"},
	}
}
