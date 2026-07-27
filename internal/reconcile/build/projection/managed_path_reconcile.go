package projection

import (
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization"
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
	if !hasState {
		if !current.Exists() {
			return newManagedPathCreate(input, reconcile.ReasonMissingOutput)
		}
		if manageUnmanagedMatches && current.ContentHash() == input.DesiredHash &&
			input.PermissionPolicy.AcceptsMode(input.DesiredFileMode, current.FileMode()) {
			return newManagedPathRecord(input, reconcile.ReasonManagedExisting, "")
		}
		detail := "destination exists but is not recorded as managed"
		if input.ContentKind == realization.PathProjectionFile &&
			!input.PermissionPolicy.AcceptsMode(input.DesiredFileMode, current.FileMode()) {
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
		if !previousEvidence.Exists() || previousEvidence.ContentHash() != state.ContentHash() ||
			!state.PermissionPolicy().AcceptsMode(state.FileMode(), previousEvidence.FileMode()) {
			return newManagedPathBlocked(input, reconcile.ReasonDriftedOutput, "previous managed destination differs from statefile baseline")
		}
		if current.Exists() {
			return newManagedPathBlocked(input, reconcile.ReasonUnmanagedOutputExists, "replacement destination already exists")
		}
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
	if current.ContentHash() != state.ContentHash() && current.ContentHash() != input.DesiredHash {
		return newManagedPathBlocked(input, reconcile.ReasonDriftedOutput, "managed output content differs from statefile baseline")
	}
	if current.ContentHash() != input.DesiredHash {
		return newManagedPathReplace(input, reconcile.ReasonContentChanged, "")
	}
	if !input.PermissionPolicy.AcceptsMode(input.DesiredFileMode, current.FileMode()) {
		return newManagedPathReplace(input, reconcile.ReasonFileModeChanged, "")
	}
	if state.ContentHash() != input.DesiredHash ||
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
