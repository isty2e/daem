package host

import (
	"path/filepath"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/output/hostpath"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
)

func TestPiMCPContextKeepsCurrentAdapterSeparateFromRetiringNative(t *testing.T) {
	workDir := t.TempDir()
	agentRoot := filepath.Join(workDir, "agent")
	t.Setenv("HOME", filepath.Join(workDir, "home"))
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	t.Setenv("PATH", t.TempDir())

	provider := desiredtest.Extension(t, desiredextension.Spec{
		Name: "provider", Carrier: desiredextension.CarrierPiPackage,
		Target: target.TargetPi, Scope: target.ScopeProject,
		Source: desiredtest.ExtensionSource(t, desiredextension.SourceKindHostSource, "npm:pi-mcp-adapter@^2.13.0"),
	})
	transport := desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, "node"), nil, nil)
	binding := desiredtest.MCPBinding(t, target.TargetPi, target.ScopeProject, transport, desiredmcp.OnAbsentRemoveBinding)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{Name: "context7", Bindings: []desiredmcp.Binding{binding}})
	current, err := lockrefine.Extensions([]desiredextension.Extension{provider})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := lockrefine.MCPSubjects([]desiredmcp.Server{server}, []desiredextension.Extension{provider}, mcpcodec.CanonicalMCPBindingContribution)
	if err != nil {
		t.Fatal(err)
	}
	section, err := lock.NewLockedSection(append(current, adapter...), nil)
	if err != nil {
		t.Fatal(err)
	}
	retiring := piNativeProjection(t, target.ScopeGlobal)
	resolver := hostpath.NewResolver(workDir).WithDestinationOverride(pihostpath.DestinationOverride(workDir))

	observed, err := Observe(Input{
		Context: t.Context(), Contracts: section.Subjects(), Codecs: aggregatecodec.Catalog(),
		WorkDir: workDir, ResolveDestination: resolver.Resolve,
		Previous: []aggregate.SubjectContribution{retiring}, Retiring: []aggregate.SubjectContribution{retiring},
	})
	if err != nil || len(observed.Current) != 1 || len(observed.Retiring) != 1 {
		t.Fatalf("separate current/retiring observation = %#v, %v", observed, err)
	}
	if observed.Current[0].Subject() != adapter[0].SubjectID() || observed.Retiring[0].Subject() != retiring.SubjectID() {
		t.Fatal("current and retiring recorded contracts were reinterpreted")
	}
}
