package execute

import (
	"context"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"

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

type statefileCommitOutcome struct {
	status statefileCommitStatus
	err    error
}

type statefileCommitter func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome

func commitCarrierState(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	next durable.Snapshot,
	stateEncoder durable.SnapshotEncoder,
	label string,
) error {
	if stateEncoder == nil {
		return fmt.Errorf("%s state codec is required", label)
	}
	content, err := stateEncoder.Encode(next)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", label, err)
	}
	if err := rootedSnapshotCommitter(filesystem, authority)(ctx, content, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", label, err)
	}
	return nil
}

func rootedSnapshotCommitter(
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
) snapshotCommitter {
	return func(ctx context.Context, content []byte, mode os.FileMode) error {
		return commitRootedControlFile(ctx, filesystem, authority, content, mode)
	}
}

func (outcome statefileCommitOutcome) committed() bool {
	return outcome.valid() && outcome.status == statefileCommitted
}

func (outcome statefileCommitOutcome) requiresRecovery() bool {
	if !outcome.valid() {
		return true
	}
	return outcome.status == statefileCommitIndeterminate
}

func (outcome statefileCommitOutcome) failure() error {
	if outcome.err != nil {
		return outcome.err
	}
	return fmt.Errorf("invalid statefile commit outcome %d", outcome.status)
}

func (outcome statefileCommitOutcome) valid() bool {
	switch outcome.status {
	case statefileCommitted:
		return outcome.err == nil
	case statefileUncommitted, statefileCommitIndeterminate:
		return outcome.err != nil
	default:
		return false
	}
}
