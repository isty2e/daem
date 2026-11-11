// Package pathreferencecodec projects complete path references across metadata
// boundaries. Enclosing formats own versions and strict raw-input admission.
package pathreferencecodec

import "github.com/isty2e/daem/internal/assurance/pathauthority"

type Record struct {
	Key              string `json:"key"`
	SemanticsWitness string `json:"semantics_witness"`
	Locator          string `json:"locator"`
}

func Encode(reference pathauthority.Reference) (Record, error) {
	if err := reference.Validate(); err != nil {
		return Record{}, err
	}
	identity := reference.Identity()
	return Record{
		Key: identity.Key(), SemanticsWitness: identity.Witness(), Locator: reference.Locator(),
	}, nil
}

func Decode(record Record) (pathauthority.Reference, error) {
	identity, err := pathauthority.NewExact(record.Key, record.SemanticsWitness)
	if err != nil {
		return pathauthority.Reference{}, err
	}
	return pathauthority.NewReference(identity, record.Locator)
}
