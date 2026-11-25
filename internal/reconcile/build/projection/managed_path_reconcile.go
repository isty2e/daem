package projection

import (
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
)

func reconcileManagedPathDesired(
	input reconcile.ManagedPathDecisionInput,
	current observe.ManagedPathEvidence,
	state durable.ManagedPathState,
	hasState bool,
	evidence map[managedPathEvidenceKey]observe.ManagedPathEvidence,
	manageUnmanagedMatches bool,
) managedPathDecision {
	matchesBaseline := hasState && current.Matches(state.ContentHash())
	modeAccepted := realization.IsInstalledContentSubject(input.Subject) || input.PermissionPolicy.AcceptsMode(input.DesiredFileMode, current.FileMode())
	if !hasState {
		if !current.Exists() {
			return newManagedPathCreate(input, reconcile.ReasonMissingOutput)
		}
		if manageUnmanagedMatches && current.Matches(input.DesiredHash) && modeAccepted {
			return newManagedPathRecord(input, reconcile.ReasonManagedExisting, "")
		}
		detail := "destination exists but is not recorded as managed"
		if input.ContentKind == realization.PathProjectionFile && !modeAccepted {
			detail = "destination exists with a different file mode and is not recorded as managed"
		}
		return newManagedPathBlocked(input, reconcile.ReasonUnmanagedOutputExists, detail)
	}

	if state.Scope() != input.Scope || state.Destination() != input.Destination {
		previousEvidence, observed := evidence[managedPathEvidenceKey{
			subject:     state.Subject(),
			destination: state.Destination(),
		}]
		if !observed {
			return newManagedPathBlocked(input, reconcile.ReasonMissingLiveObservation, "fresh evidence for the previous managed destination is required")
		}
		retirePrevious := !previousEvidence.Exists() && aggregate.IsAgentConfigDocumentSubject(input.Subject)
		if !retirePrevious && (!previousEvidence.Matches(state.ContentHash()) ||
			!realization.IsInstalledContentSubject(state.Subject()) && !state.PermissionPolicy().AcceptsMode(state.FileMode(), previousEvidence.FileMode())) {
			return newManagedPathBlocked(input, reconcile.ReasonDriftedOutput, "previous managed destination differs from statefile baseline")
		}
		if current.Exists() {
			return newManagedPathBlocked(input, reconcile.ReasonUnmanagedOutputExists, "replacement destination already exists")
		}
		if retirePrevious {
			input.RetiresAbsentPrevious = true
			return newManagedPathReplace(input, reconcile.ReasonContentChanged, "previous managed destination is absent")
		}
		input.PreviousObservation = previousEvidence
		return newManagedPathReplace(input, reconcile.ReasonContentChanged, "managed destination changed")
	}

	if !current.Exists() {
		return newManagedPathCreate(input, reconcile.ReasonMissingOutput)
	}
	if input.PlacementMode != realization.PathProjectionCopy {
		return newManagedPathReplace(
			input,
			reconcile.ReasonContentChanged,
			"current path kind cannot satisfy the desired placement mode",
		)
	}
	if !matchesBaseline && !current.Matches(input.DesiredHash) {
		return newManagedPathBlocked(input, reconcile.ReasonDriftedOutput, baselineMismatch(state))
	}
	if !current.Matches(input.DesiredHash) {
		return newManagedPathReplace(input, reconcile.ReasonContentChanged, "")
	}
	if !modeAccepted {
		return newManagedPathReplace(input, reconcile.ReasonFileModeChanged, "")
	}
	if !matchesBaseline ||
		state.PermissionPolicy() != input.PermissionPolicy ||
		(state.PermissionPolicy() == realization.PathPermissionsExact && state.FileMode() != input.DesiredFileMode) ||
		!sameManagedPathConsumers(state.ConsumerTargets(), input.ConsumerTargets) {
		return newManagedPathRecord(input, reconcile.ReasonStateStale, "")
	}
	return newManagedPathNoOp(input, reconcile.ReasonAlreadyCurrent)
}

func managedFilePublishMode(executable bool) os.FileMode {
	if executable {
		return 0o700
	}
	return 0o600
}

func managedFileDesiredMode(
	policy realization.PathPermissionPolicy,
	realizationMode os.FileMode,
	executable bool,
) os.FileMode {
	if policy == realization.PathPermissionsExact {
		return realizationMode
	}
	return managedFilePublishMode(executable)
}
