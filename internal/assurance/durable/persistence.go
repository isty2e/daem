package durable

import "context"

// SnapshotEncoder encodes one canonical Snapshot through a caller-selected boundary.
// Implementations must be deterministic and side-effect-free.
type SnapshotEncoder interface {
	Encode(Snapshot) ([]byte, error)
}

// SnapshotCodec encodes and decodes one versioned durable snapshot representation.
type SnapshotCodec interface {
	SnapshotEncoder
	Decode([]byte) (Snapshot, error)
}

// SnapshotReader observes the current snapshot selected by a boundary.
type SnapshotReader func(context.Context) (Snapshot, error)
