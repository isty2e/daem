package profile_test

import (
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestPiMCPProviderContributionAdmitsOnlyExactNPMProviderIdentity(t *testing.T) {
	for _, sourceRef := range []string{
		"npm:pi-mcp-adapter@^2.13.0",
		"npm:pi-mcp-adapter@2.15.0",
	} {
		carrier := piProviderProfileCarrier(t, target.ScopeProject, sourceRef)
		contribution, admitted, err := profile.PiMCPProviderContribution(carrier)
		if err != nil || !admitted {
			t.Fatalf("PiMCPProviderContribution(%q) = admitted=%t err=%v", sourceRef, admitted, err)
		}
		if contribution.Kind() != "mcp-client" ||
			contribution.Key() != "default" ||
			contribution.Provider().SubjectID() != carrier.SubjectID() {
			t.Fatalf("contribution = %#v, want mcp-client/default provided by exact carrier", contribution)
		}
	}

	for _, sourceRef := range []string{
		"npm:@fork/pi-mcp-adapter@2.15.0",
		"npm:pi-mcp-extension@1.5.0",
		"github:acme/other-package",
	} {
		carrier := piProviderProfileCarrier(t, target.ScopeProject, sourceRef)
		if _, admitted, err := profile.PiMCPProviderContribution(carrier); err != nil || admitted {
			t.Fatalf("PiMCPProviderContribution(%q) = admitted=%t err=%v, want unrelated", sourceRef, admitted, err)
		}
	}
}

func TestPiMCPProviderContributionRejectsLookalikeNonNPMSource(t *testing.T) {
	carrier := piProviderProfileCarrier(t, target.ScopeProject, "./pi-mcp-adapter")
	_, admitted, err := profile.PiMCPProviderContribution(carrier)
	if err == nil || admitted || !strings.Contains(err.Error(), "requires npm source identity") {
		t.Fatalf("PiMCPProviderContribution = admitted=%t err=%v", admitted, err)
	}
}

func piProviderProfileCarrier(
	t *testing.T,
	scope target.Scope,
	sourceRef string,
) extensiontopology.Carrier {
	t.Helper()
	source := desiredtest.ExtensionSource(t, desiredextension.SourceKindHostSource, sourceRef)
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		scope,
		source,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey returned error: %v", err)
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatalf("NewCarrier returned error: %v", err)
	}
	return carrier
}
