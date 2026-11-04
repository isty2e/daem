// Package footprintcodec projects output footprints across versioned metadata
// boundaries. The enclosing format owns version and strict JSON admission.
package footprintcodec

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
)

// Record is the tagged footprint representation in current metadata. KeyPath
// is inapplicable to whole-path authority; it never denotes a root selector.
type Record struct {
	Kind    string  `json:"kind"`
	KeyPath *string `json:"key_path,omitempty"`
}

func Encode(value output.Footprint) (Record, error) {
	if err := value.Validate(); err != nil {
		return Record{}, err
	}
	record := Record{Kind: value.Kind().String()}
	if key, selected := value.Key(); selected {
		pointer := key.String()
		record.KeyPath = &pointer
	}
	return record, nil
}

func Decode(record Record) (output.Footprint, error) {
	switch record.Kind {
	case "path":
		if record.KeyPath != nil {
			return output.Footprint{}, fmt.Errorf("%w: whole-path footprint must not carry a key path", output.ErrInvalidFootprint)
		}
		return output.WholePathFootprint(), nil
	case "key":
		if record.KeyPath == nil {
			return output.Footprint{}, fmt.Errorf("%w: key footprint requires a key path", output.ErrInvalidFootprint)
		}
		key, err := output.ParseKeyPath(*record.KeyPath)
		if err != nil {
			return output.Footprint{}, fmt.Errorf("%w: invalid key pointer", output.ErrInvalidFootprint)
		}
		return output.NewKeyFootprint(key)
	default:
		return output.Footprint{}, fmt.Errorf("%w: unknown footprint kind", output.ErrInvalidFootprint)
	}
}
