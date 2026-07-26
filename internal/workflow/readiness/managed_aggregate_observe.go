package readiness

import (
	"errors"
	"fmt"
	"os"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func observeAggregatePrecondition(
	resolver liveobserve.DestinationResolver,
	precondition aggregate.OperationPrecondition,
) (bool, error) {
	document := precondition.DocumentAddress()
	logical := output.Destination(document.AggregateRoot())
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

func readAggregateDocument(path string) (aggregate.Document, os.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return aggregate.AbsentDocument(), 0, nil
	}
	if err != nil {
		return aggregate.Document{}, 0, err
	}
	if !info.Mode().IsRegular() {
		return aggregate.Document{}, 0, fmt.Errorf("aggregate destination is not a regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return aggregate.Document{}, 0, err
	}
	return aggregate.ExistingDocument(content), info.Mode(), nil
}
