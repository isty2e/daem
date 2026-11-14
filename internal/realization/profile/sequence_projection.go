package profile

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
)

// NativeSequenceProjection locates one admitted array within its portable
// document. It contains no observed rows, membership or filesystem authority.
type NativeSequenceProjection struct {
	sequence    hostrelation.PhysicalSequenceID
	destination output.Destination
	grammar     configuration.Grammar
	array       output.KeyPath
}

func NewNativeSequenceProjection(sequence hostrelation.PhysicalSequenceID, destination output.Destination, grammar configuration.Grammar, array output.KeyPath) (NativeSequenceProjection, error) {
	if err := sequence.Validate(); err != nil {
		return NativeSequenceProjection{}, err
	}
	canonical, err := output.Parse(destination.String())
	if err != nil || canonical != destination {
		return NativeSequenceProjection{}, fmt.Errorf("native sequence requires a canonical document reference")
	}
	switch grammar {
	case configuration.JSON, configuration.JSONC, configuration.JSONLineComments:
	default:
		return NativeSequenceProjection{}, fmt.Errorf("native sequence requires a supported JSON array grammar")
	}
	if array.IsRoot() {
		return NativeSequenceProjection{}, fmt.Errorf("native sequence requires a non-root array key")
	}
	return NativeSequenceProjection{sequence: sequence, destination: destination, grammar: grammar, array: array}, nil
}

func (projection NativeSequenceProjection) Validate() error {
	_, err := NewNativeSequenceProjection(projection.sequence, projection.destination, projection.grammar, projection.array)
	return err
}

func (projection NativeSequenceProjection) SequenceID() hostrelation.PhysicalSequenceID {
	return projection.sequence
}

func (projection NativeSequenceProjection) Destination() output.Destination {
	return projection.destination
}

func (projection NativeSequenceProjection) Grammar() configuration.Grammar { return projection.grammar }

func (projection NativeSequenceProjection) ArrayPath() output.KeyPath { return projection.array }

func mustNativeSequenceProjection(sequence, destination string, grammar configuration.Grammar, array ...string) NativeSequenceProjection {
	selected, err := output.Parse(destination)
	if err != nil {
		panic(err)
	}
	path, err := output.NewKeyPath(array...)
	if err != nil {
		panic(err)
	}
	projection, err := NewNativeSequenceProjection(mustPhysicalSequenceID(sequence), selected, grammar, path)
	if err != nil {
		panic(err)
	}
	return projection
}
