package s3object

import (
	"context"
	"fmt"
	"sync"

	"github.com/isty2e/daem/internal/supply/source"
)

// clientConfiguration is the complete S3 client specialization currently
// exposed by the source contract. Add future profile or endpoint fields here
// before teaching a client factory to interpret them.
type clientConfiguration struct {
	region string
}

func clientConfigurationFor(s3Source source.S3Source) clientConfiguration {
	return clientConfiguration{region: s3Source.Region()}
}

type clientFactory func(context.Context, clientConfiguration) (client, error)

// clientPool owns one resolver-scoped AWS configuration epoch. Successful
// clients are shared by configuration; failures and cancellation are not.
type clientPool struct {
	mu      sync.Mutex
	factory clientFactory
	entries map[clientConfiguration]*clientPoolEntry
}

type clientPoolEntry struct {
	ready  chan struct{}
	client client
	err    error
}

func newClientPool(factory clientFactory) *clientPool {
	return &clientPool{
		factory: factory,
		entries: make(map[clientConfiguration]*clientPoolEntry),
	}
}

func (pool *clientPool) get(
	ctx context.Context,
	configuration clientConfiguration,
) (client, error) {
	if ctx == nil {
		return nil, fmt.Errorf("s3 client pool context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pool == nil || pool.factory == nil {
		return nil, fmt.Errorf("s3 client pool is not initialized")
	}

	pool.mu.Lock()
	if entry, ok := pool.entries[configuration]; ok {
		pool.mu.Unlock()
		select {
		case <-entry.ready:
			return entry.client, entry.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	entry := &clientPoolEntry{ready: make(chan struct{})}
	pool.entries[configuration] = entry
	pool.mu.Unlock()

	resolved, err := pool.factory(ctx, configuration)
	if err == nil && resolved == nil {
		err = fmt.Errorf("s3 client factory returned a nil client")
	}

	pool.mu.Lock()
	entry.client = resolved
	entry.err = err
	if err != nil && pool.entries[configuration] == entry {
		delete(pool.entries, configuration)
	}
	close(entry.ready)
	pool.mu.Unlock()

	return resolved, err
}
