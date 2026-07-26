package commit

import (
	"context"
	"fmt"
	"io"
)

type phase string

const (
	phaseCaptureIdentity   phase = "capture_identity"
	phaseValidate          phase = "validate"
	phaseCreateAncestors   phase = "create_ancestors"
	phaseCreateTemporary   phase = "create_temporary"
	phaseWritePayload      phase = "write_payload"
	phaseReadPayload       phase = "read_payload"
	phaseApplyMode         phase = "apply_mode"
	phaseCaptureMetadata   phase = "capture_metadata"
	phaseApplyMetadata     phase = "apply_metadata"
	phaseSyncPayload       phase = "sync_payload"
	phaseClosePayload      phase = "close_payload"
	phaseRevalidateEntry   phase = "revalidate_entry"
	phaseCommitEntry       phase = "commit_entry"
	phaseVerifyEntry       phase = "verify_entry"
	phaseSyncParent        phase = "sync_parent"
	phaseSyncAncestors     phase = "sync_ancestors"
	phaseCleanupTemporary  phase = "cleanup_temporary"
	phaseCleanupAncestors  phase = "cleanup_ancestors"
	phaseSyncTreeFile      phase = "sync_tree_file"
	phaseSyncTreeDirectory phase = "sync_tree_directory"
	phaseCommitTombstone   phase = "commit_tombstone"
	phaseCleanupTombstone  phase = "cleanup_tombstone"
	phaseSyncCleanupParent phase = "sync_cleanup_parent"
	phaseUnsupported       phase = "unsupported_platform"
)

type faultPlan struct {
	failures     map[phase]error
	actions      map[phase]func()
	payloadWrite func(context.Context, io.Writer, []byte) error
}

func (plan faultPlan) check(ctx context.Context, current phase) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if action := plan.actions[current]; action != nil {
		action()
	}
	return plan.failures[current]
}

func (plan faultPlan) run(ctx context.Context, current phase, effect func() error) error {
	if err := plan.check(ctx, current); err != nil {
		return err
	}
	return effect()
}

func (plan faultPlan) writePayload(ctx context.Context, writer io.Writer, payload []byte) error {
	if err := plan.check(ctx, phaseWritePayload); err != nil {
		return err
	}
	if plan.payloadWrite != nil {
		return plan.payloadWrite(ctx, writer, payload)
	}
	return writeAllContext(ctx, writer, payload)
}

func writeAllContext(ctx context.Context, writer io.Writer, payload []byte) error {
	for len(payload) != 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		written, err := writer.Write(payload)
		if written < 0 || written > len(payload) {
			return fmt.Errorf("invalid write count %d for %d remaining bytes", written, len(payload))
		}
		payload = payload[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

type phaseError struct {
	phase phase
	cause error
}

func (err *phaseError) Error() string { return err.cause.Error() }

func (err *phaseError) Unwrap() error { return err.cause }

func atPhase(current phase, err error) error {
	if err == nil {
		return nil
	}
	return &phaseError{phase: current, cause: err}
}

func errorPhase(err error, fallback phase) phase {
	for current := err; current != nil; {
		if phased, ok := current.(*phaseError); ok {
			return phased.phase
		}
		type unwrapper interface{ Unwrap() error }
		wrapped, ok := current.(unwrapper)
		if !ok {
			break
		}
		current = wrapped.Unwrap()
	}
	return fallback
}
