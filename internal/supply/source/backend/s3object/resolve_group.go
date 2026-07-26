package s3object

import (
	"context"
	"fmt"
	"sync"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

// resolutionGroup coalesces in-flight resolution of one canonical S3 source
// within a resolver instance. Completed results are not memoized.
type resolutionGroup struct {
	mu    sync.Mutex
	calls map[artifact.SourceID]*resolutionCall
}

type resolutionCall struct {
	done   chan struct{}
	result acquisition.Resolution
	err    error
}

func (group *resolutionGroup) do(
	ctx context.Context,
	sourceID artifact.SourceID,
	resolve func(context.Context) (acquisition.Resolution, error),
) (acquisition.Resolution, error) {
	if err := sourceID.Validate(); err != nil {
		return acquisition.Resolution{}, err
	}
	if ctx == nil {
		return acquisition.Resolution{}, fmt.Errorf("s3 resolution context is required")
	}
	if err := ctx.Err(); err != nil {
		return acquisition.Resolution{}, err
	}
	if resolve == nil {
		return acquisition.Resolution{}, fmt.Errorf("s3 resolution function is required for source %q", sourceID)
	}

	group.mu.Lock()
	if group.calls == nil {
		group.calls = make(map[artifact.SourceID]*resolutionCall)
	}
	if existing, ok := group.calls[sourceID]; ok {
		group.mu.Unlock()
		select {
		case <-existing.done:
			return existing.result, existing.err
		default:
		}
		select {
		case <-existing.done:
			return existing.result, existing.err
		case <-ctx.Done():
			select {
			case <-existing.done:
				return existing.result, existing.err
			default:
			}
			return acquisition.Resolution{}, fmt.Errorf("wait for s3 source %q: %w", sourceID, ctx.Err())
		}
	}

	current := &resolutionCall{done: make(chan struct{})}
	group.calls[sourceID] = current
	group.mu.Unlock()

	current.result, current.err = resolve(ctx)
	close(current.done)

	group.mu.Lock()
	delete(group.calls, sourceID)
	group.mu.Unlock()

	return current.result, current.err
}
