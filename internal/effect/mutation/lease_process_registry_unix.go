//go:build darwin || linux

package mutation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

type unixLeaseObjectID struct {
	device uint64
	inode  uint64
}

type unixProcessLeaseKey struct {
	directory unixLeaseObjectID
	name      string
}

type unixProcessLeaseState struct {
	changed    chan struct{}
	record     *unixLeaseRecord
	recordID   unixLeaseObjectID
	access     AccessMode
	holders    int
	references int
	acquiring  bool
}

type unixProcessLeaseRegistry struct {
	states map[unixProcessLeaseKey]*unixProcessLeaseState
	mu     sync.Mutex
}

type unixProcessLease struct {
	registry *unixProcessLeaseRegistry
	state    *unixProcessLeaseState
	key      unixProcessLeaseKey
	once     sync.Once
	err      error
}

var processLeaseRegistry = unixProcessLeaseRegistry{
	states: make(map[unixProcessLeaseKey]*unixProcessLeaseState),
}

func (registry *unixProcessLeaseRegistry) acquire(
	ctx context.Context,
	key unixProcessLeaseKey,
	recordID unixLeaseObjectID,
	record *unixLeaseRecord,
	access AccessMode,
	interval time.Duration,
) (*unixProcessLease, bool, error) {
	registry.mu.Lock()
	state := registry.states[key]
	if state == nil {
		state = &unixProcessLeaseState{changed: make(chan struct{})}
		registry.states[key] = state
	}
	state.references++

	for {
		if err := ctx.Err(); err != nil {
			registry.abandonLocked(key, state)
			registry.mu.Unlock()
			return nil, false, errors.Join(err, record.close())
		}
		if !state.acquiring && state.holders == 0 {
			state.acquiring = true
			registry.mu.Unlock()

			locked, err := acquireUnixFlock(ctx, record.file, access, interval)

			registry.mu.Lock()
			state.acquiring = false
			if err != nil || !locked {
				registry.signalLocked(state)
				registry.abandonLocked(key, state)
				registry.mu.Unlock()
				return nil, locked, errors.Join(err, record.close())
			}
			state.record = record
			state.recordID = recordID
			state.access = access
			state.holders = 1
			registry.signalLocked(state)
			registry.mu.Unlock()
			return &unixProcessLease{registry: registry, state: state, key: key}, true, nil
		}
		if !state.acquiring && state.access == AccessShared && access == AccessShared {
			if state.recordID != recordID {
				registry.abandonLocked(key, state)
				registry.mu.Unlock()
				return nil, false, errors.Join(
					fmt.Errorf("mutation lock record changed while held"),
					record.close(),
				)
			}
			if err := record.close(); err != nil {
				registry.abandonLocked(key, state)
				registry.mu.Unlock()
				return nil, false, fmt.Errorf("close redundant mutation lock descriptor: %w", err)
			}
			state.holders++
			registry.mu.Unlock()
			return &unixProcessLease{registry: registry, state: state, key: key}, true, nil
		}

		changed := state.changed
		registry.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
		registry.mu.Lock()
	}
}

func (registry *unixProcessLeaseRegistry) abandonLocked(
	key unixProcessLeaseKey,
	state *unixProcessLeaseState,
) {
	state.references--
	if state.references == 0 && state.holders == 0 && !state.acquiring {
		delete(registry.states, key)
	}
}

func (registry *unixProcessLeaseRegistry) signalLocked(state *unixProcessLeaseState) {
	close(state.changed)
	state.changed = make(chan struct{})
}

func (registry *unixProcessLeaseRegistry) release(
	key unixProcessLeaseKey,
	state *unixProcessLeaseState,
) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.states[key] != state || state.holders <= 0 || state.references <= 0 {
		return fmt.Errorf("process mutation lease is not active")
	}
	state.holders--
	state.references--
	if state.holders > 0 {
		return nil
	}

	record := state.record
	state.record = nil
	state.recordID = unixLeaseObjectID{}
	state.access = AccessMode(0)
	err := record.unlock()
	registry.signalLocked(state)
	if state.references == 0 && !state.acquiring {
		delete(registry.states, key)
	}
	return err
}

func (lease *unixProcessLease) Unlock() error {
	if lease == nil {
		return nil
	}
	lease.once.Do(func() {
		if lease.registry == nil || lease.state == nil {
			lease.err = fmt.Errorf("process mutation lease is not initialized")
			return
		}
		lease.err = lease.registry.release(lease.key, lease.state)
	})
	return lease.err
}

func leaseObjectID(file *os.File) (unixLeaseObjectID, error) {
	if file == nil {
		return unixLeaseObjectID{}, fmt.Errorf("mutation lease descriptor is required")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return unixLeaseObjectID{}, err
	}
	return unixLeaseObjectID{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}

func (record *unixLeaseRecord) close() error {
	if record == nil || record.file == nil {
		return nil
	}
	return record.file.Close()
}

func (record *unixLeaseRecord) unlock() error {
	if record == nil || record.file == nil {
		return nil
	}
	return errors.Join(
		unix.Flock(int(record.file.Fd()), unix.LOCK_UN),
		record.file.Close(),
	)
}
