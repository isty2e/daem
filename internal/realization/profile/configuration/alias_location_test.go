package configuration_test

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func TestAliasLocationRetainsOnlyCanonicalRootAndDirectoryChildFacts(t *testing.T) {
	reference, err := output.NewRootReference("pi-agent", "retained-location-v1")
	if err != nil {
		t.Fatal(err)
	}
	location, err := configuration.NewAliasLocation(reference, "retained/settings.json")
	child, directory := location.Coordinate().DirectoryChild()
	if err != nil || location.Validate() != nil || location.RootReference() != reference || !directory || child != "retained/settings.json" {
		t.Fatal("alias location changed supplied canonical facts", location, err)
	}
	for _, relative := range []string{"", ".", "..", "../outside.json", "/outside.json", " child.json"} {
		if location, err := configuration.NewAliasLocation(reference, relative); err == nil || location != (configuration.AliasLocation{}) {
			t.Fatal("non-child alias coordinate returned usable facts", relative, location, err)
		}
	}
	if location, err := configuration.NewAliasLocation(output.RootReference{}, "settings.json"); err == nil || location.Validate() == nil {
		t.Fatal("a child path supplied a missing root reference", location, err)
	}
	point, err := configuration.NewPointAliasLocation(reference)
	if err != nil || point.Validate() != nil || point.RootReference() != reference || point.Coordinate() != configuration.PointSelf() || point == location {
		t.Fatal("point-self location lost its explicit coordinate", point, err)
	}
	if child, directory := point.Coordinate().DirectoryChild(); directory || child != "" {
		t.Fatal("point-self became a child of its witness directory", child)
	}
	if point, err := configuration.NewPointAliasLocation(output.RootReference{}); err == nil || point != (configuration.AliasLocation{}) {
		t.Fatal("point-self supplied a missing root reference", point, err)
	}
	if (configuration.RootCoordinate{}).Validate() == nil {
		t.Fatal("zero coordinate acquired a root kind")
	}
	physical, err := configuration.NewDirectoryChild(" child.json")
	if child, directory := physical.DirectoryChild(); err != nil || !directory || child != " child.json" {
		t.Fatal("structural coordinate imposed declaration spelling rules", physical, err)
	}
}
