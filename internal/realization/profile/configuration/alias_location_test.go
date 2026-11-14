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

func TestNativeAliasLocationMatchesItsLocatorCoordinateKind(t *testing.T) {
	for _, identity := range []string{"claude-account", "antigravity-shared"} {
		point, err := output.NewRootReference(identity, identity+"-point-v2")
		if err != nil {
			t.Fatal(err)
		}
		directory, err := output.NewRootReference(identity, identity+"-location-v1")
		if err != nil {
			t.Fatal(err)
		}
		if location, err := configuration.NewPointAliasLocation(point); err != nil || location.Validate() != nil {
			t.Fatal("point locator refused its self coordinate", location, err)
		}
		if location, err := configuration.NewAliasLocation(directory, "endpoint.json"); err != nil || location.Validate() != nil {
			t.Fatal("directory anchor refused its child coordinate", location, err)
		}
		if location, err := configuration.NewAliasLocation(point, "sibling.json"); err == nil || location != (configuration.AliasLocation{}) {
			t.Fatal("point locator acquired a directory-child coordinate", location, err)
		}
		if location, err := configuration.NewPointAliasLocation(directory); err == nil || location != (configuration.AliasLocation{}) {
			t.Fatal("directory locator acquired a point-self coordinate", location, err)
		}
	}
}
