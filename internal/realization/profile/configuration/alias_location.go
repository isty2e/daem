package configuration

import (
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
)

// AliasLocation names a coordinate in an explicitly selected root. It carries
// no carrier/layout policy, selected filesystem facts or publication authority.
type AliasLocation struct {
	reference  output.RootReference
	coordinate RootCoordinate
}

func NewAliasLocation(reference output.RootReference, relative string) (AliasLocation, error) {
	coordinate, err := NewDirectoryChild(relative)
	if err != nil {
		return AliasLocation{}, err
	}
	return newAliasLocation(reference, coordinate)
}

func NewPointAliasLocation(reference output.RootReference) (AliasLocation, error) {
	return newAliasLocation(reference, PointSelf())
}

func newAliasLocation(reference output.RootReference, coordinate RootCoordinate) (AliasLocation, error) {
	location := AliasLocation{reference: reference, coordinate: coordinate}
	if err := location.Validate(); err != nil {
		return AliasLocation{}, err
	}
	return location, nil
}

func (location AliasLocation) RootReference() output.RootReference { return location.reference }
func (location AliasLocation) Coordinate() RootCoordinate          { return location.coordinate }

func (location AliasLocation) Validate() error {
	if err := location.reference.Validate(); err != nil {
		return err
	}
	if err := location.coordinate.Validate(); err != nil {
		return err
	}
	if child, directory := location.coordinate.DirectoryChild(); directory {
		return hostconfig.ValidateRelativePath(child)
	}
	return nil
}
