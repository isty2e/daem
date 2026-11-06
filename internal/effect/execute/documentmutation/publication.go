package documentmutation

import (
	"context"
	"errors"
	"fmt"
	"os"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (operation *Operation) PublishCaptured(ctx context.Context, document journalpublication.CapturedNativeDocument) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	expected, expectedMode := document.Expected()
	if expected.Exists() != (expectedMode != nil) {
		return fmt.Errorf("native document has inconsistent expected root metadata")
	}
	capability, err := document.Namespace().AcquireReferent(ctx, operation.physicalWorkBudget)
	if err != nil {
		return err
	}
	open := true
	defer func() {
		if open {
			resultErr = errors.Join(resultErr, capability.Close())
		}
	}()
	maximum := min(document.Comparison().MaximumDocumentBytes(), operation.physicalWorkBudget.RemainingBytes())
	if maximum <= 0 {
		return fmt.Errorf("native document revalidation exhausted its byte allocation")
	}
	content, mode, identity, readErr := operation.filesystem.ReadRootedRegularFileUpTo(ctx, capability, maximum)
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		charged = maximum
	}
	work, err := workmeasure.NewArtifactWork(0, charged)
	if err == nil {
		err = operation.physicalWorkBudget.AdmitTree(work)
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
		destination, err := operation.removals.DestinationFor(intent.Scope(), intent.Destination())
		if err != nil {
			return err
		}
		open = false
		_, err = operation.removals.RemoveRootedEntry(ctx, destination, capability, identity)
		return err
	}
	open = false
	if actual.Exists() {
		return operation.filesystem.ReplaceRootedFile(ctx, capability, expected.Content(), expectedMode.FileMode(), identity)
	}
	return operation.filesystem.CreateRootedFile(ctx, capability, expected.Content(), expectedMode.FileMode())
}
