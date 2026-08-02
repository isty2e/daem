package readiness

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/observe/filesnapshot"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func observeAggregatePrecondition(
	resolver liveobserve.DestinationResolver,
	precondition aggregate.OperationPrecondition,
) (bool, error) {
	document := precondition.DocumentAddress()
	logical := document.AggregateRoot()
	if err := liveobserve.ValidateAggregateReadPreconditions(logical, resolver); err != nil {
		return false, err
	}
	physical, err := resolver(logical)
	if err != nil {
		return false, fmt.Errorf("resolve aggregate operation precondition %q: %w", logical, err)
	}
	_, err = os.Lstat(physical)
	switch {
	case err == nil:
		return precondition.Kind() != aggregate.OperationPreconditionDocumentAbsent, nil
	case errors.Is(err, os.ErrNotExist):
		return precondition.Kind() == aggregate.OperationPreconditionDocumentAbsent, nil
	default:
		return false, fmt.Errorf("inspect aggregate operation precondition %q: %w", logical, err)
	}
}

func readAggregateDocument(
	ctx context.Context,
	path string,
	maximumBytes int64,
) (aggregate.Document, os.FileMode, error) {
	snapshot, exists, err := filesnapshot.ReadRegularFileSnapshotContext(ctx, path, maximumBytes)
	if !exists && err == nil {
		return aggregate.AbsentDocument(), 0, nil
	}
	if errors.Is(err, filesnapshot.ErrNotRegular) || errors.Is(err, filesnapshot.ErrSymlink) {
		return aggregate.Document{}, 0, fmt.Errorf("aggregate destination is not a regular file")
	}
	if err != nil {
		return aggregate.Document{}, 0, err
	}
	return aggregate.ExistingDocument(snapshot.Content()), snapshot.Mode(), nil
}
