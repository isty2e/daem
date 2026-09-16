package statefile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/contractversion"
)

// Relocation is a retired statefile's exact authority-transfer receipt, not a
// snapshot or permission to follow an arbitrary path.
type Relocation struct {
	from stateauthority.Authority
	to   stateauthority.Authority
}

// NewRelocation records one transition between distinct valid authorities.
func NewRelocation(from, to stateauthority.Authority) (Relocation, error) {
	if err := from.Validate(); err != nil {
		return Relocation{}, err
	}
	if err := to.Validate(); err != nil {
		return Relocation{}, err
	}
	if from.Equal(to) {
		return Relocation{}, fmt.Errorf("state relocation requires distinct authorities")
	}
	return Relocation{from: from, to: to}, nil
}

func (receipt Relocation) From() stateauthority.Authority { return receipt.from }
func (receipt Relocation) To() stateauthority.Authority   { return receipt.to }

type relocationDTO struct {
	Version int               `json:"version"`
	Kind    string            `json:"kind"`
	From    stateAuthorityDTO `json:"from"`
	To      stateAuthorityDTO `json:"to"`
}

// MarshalRelocation produces a strict document that snapshot readers reject.
func MarshalRelocation(receipt Relocation) ([]byte, error) {
	if _, err := NewRelocation(receipt.from, receipt.to); err != nil {
		return nil, err
	}
	return json.MarshalIndent(relocationDTO{
		Version: contractversion.StateRelocation, Kind: "state-relocation",
		From: persistedStateAuthority(receipt.from), To: persistedStateAuthority(receipt.to),
	}, "", "  ")
}

// LoadRelocation distinguishes a relocation receipt from ordinary state. It
// never obtains management authority from a receipt's destination field.
func LoadRelocation(ctx context.Context, path string) (Relocation, bool, error) {
	content, err := readStatefile(ctx, path)
	if err != nil {
		return Relocation{}, false, err
	}
	version, err := statefileDocumentVersion(content)
	if err != nil {
		return Relocation{}, false, err
	}
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(content, &header); err != nil {
		return Relocation{}, false, err
	}
	if header.Kind == "" {
		return Relocation{}, false, nil
	}
	if version != contractversion.StateRelocation || header.Kind != "state-relocation" {
		return Relocation{}, false, fmt.Errorf("unsupported state relocation document")
	}
	var wire relocationDTO
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return Relocation{}, false, err
	}
	from, err := wire.From.canonical()
	if err != nil {
		return Relocation{}, false, err
	}
	to, err := wire.To.canonical()
	if err != nil {
		return Relocation{}, false, err
	}
	receipt, err := NewRelocation(from, to)
	return receipt, err == nil, err
}
