package configuration_test

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func TestSelectedAliasRootsHaveExplicitLocatorVersionAdmission(t *testing.T) {
	contracts, err := configuration.NativeRootContracts()
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range contracts {
		err := configuration.AdmitNativeAliasRootReference(contract.Reference())
		if contract.SupportsAliases() && err != nil {
			t.Fatal("current alias placement has no known locator version", contract.Reference(), err)
		}
		foreign, err := output.NewRootReference("unrelated-root", contract.Reference().LocatorPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if err := configuration.AdmitNativeAliasRootReference(foreign); err == nil {
			t.Fatal("a recognized policy name admitted another root identity")
		}
	}
}

func TestNonAliasV1ContractsDoNotBecomeGlobalAliasLocators(t *testing.T) {
	for _, test := range []struct{ identity, policy string }{
		{"opencode-additional-configuration", "opencode-additional-location-v1"},
		{"opencode-additional-sources", "opencode-source-location-v1"},
		{"opencode-legacy-configuration", "opencode-legacy-location-v1"},
		{"project", "selected-project-v1"},
	} {
		reference, err := output.NewRootReference(test.identity, test.policy)
		if err != nil {
			t.Fatal(err)
		}
		if err := configuration.AdmitNativeAliasRootReference(reference); err == nil {
			t.Fatal("a non-alias locator acquired alias admission", reference)
		}
	}
}
