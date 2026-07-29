package refine_test

import (
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestSelectMCPProviderContributionUsesProjectFirstThenExplicitGlobalFallback(t *testing.T) {
	project := providerContribution(t, target.ScopeProject, "npm:pi-mcp-adapter@^2.13.0")
	global := providerContribution(t, target.ScopeGlobal, "npm:pi-mcp-adapter@2.15.0")
	binding := providerBinding(t, target.ScopeProject)

	selected, err := refine.SelectMCPProviderContribution(
		binding,
		[]extensiontopology.Contribution{global, project},
	)
	if err != nil {
		t.Fatalf("SelectMCPProviderContribution returned error: %v", err)
	}
	if selected.SubjectID() != project.SubjectID() {
		t.Fatalf("selected = %s, want project provider %s", selected.SubjectID(), project.SubjectID())
	}

	selected, err = refine.SelectMCPProviderContribution(
		binding,
		[]extensiontopology.Contribution{global},
	)
	if err != nil {
		t.Fatalf("SelectMCPProviderContribution(global fallback) returned error: %v", err)
	}
	if selected.SubjectID() != global.SubjectID() {
		t.Fatalf("selected = %s, want global provider %s", selected.SubjectID(), global.SubjectID())
	}
}

func TestSelectMCPProviderContributionRequiresGlobalForGlobalBinding(t *testing.T) {
	project := providerContribution(t, target.ScopeProject, "npm:pi-mcp-adapter@^2.13.0")
	global := providerContribution(t, target.ScopeGlobal, "npm:pi-mcp-adapter@^2.13.0")
	binding := providerBinding(t, target.ScopeGlobal)

	selected, err := refine.SelectMCPProviderContribution(
		binding,
		[]extensiontopology.Contribution{project, global},
	)
	if err != nil || selected.SubjectID() != global.SubjectID() {
		t.Fatalf("SelectMCPProviderContribution = %s, %v; want global", selected.SubjectID(), err)
	}
	if _, err := refine.SelectMCPProviderContribution(
		binding,
		[]extensiontopology.Contribution{project},
	); err == nil || !strings.Contains(err.Error(), "cannot select a project-scoped provider") {
		t.Fatalf("project-only global selection error = %v", err)
	}
}

func TestSelectMCPProviderContributionRejectsMissingDuplicateAndAmbiguousCandidates(t *testing.T) {
	binding := providerBinding(t, target.ScopeProject)
	first := providerContribution(t, target.ScopeProject, "npm:pi-mcp-adapter@^2.13.0")
	second := providerContribution(t, target.ScopeProject, "npm:pi-mcp-adapter@2.15.0")

	if _, err := refine.SelectMCPProviderContribution(binding, nil); err == nil ||
		!strings.Contains(err.Error(), "requires one explicit compatible provider") {
		t.Fatalf("missing provider error = %v", err)
	}
	if _, err := refine.SelectMCPProviderContribution(
		binding,
		[]extensiontopology.Contribution{first, first},
	); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate provider error = %v", err)
	}
	if _, err := refine.SelectMCPProviderContribution(
		binding,
		[]extensiontopology.Contribution{second, first},
	); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous provider error = %v", err)
	}
}

func providerBinding(t *testing.T, scope target.Scope) desiredmcp.Binding {
	t.Helper()
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "npx"),
		[]string{"-y", "@upstash/context7-mcp"},
		nil,
	)
	return desiredtest.MCPBinding(
		t,
		target.TargetPi,
		scope,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
}

func providerContribution(
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
