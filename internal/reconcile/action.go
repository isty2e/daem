package reconcile

import "fmt"

// ActionKind identifies the operation needed to reconcile one output.
type ActionKind string

const (
	ActionKindCreate ActionKind = "create"
	ActionKindUpdate ActionKind = "update"
	ActionKindDelete ActionKind = "delete"
	ActionKindRecord ActionKind = "record"
	ActionKindNoOp   ActionKind = "noop"
	ActionKindError  ActionKind = "error"
)

// ActionReason explains why an action was produced.
type ActionReason string

const (
	ReasonMissingOutput               ActionReason = "missing_output"
	ReasonContentChanged              ActionReason = "content_changed"
	ReasonDriftedOutput               ActionReason = "drifted_output"
	ReasonRemovedFromManifest         ActionReason = "removed_from_manifest"
	ReasonAlreadyCurrent              ActionReason = "already_current"
	ReasonStateStale                  ActionReason = "state_stale"
	ReasonFileModeChanged             ActionReason = "file_mode_changed"
	ReasonMissingLock                 ActionReason = "missing_lock"
	ReasonStaleLock                   ActionReason = "stale_lock"
	ReasonUnexpectedLockSubject       ActionReason = "unexpected_lock_subject"
	ReasonAggregateLockBlocked        ActionReason = "aggregate_lock_blocked"
	ReasonMissingLiveObservation      ActionReason = "missing_live_observation"
	ReasonUnmanagedOutputExists       ActionReason = "unmanaged_output_exists"
	ReasonManagedExisting             ActionReason = "managed_existing"
	ReasonDestinationConflict         ActionReason = "destination_conflict"
	ReasonInvalidDesiredState         ActionReason = "invalid_desired_output"
	ReasonOwnershipObservationMissing ActionReason = "ownership_observation_missing"
	ReasonOwnershipClaimMissing       ActionReason = "ownership_claim_missing"
	ReasonOwnershipConflict           ActionReason = "ownership_conflict"
	ReasonOwnershipReserved           ActionReason = "ownership_reserved"
	ReasonOwnershipStateConflict      ActionReason = "ownership_state_conflict"
)

func validateActionReason(reason ActionReason) error {
	switch reason {
	case ReasonMissingOutput,
		ReasonContentChanged,
		ReasonDriftedOutput,
		ReasonRemovedFromManifest,
		ReasonAlreadyCurrent,
		ReasonStateStale,
		ReasonFileModeChanged,
		ReasonMissingLock,
		ReasonStaleLock,
		ReasonUnexpectedLockSubject,
		ReasonAggregateLockBlocked,
		ReasonMissingLiveObservation,
		ReasonUnmanagedOutputExists,
		ReasonManagedExisting,
		ReasonDestinationConflict,
		ReasonInvalidDesiredState,
		ReasonOwnershipObservationMissing,
		ReasonOwnershipClaimMissing,
		ReasonOwnershipConflict,
		ReasonOwnershipReserved,
		ReasonOwnershipStateConflict:
		return nil
	default:
		return fmt.Errorf("action reason %q is unsupported", reason)
	}
}
