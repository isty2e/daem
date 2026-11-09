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
	if err != nil || location.Validate() != nil || location.RootReference() != reference || location.RelativePath() != "retained/settings.json" {
		t.Fatal("alias location changed supplied canonical facts", location, err)
	}
	for _, relative := range []string{"", ".", "..", "../outside.json", "/outside.json"} {
		if location, err := configuration.NewAliasLocation(reference, relative); err == nil || location != (configuration.AliasLocation{}) {
			t.Fatal("non-child alias coordinate returned usable facts", relative, location, err)
		}
	}
	if location, err := configuration.NewAliasLocation(output.RootReference{}, "settings.json"); err == nil || location.Validate() == nil {
		t.Fatal("a child path supplied a missing root reference", location, err)
	}
}
