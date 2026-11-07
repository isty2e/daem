package readiness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	"github.com/isty2e/daem/internal/output"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/target"
)

func TestNativeWholeProducerRetainsSameOwnerMCPPhysicalParticipant(t *testing.T) {
	fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{ServerID: "peer", Command: "npx", Args: []string{}, AdapterContract: aggregate.CodexProjectMCPStdioCommandV1})
	if err != nil {
		t.Fatal(err)
	}
	contract := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
		PlacementID: aggregate.MCPPlacementCodexProject, ServerID: "peer", LauncherCommand: "npx",
		CanonicalProjection: string(canonical),
	})
	item, present, err := contract.ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatal("MCP fixture has no canonical contribution", err)
	}
	row, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
	if err != nil {
		t.Fatal(err)
	}
	alias := row.Contribution().Contract().Address().Document()
	path := filepath.Join(namespacetest.LegacyRoot(t, fixture.observation.Roots, output.RootProject).SelectedPath, filepath.FromSlash(alias.AggregateRoot().RelativePath()))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fixture.hostPath, path); err != nil {
		t.Fatal(err)
	}
	fixture.observation.Aliases = append(fixture.observation.Aliases, alias)
	rows := append(fixture.observation.Before.ManagedAggregates(), row)
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: rows})
	if err != nil {
		t.Fatal(err)
	}
	nativeCodec := fixture.observation.Before.ManagedAggregates()[0].Contribution().CodecContractID()
	validators, err := aggregatecodec.ForContributions(t.Context(), []aggregate.CodecContractID{nativeCodec, row.Contribution().CodecContractID()}, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	fixture.codec, err = statefile.NewNativeCodec(validators)
	if err != nil {
		t.Fatal(err)
	}
	fixture.persistBefore(t, before, nil)
	plan, err := PrepareNativeWholeAdoption(t.Context(), fixture.adoption(t, true))
	if err != nil || plan.Validate() != nil || !plan.Decision().IsBlocked() || len(plan.RetiredSubjects()) != 0 {
		t.Fatal("whole producer adopted over a same-owner MCP participant", err)
	}
	projection, err := fixture.input.Documents.Evidence().Ownership(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := projection.Document(t.Context(), fixture.observation.Aliases[0])
	if err != nil || len(fragment.Aliases()) != 2 || len(fragment.BeforeSubjects()) != 2 || len(fragment.CurrentClaims()) != 0 {
		t.Fatal("physical alias projection lost logical members or claims", err)
	}
}
