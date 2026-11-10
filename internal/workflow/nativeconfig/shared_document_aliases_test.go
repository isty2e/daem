package nativeconfig

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func TestOrdinaryAliasCandidatesRetainHistoryWithoutIntroducingUnselectedDesiredWork(t *testing.T) {
	environment, err := declarationmanifest.Decode([]byte("version=1\ntargets=['codex']\n[[mcp_server]]\nname='search'\nscope='global'\ntransport='stdio'\ncommand='node'\n"))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := lockrefine.MCPSubjects(environment.MCPServers(), nil, mcpcodec.CanonicalMCPBindingContribution)
	if err != nil || len(contracts) != 1 {
		t.Fatalf("lower MCP contract: %v (%d contracts)", err, len(contracts))
	}
	locked, err := lock.NewLockedSection(contracts, nil)
	if err != nil {
		t.Fatal(err)
	}
	item, present, err := contracts[0].ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatalf("MCP contribution unavailable: %v", err)
	}
	state, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
	if err != nil {
		t.Fatal(err)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: []durable.ManagedAggregateState{state}})
	if err != nil {
		t.Fatal(err)
	}
	nativeOnly, err := environment.HostConfigsOnly()
	if err != nil {
		t.Fatal(err)
	}
	selection, err := targetselection.ForDiagnostics(nil)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("@codex-home/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := aggregate.NewDocumentAddress(target.TargetCodex, target.ScopeGlobal, destination)
	if err != nil {
		t.Fatal(err)
	}
	want := item.Contribution().Contract().Address().Document()
	for _, scenario := range []struct {
		name        string
		environment desired.Environment
		before      durable.Snapshot
		count       int
	}{
		{name: "new selected contribution", environment: environment, count: 1},
		{name: "unselected locked contribution", environment: nativeOnly},
		{name: "established history in native-only observation", environment: nativeOnly, before: before, count: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			candidates, err := ordinaryDocumentAliasCandidates(readiness.Input{
				Environment: scenario.environment, Lockfile: lock.File{Locked: locked}, Selection: selection,
			}, scenario.before, []aggregate.DocumentAddress{selected})
			if err != nil {
				t.Fatal(err)
			}
			if len(candidates) != scenario.count || (len(candidates) == 1 && candidates[0] != want) {
				t.Fatalf("candidate source selection = %v, wanted %d matching MCP aliases", candidates, scenario.count)
			}
		})
	}
}
