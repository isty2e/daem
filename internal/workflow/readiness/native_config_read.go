package readiness

import (
	"context"
	"fmt"
	"path/filepath"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func nativeConfigReader(native []nativeAssessmentSources, resolver liveobserve.DestinationResolver) (func(context.Context, string, int64) ([]byte, bool, error), error) {
	if len(native) == 0 || native[0].input == nil || native[0].input.Documents == nil {
		return nil, nil
	}
	batch := native[0].input.Documents
	documents := make(map[string]aggregate.Document)
	for _, namespace := range batch.Evidence().Namespaces() {
		for _, alias := range namespace.Addresses() {
			path, err := resolver(alias.AggregateRoot())
			if err != nil {
				return nil, err
			}
			document, _, observed := batch.Evidence().Document(alias)
			if !observed {
				return nil, fmt.Errorf("configuration read requires its retained native document")
			}
			documents[filepath.Clean(path)] = document
			if exact, present := namespace.Observation().Exact(); present {
				documents[exact.Key()] = document
			}
		}
	}
	return func(ctx context.Context, path string, maximumBytes int64) ([]byte, bool, error) {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if document, retained := documents[filepath.Clean(path)]; retained {
			if maximumBytes <= 0 || document.ContentSize() > maximumBytes {
				return nil, false, filesnapshot.ErrLimitExceeded
			}
			return document.Content(), document.Exists(), nil
		}
		return filesnapshot.ReadRegularFileContext(ctx, path, maximumBytes)
	}, nil
}
