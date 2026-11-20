package clipresent

import (
	"errors"
	"fmt"

	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/recoverygate"
)

func MissingStateFailureDiagnostic(err error) (FailureDiagnostic, bool) {
	if recoverygate.IsCancellation(err) || recoverygate.IsBarrierRefusal(err) {
		return FailureDiagnostic{}, false
	}
	var refusal *ownershipobserve.MissingStateRefusal
	if !errors.As(err, &refusal) {
		return FailureDiagnostic{}, false
	}
	diagnostic := NewFailureDiagnostic(refusal.Error())
	var protection *ownership.ProtectionPendingError
	switch {
	case errors.As(refusal, &protection):
		manifest := protection.Protection.Owner().ManifestPath()
		if manifest == refusal.ManifestPath() {
			diagnostic = diagnostic.WithGuidance(missingStateCommand(manifest, "to inspect pending permission recovery", "recover", "--dry-run"))
		} else if safeReleaseReference(manifest) {
			diagnostic = diagnostic.WithGuidance(InstructionGuidance(fmt.Sprintf("preserve the records and preview daem recover --dry-run for the original selection of the protection owner (%q)", manifest)))
		} else {
			diagnostic = diagnostic.WithGuidance(InstructionGuidance("preserve the records and preview daem recover --dry-run with the original selection of the protection owner"))
		}
		if errors.Is(refusal, ownership.ErrOwnerOutputsReserved) {
			diagnostic = diagnostic.WithGuidance(InstructionGuidance("reserved output claims also remain; keep their matching journal and recovery records"))
		}
	case errors.Is(refusal, ownership.ErrOwnerOutputsReserved):
		diagnostic = diagnostic.WithGuidance(InstructionGuidance("preserve the reserved claims; resume journal recovery only with matching recovery records as well as any required state backup"))
	case errors.Is(refusal, ownershipobserve.ErrAmbiguousStateOwner):
		diagnostic = diagnostic.WithGuidance(InstructionGuidance("check the original manifest and HOME/XDG selection; preserve the records and restore a matching state backup if available"))
	case errors.Is(refusal, ownershipobserve.ErrMissingStateWithOutputs):
		diagnostic = diagnostic.WithGuidance(
			InstructionGuidance("preserve the registry and host files; restore the last matching state backup with its private permissions if available"),
			missingStateCommand(refusal.ManifestPath(), "to check the workspace after restoring the backup", "status", "--check"),
			missingStateCommand(refusal.ManifestPath(), "to preview releasing output ownership without changing host files if no backup is available", "recover", "--release-output-ownership", "--dry-run"),
		)
	default:
		diagnostic = diagnostic.WithGuidance(InstructionGuidance("preserve the records and restore access to the output ownership registry before retrying"))
	}
	return diagnostic, true
}

func missingStateCommand(manifest, purpose, command string, flags ...string) Guidance {
	if safeReleaseReference(manifest) {
		args := ManifestCommandArguments([]string{command}, manifest, HumanOptions{}, flags...)
		if guidance, err := CommandGuidance(args, purpose); err == nil {
			return guidance
		}
	}
	args := append([]string{"daem", command}, flags...)
	invocation, _ := ShellCommand(args...)
	return InstructionGuidance("with the same manifest selection, run " + invocation + " " + purpose)
}
