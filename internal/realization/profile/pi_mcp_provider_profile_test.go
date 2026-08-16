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
		"npm:pi-mcp-adapter@^2.99.0",
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

func TestPiMCPProviderContributionRejectsUnboundedOrOutOfProfileSelectors(t *testing.T) {
	for _, sourceRef := range []string{
		"npm:pi-mcp-adapter",
		"npm:pi-mcp-adapter@latest",
		"npm:pi-mcp-adapter@*",
		"npm:pi-mcp-adapter@^2.12.9",
		"npm:pi-mcp-adapter@2.13.0-beta.1",
		"npm:pi-mcp-adapter@^3.0.0",
		"npm:pi-mcp-adapter@~2.15.0",
	} {
		carrier := piProviderProfileCarrier(t, target.ScopeProject, sourceRef)
		if _, admitted, err := profile.PiMCPProviderContribution(carrier); err == nil || admitted {
			t.Fatalf(
				"PiMCPProviderContribution(%q) = admitted=%t err=%v, want rejection",
				sourceRef,
				admitted,
				err,
			)
		}
	}
}

func TestPiMCPProviderContributionRejectsCredentialBearingDurableSource(t *testing.T) {
	carrier := piProviderProfileCarrier(
		t,
		target.ScopeProject,
		"npm:pi-mcp-adapter@token = actual-secret",
	)
	_, admitted, err := profile.PiMCPProviderContribution(carrier)
	if err == nil || admitted {
		t.Fatalf("PiMCPProviderContribution = admitted:%t error:%v, want authority rejection", admitted, err)
	}
	if strings.Contains(err.Error(), "actual-secret") {
		t.Fatalf("PiMCPProviderContribution exposed source credential: %q", err)
	}
}

func TestMCPProviderCodecForCurrentVersionMapsStableCompatibleFamily(t *testing.T) {
	carrier := piProviderProfileCarrier(
		t,
		target.ScopeProject,
		"npm:pi-mcp-adapter@^2.13.0",
	)
	contribution, admitted, err := profile.PiMCPProviderContribution(carrier)
	if err != nil || !admitted {
		t.Fatalf("PiMCPProviderContribution = admitted=%t err=%v", admitted, err)
	}
	for _, version := range []string{"2.13.0", "2.15.0", "2.99.7"} {
		codec, err := profile.MCPProviderCodecForCurrentVersion(
			target.TargetPi,
			carrier,
			contribution.Reference(),
			version,
		)
		if err != nil || codec != "pi-mcp-adapter-stdio-v1" {
			t.Fatalf("version %q codec = %q err=%v", version, codec, err)
		}
	}
	for _, version := range []string{
		"2.12.9",
		"2.15.0-beta.1",
		"3.0.0",
		"v2.15.0",
		"2.15",
	} {
		if _, err := profile.MCPProviderCodecForCurrentVersion(
			target.TargetPi,
			carrier,
			contribution.Reference(),
			version,
		); err == nil {
			t.Fatalf("version %q unexpectedly mapped", version)
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
