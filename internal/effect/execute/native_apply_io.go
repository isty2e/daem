package execute

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func publishNativeDocument(ctx context.Context, input NativeApplyInput, document journalpublication.CapturedNativeDocument, authority *mutationAuthority) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	expected, expectedMode := document.Expected()
	if expected.Exists() != (expectedMode != nil) {
		return fmt.Errorf("native document has inconsistent expected root metadata")
	}
	capability, err := document.Namespace().AcquireReferent(ctx, input.Budget)
	if err != nil {
		return err
	}
	open := true
	defer func() {
		if open {
			resultErr = errors.Join(resultErr, capability.Close())
		}
	}()
	maximum := min(document.Comparison().MaximumDocumentBytes(), input.Budget.RemainingBytes())
	if maximum <= 0 {
		return fmt.Errorf("native document revalidation exhausted its byte allocation")
	}
	content, mode, identity, readErr := input.Filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximum)
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		charged = maximum
	}
	work, err := workmeasure.NewArtifactWork(0, charged)
	if err == nil {
		err = input.Budget.AdmitTree(work)
	}
	if err != nil {
		return err
	}
	actual := aggregate.AbsentDocument()
	if readErr == nil {
		actual = aggregate.ExistingDocument(content)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	original, originalMode := document.Original()
	if !original.Equal(actual) || actual.Exists() && (originalMode == nil || originalMode.FileMode() != mode.Perm()) {
		return fmt.Errorf("native document changed after its journal image was captured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if actual.Exists() && identity == nil {
		return fmt.Errorf("native document has no exact entry identity")
	}
	if !expected.Exists() {
		removal, present, err := document.Removal()
		if err != nil || !present {
			return errors.Join(err, fmt.Errorf("native removal requires its captured physical intent"))
		}
		intent, err := removal.PhysicalIntent()
		if err != nil {
			return err
		}
		destination, err := authority.removals.DestinationFor(intent.Scope(), intent.Destination())
		if err != nil {
			return err
		}
		open = false
		_, err = authority.removals.RemoveRootedEntry(ctx, destination, capability, identity)
		return err
	}
	open = false
	if actual.Exists() {
		return input.Filesystem.ReplaceRootedFile(ctx, capability, expected.Content(), expectedMode.FileMode(), identity)
	}
	return input.Filesystem.CreateRootedFile(ctx, capability, expected.Content(), expectedMode.FileMode())
}

func readNativeCommittedState(ctx context.Context, authority *mutationAuthority, input NativeApplyInput) (durable.Snapshot, error) {
	return authority.readNativeState(ctx, input.StateCodec, input.Budget)
}

func (authority *mutationAuthority) readNativeState(ctx context.Context, codec durable.SnapshotCodec, budget *physicalwork.Budget) (result durable.Snapshot, resultErr error) {
	if ctx == nil || authority == nil || authority.projectStatefile == nil || codec == nil || budget == nil || authority.physicalWorkBudget != budget {
		return durable.Snapshot{}, fmt.Errorf("native state read requires selected authority, codec and original allocation")
	}
	if err := ctx.Err(); err != nil {
		return durable.Snapshot{}, err
	}
	capability, err := authority.projectStatefile.Acquire()
	if err != nil {
		return durable.Snapshot{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, capability.Close(), ctx.Err()) }()
	maximum := min(recovery.MaximumRecoveryBackupFileBytes, budget.RemainingBytes())
	if maximum <= 0 {
		return durable.Snapshot{}, fmt.Errorf("native state revalidation exhausted its byte allocation")
	}
	content, _, _, readErr := authority.filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximum)
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
	return codec.Decode(content)
}
