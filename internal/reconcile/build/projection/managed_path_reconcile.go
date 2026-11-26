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
	matchesBaseline := hasState && current.MatchesBaseline(state.ContentHash())
	if !hasState {
		if !current.Exists() {
			return newManagedPathCreate(input, reconcile.ReasonMissingOutput)
		}
		if manageUnmanagedMatches && current.Matches(input.DesiredHash) {
			return newManagedPathRecord(input, reconcile.ReasonManagedExisting, "")
		}
		return newManagedPathBlocked(input, reconcile.ReasonUnmanagedOutputExists, "destination exists but is not recorded as managed")
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
		if !retirePrevious && !previousEvidence.MatchesBaseline(state.ContentHash()) {
			return newManagedPathBlocked(input, reconcile.ReasonDriftedOutput, baselineMismatch(state))
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
	matchesDesired := current.Matches(input.DesiredHash)
	if current.VerifiedMembers(state.ContentHash()).Known() {
		input.PreviousObservation = current
		// An extra can already contain the bytes of a newly shipped file.
		// Compare releases so that its previous permissions are not adopted.
		matchesDesired = current.InstalledBaseline(state.ContentHash()) == input.DesiredHash
	}
	if !matchesBaseline && !matchesDesired {
		return newManagedPathBlocked(input, reconcile.ReasonDriftedOutput, baselineMismatch(state))
	}
	if !matchesDesired {
		return newManagedPathReplace(input, reconcile.ReasonContentChanged, "")
	}
	if !matchesBaseline ||
		state.PermissionPolicy() != input.PermissionPolicy ||
		(state.PermissionPolicy() == realization.PathPermissionsExact && state.FileMode() != input.DesiredFileMode) ||
		!sameManagedPathConsumers(state.ConsumerTargets(), input.ConsumerTargets) {
		return newManagedPathRecord(input, reconcile.ReasonStateStale, "")
	}
	if current.VerifiedMembers(state.ContentHash()).Known() {
		// A no-op depends on the shipped contents, not the presence of unrelated entries.
		input.LiveHash = input.DesiredHash
		input.PreviousObservation = observe.ManagedPathEvidence{}
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
