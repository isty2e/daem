// Package statefileoperation owns selected statefile IO and commit outcomes,
// not the containing operation's state/Registry transaction policy.
package statefileoperation

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

type Selected struct {
	filesystem mutationfs.Store
	entry      *rootedpath.EntryAuthority
	budget     *physicalwork.Budget
}

// NewSelected takes custody of the bound entry, without acquiring or replacing it.
func NewSelected(filesystem mutationfs.Store, entry *rootedpath.EntryAuthority, budget *physicalwork.Budget) *Selected {
	return &Selected{filesystem: filesystem, entry: entry, budget: budget}
}

func (selected *Selected) Read(ctx context.Context, codec statefile.PhysicalCodec, budget *physicalwork.Budget) (result durable.Snapshot, resultErr error) {
	if ctx == nil || selected == nil || selected.entry == nil || codec.IsZero() || budget == nil || selected.budget != budget {
		return durable.Snapshot{}, fmt.Errorf("native state read requires selected authority, codec and original allocation")
	}
	if err := ctx.Err(); err != nil {
		return durable.Snapshot{}, err
	}
	capability, err := selected.entry.Acquire()
	if err != nil {
		return durable.Snapshot{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, capability.Close(), ctx.Err()) }()
	maximum := min(recovery.MaximumRecoveryBackupFileBytes, budget.RemainingBytes())
	if maximum <= 0 {
		return durable.Snapshot{}, fmt.Errorf("native state revalidation exhausted its byte allocation")
	}
	content, _, _, readErr := selected.filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximum)
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		charged = maximum
	}
	work, err := workmeasure.NewArtifactWork(0, charged)
	if err == nil {
		err = budget.AdmitTree(work)
	}
	if err != nil {
		return durable.Snapshot{}, err
	}
	if errors.Is(readErr, os.ErrNotExist) {
		return durable.EmptySnapshot(), nil
	}
	if readErr != nil {
		return durable.Snapshot{}, readErr
	}
	return codec.DecodeState(content)
}

func (selected *Selected) Commit(ctx context.Context, content []byte, mode os.FileMode) Outcome {
	if selected == nil || selected.filesystem == nil || selected.entry == nil {
		return Outcome{status: statefileUncommitted, err: fmt.Errorf("project statefile authority is unavailable")}
	}
	return FromError(commitRootedControlFile(ctx, selected.filesystem, selected.entry, content, mode))
}

func (selected *Selected) Close() error {
	if selected == nil {
		return nil
	}
	err := selected.entry.Close()
	selected.entry, selected.filesystem, selected.budget = nil, nil, nil
	return err
}

func commitRootedControlFile(ctx context.Context, filesystem mutationfs.RootedStore, authority *rootedpath.EntryAuthority, content []byte, mode os.FileMode) error {
	if filesystem == nil || authority == nil {
		return fmt.Errorf("rooted control-file authority is required")
	}
	capability, err := authority.Acquire()
	if err != nil {
		return err
	}
	expected, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if errors.Is(err, os.ErrNotExist) {
		return filesystem.CreateRootedFile(ctx, capability, content, mode)
	}
	if err != nil {
		_ = capability.Close()
		return err
	}
	return filesystem.ReplaceRootedFile(ctx, capability, content, mode, expected)
}
