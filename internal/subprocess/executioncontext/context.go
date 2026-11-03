// Package executioncontext qualifies local host, boot and process observations.
// Stored identities and ordinary process exit results do not grant its evidence.
package executioncontext

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/isty2e/daem/internal/executionidentity"
)

type ExecutionObservationKind string

const (
	ExecutionContextUnavailable  ExecutionObservationKind = "execution_context_unavailable"
	ExecutionContextIncomparable ExecutionObservationKind = "execution_context_incomparable"
	ExecutionGroupOccupied       ExecutionObservationKind = "execution_group_occupied"
	ExecutionGroupUnsignalable   ExecutionObservationKind = "execution_group_unsignalable"
	ExecutionGroupUnknown        ExecutionObservationKind = "execution_group_unknown"
	ExecutionIdentityInvalid     ExecutionObservationKind = "execution_identity_invalid"
)

type ExecutionObservationError struct {
	kind  ExecutionObservationKind
	cause error
}

func (failure *ExecutionObservationError) Error() string                  { return string(failure.kind) }
func (failure *ExecutionObservationError) Unwrap() error                  { return failure.cause }
func (failure *ExecutionObservationError) Kind() ExecutionObservationKind { return failure.kind }

type QuiescenceBasis string

const (
	ProcessGroupAbsent QuiescenceBasis = "process_group_absent"
	PreviousBootEnded  QuiescenceBasis = "previous_boot_ended"
)

// ExecutionQuiescence is local observation evidence for one exact binding.
// Its zero value and a matching executionidentity value grant no authority.
type ExecutionQuiescence struct {
	binding executionidentity.Binding
	basis   QuiescenceBasis
}

func (observation ExecutionQuiescence) Basis() QuiescenceBasis { return observation.basis }

func (observation ExecutionQuiescence) Require(binding executionidentity.Binding) error {
	if binding.Validate() != nil || observation.binding != binding ||
		(observation.basis != ProcessGroupAbsent && observation.basis != PreviousBootEnded) {
		return &ExecutionObservationError{kind: ExecutionIdentityInvalid}
	}
	return nil
}

// ObserveExecutionContext obtains platform facts, not a process-ownership grant.
// Machine identifiers are reduced to application-specific keyed digests locally.
func ObserveExecutionContext(ctx context.Context) (executionidentity.Context, error) {
	if ctx == nil {
		return executionidentity.Context{}, &ExecutionObservationError{kind: ExecutionContextUnavailable}
	}
	if err := ctx.Err(); err != nil {
		return executionidentity.Context{}, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	value, err := readExecutionContext(ctx)
	if err != nil {
		return executionidentity.Context{}, &ExecutionObservationError{kind: ExecutionContextUnavailable, cause: err}
	}
	if err := ctx.Err(); err != nil {
		return executionidentity.Context{}, err
	}
	if value.Validate() != nil || !value.HasVisibility() {
		return executionidentity.Context{}, &ExecutionObservationError{kind: ExecutionContextUnavailable}
	}
	return value, nil
}

// ObserveExecutionQuiescence never signals a recorded group. Occupied, foreign,
// unqualified and uncertain observations leave the caller's safety fence intact.
func ObserveExecutionQuiescence(ctx context.Context, binding executionidentity.Binding) (ExecutionQuiescence, error) {
	if binding.Validate() != nil || binding.Group() <= 1 || !binding.Context().HasVisibility() {
		return ExecutionQuiescence{}, &ExecutionObservationError{kind: ExecutionIdentityInvalid}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	current, err := ObserveExecutionContext(ctx)
	if err != nil {
		return ExecutionQuiescence{}, err
	}
	switch binding.Context().Relation(current) {
	case executionidentity.DifferentEpoch:
		return ExecutionQuiescence{binding: binding, basis: PreviousBootEnded}, nil
	case executionidentity.SameEpoch:
		if err := requireRecordedProcessGroupAbsent(binding.Group()); err != nil {
			return ExecutionQuiescence{}, err
		}
		if err := ctx.Err(); err != nil {
			return ExecutionQuiescence{}, err
		}
		return ExecutionQuiescence{binding: binding, basis: ProcessGroupAbsent}, nil
	default:
		return ExecutionQuiescence{}, &ExecutionObservationError{kind: ExecutionContextIncomparable}
	}
}

func executionContextDigest(purpose string, parts ...string) string {
	digest := hmac.New(sha256.New, []byte("daem.execution-context.v1"))
	for _, part := range append([]string{purpose}, parts...) {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func canonicalExecutionHostID(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) == 36 {
		if value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
			return "", errors.New("invalid platform host identity")
		}
		value = strings.ReplaceAll(value, "-", "")
	}
	if len(value) != 32 {
		return "", errors.New("invalid platform host identity")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return "", errors.New("invalid platform host identity")
	}
	for _, part := range decoded {
		if part != 0 {
			return value, nil
		}
	}
	return "", fmt.Errorf("empty platform host identity")
}
