package statefile

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
)

// PhysicalCodec selects the representation of an actual State file. Its methods
// deliberately do not implement the frozen journal Snapshot codec ports.
type PhysicalCodec struct{ codec durable.SnapshotCodec }

func NewPhysicalCodec(codec durable.SnapshotCodec) (PhysicalCodec, error) {
	if codec == nil {
		return PhysicalCodec{}, fmt.Errorf("physical state codec is required")
	}
	return PhysicalCodec{codec: codec}, nil
}

func DefaultPhysicalCodec() PhysicalCodec { return PhysicalCodec{codec: Codec{}} }

func (codec PhysicalCodec) IsZero() bool { return codec.codec == nil }

func (codec PhysicalCodec) EncodeState(snapshot durable.Snapshot) ([]byte, error) {
	if codec.IsZero() {
		return nil, fmt.Errorf("physical state codec is required")
	}
	return codec.codec.Encode(snapshot)
}

func (codec PhysicalCodec) DecodeState(content []byte) (durable.Snapshot, error) {
	if codec.IsZero() {
		return durable.Snapshot{}, fmt.Errorf("physical state codec is required")
	}
	return codec.codec.Decode(content)
}
