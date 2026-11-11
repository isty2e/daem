package statefileoperation

import (
	"context"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type statefileCommitStatus uint8

const (
	statefileCommitInvalid statefileCommitStatus = iota
	statefileCommitted
	statefileUncommitted
	statefileCommitIndeterminate
)

type Outcome struct {
	status statefileCommitStatus
	err    error
}

type SnapshotCommitter func(context.Context, []byte, os.FileMode) error

func CommitSnapshot(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	next durable.Snapshot,
	stateEncoder statefile.PhysicalCodec,
	label string,
) error {
	if stateEncoder.IsZero() {
		return fmt.Errorf("%s state codec is required", label)
	}
	content, err := stateEncoder.EncodeState(next)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", label, err)
	}
	if err := RootedSnapshotCommitter(filesystem, authority)(ctx, content, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", label, err)
	}
	return nil
}

func RootedSnapshotCommitter(
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
) SnapshotCommitter {
	return func(ctx context.Context, content []byte, mode os.FileMode) error {
		return commitRootedControlFile(ctx, filesystem, authority, content, mode)
	}
}

func FromError(err error) Outcome {
	if err == nil {
		return Outcome{status: statefileCommitted}
	}
	status := statefileUncommitted
	if mutationfs.MayHaveVisibleEffect(err) {
		status = statefileCommitIndeterminate
	}
	return Outcome{status: status, err: err}
}

func (outcome Outcome) Committed() bool {
	return outcome.valid() && outcome.status == statefileCommitted
}

func (outcome Outcome) RequiresRecovery() bool {
	if !outcome.valid() {
		return true
	}
	return outcome.status == statefileCommitIndeterminate
}

func (outcome Outcome) Failure() error {
	if outcome.err != nil {
		return outcome.err
	}
	return fmt.Errorf("invalid statefile commit outcome %d", outcome.status)
}

func (outcome Outcome) valid() bool {
	switch outcome.status {
	case statefileCommitted:
		return outcome.err == nil
	case statefileUncommitted, statefileCommitIndeterminate:
		return outcome.err != nil
	default:
		return false
	}
}
