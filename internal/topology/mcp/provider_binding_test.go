package mcp_test

import (
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestBindingWithProviderRecordsExactProviderAndBindingEdges(t *testing.T) {
	server := ambientServer(t, "context7", target.TargetPi, target.ScopeProject, "npx", nil, nil)
	binding := onlyBinding(t, server)
	contribution := piMCPProviderContribution(t, target.ScopeProject, "npm:pi-mcp-adapter@^2.13.0")
	selection, err := topologymcp.NewProviderSelection(
		contribution.Provider().SubjectID(),
		contribution.SubjectID(),
	)
	if err != nil {
		t.Fatalf("NewProviderSelection returned error: %v", err)
	}
	projection := projectionSubject(t, server, binding)
	graph, err := topologymcp.ServersWithProviderSelections(
		[]desiredmcp.Server{server},
		map[topology.SubjectID]topologymcp.ProviderSelection{projection: selection},
	)
	if err != nil {
		t.Fatalf("ServersWithProviderSelections returned error: %v", err)
	}
	provider, ok := graph.ProviderOf(contribution.SubjectID())
	if !ok || provider != contribution.Provider().SubjectID() {
		t.Fatalf("ProviderOf = %s, %t; want %s", provider, ok, contribution.Provider().SubjectID())
	}
	bound := graph.BoundTargetsOf(contribution.SubjectID())
	if len(bound) != 1 || bound[0] != projection {
		t.Fatalf("BoundTargetsOf = %v, want [%s]", bound, projection)
	}
}

func piMCPProviderContribution(
	t *testing.T,
	scope target.Scope,
	sourceRef string,
) extensiontopology.Contribution {
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
	contribution, err := extensiontopology.NewContribution(
		carrier,
		extensiontopology.ContributionSpec{Kind: "mcp-client", Key: "default"},
	)
	if err != nil {
		t.Fatalf("NewContribution returned error: %v", err)
	}
	return contribution
}
