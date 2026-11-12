package statefile

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
)

// PhysicalCodec selects the representation of an actual State file. Its methods
// deliberately do not implement the frozen journal Snapshot codec ports.
type PhysicalCodec struct {
	codec     durable.SnapshotCodec
	selection *physicalStateSelection
}

type physicalStateSelection struct {
	before pathauthority.Reference
	after  pathauthority.Reference
}

type imageFactCodec interface {
	physicalImageFacts(context.Context) (snapshotCodec, error)
}

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
	if codec.selection != nil && !codec.selection.after.IsZero() {
		if _, owned := codec.codec.(imageFactCodec); !owned {
			content, err := codec.codec.Encode(snapshot)
			if err != nil {
				return nil, err
			}
			reference, err := stateImageReference(content)
			if err != nil {
				return nil, err
			}
			if !reference.Equal(codec.selection.after) {
				return nil, fmt.Errorf("selected physical encoder changed the State reference")
			}
			return content, nil
		}
		image, err := NewStateImage(snapshot, codec.selection.after)
		if err != nil {
			return nil, err
		}
		facts, err := codec.imageFactsCodec(context.Background())
		if err != nil {
			return nil, err
		}
		return marshalImage(image, facts)
	}
	return codec.codec.Encode(snapshot)
}

func (codec PhysicalCodec) DecodeState(content []byte) (durable.Snapshot, error) {
	if codec.IsZero() {
		return durable.Snapshot{}, fmt.Errorf("physical state codec is required")
	}
	if _, owned := codec.codec.(imageFactCodec); !owned {
		snapshot, err := codec.codec.Decode(content)
		if err != nil {
			return durable.Snapshot{}, err
		}
		if codec.selection != nil {
			reference, err := stateImageReference(content)
			if err != nil {
				return durable.Snapshot{}, err
			}
			if !codec.admitsReference(reference) {
				return durable.Snapshot{}, fmt.Errorf("statefile reference changed after observation: %w", mutation.StaleSnapshotError{})
			}
		}
		return snapshot, nil
	}
	version, err := statefileDocumentVersion(content)
	if err != nil {
		return durable.Snapshot{}, err
	}
	if version == locatedSnapshotVersion {
		facts, err := codec.imageFactsCodec(context.Background())
		if err != nil {
			return durable.Snapshot{}, err
		}
		image, err := unmarshalImage(content, facts)
		if err != nil {
			return durable.Snapshot{}, err
		}
		if !codec.admitsReference(image.Reference()) {
			return durable.Snapshot{}, fmt.Errorf("statefile reference changed after observation: %w", mutation.StaleSnapshotError{})
		}
		return image.Snapshot(), nil
	}
	if !codec.admitsReference(pathauthority.Reference{}) {
		return durable.Snapshot{}, fmt.Errorf("statefile image format changed after observation: %w", mutation.StaleSnapshotError{})
	}
	return codec.codec.Decode(content)
}

func (codec PhysicalCodec) admitsReference(reference pathauthority.Reference) bool {
	return codec.selection == nil || reference.Equal(codec.selection.before) || reference.Equal(codec.selection.after)
}

// ForObservedState keeps the observed physical selection while selecting this
// operation's fact validators. Neither codec implements the journal codec port.
func (codec PhysicalCodec) ForObservedState(observed PhysicalCodec) (PhysicalCodec, error) {
	if codec.IsZero() || observed.IsZero() {
		return PhysicalCodec{}, fmt.Errorf("selected and observed physical codecs are required")
	}
	codec.selection = observed.selection
	return codec, nil
}

// ForDestination selects the successor location for an explicit State transfer.
// Legacy input keeps its format; located input does not retain a moved self path.
func (codec PhysicalCodec) ForDestination(observed pathauthority.Reference) (PhysicalCodec, error) {
	if codec.IsZero() || codec.selection == nil {
		return PhysicalCodec{}, fmt.Errorf("observed physical State is required")
	}
	if err := observed.Validate(); err != nil {
		return PhysicalCodec{}, err
	}
	selection := *codec.selection
	if !selection.after.IsZero() {
		reference, err := SelectStatefileReference(selection.after, observed)
		if err != nil {
			return PhysicalCodec{}, err
		}
		selection.after = reference
	}
	codec.selection = &selection
	return codec, nil
}

func (codec PhysicalCodec) imageFactsCodec(ctx context.Context) (snapshotCodec, error) {
	selected, owned := codec.codec.(imageFactCodec)
	if !owned {
		return snapshotCodec{}, fmt.Errorf("State image requires a statefile-owned fact codec")
	}
	return selected.physicalImageFacts(ctx)
}
