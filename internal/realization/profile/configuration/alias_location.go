package configuration

import (
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
)

// AliasLocation names a canonical child of a selected directory root. It carries
// no carrier/layout policy, selected filesystem facts or publication authority.
type AliasLocation struct {
	reference output.RootReference
	relative  string
}

func NewAliasLocation(reference output.RootReference, relative string) (AliasLocation, error) {
	location := AliasLocation{reference: reference, relative: relative}
	if err := location.Validate(); err != nil {
		return AliasLocation{}, err
	}
	return location, nil
}

func (location AliasLocation) RootReference() output.RootReference { return location.reference }
func (location AliasLocation) RelativePath() string                { return location.relative }

func (location AliasLocation) Validate() error {
	if err := location.reference.Validate(); err != nil {
		return err
	}
	return hostconfig.ValidateRelativePath(location.relative)
}
