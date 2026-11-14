package compose_test

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestCompositionDoesNotRewriteUnselectedValuesInsideEnvelope(t *testing.T) {
	codec, err := nativecodec.New(t.Context(), "native-json-v1", limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	foreign := `"foreign": ["literal",1e+03,{"marker":true}]`
	original := aggregate.ExistingDocument([]byte(`{"container":{"selected":false,` + foreign + `}}`))
	key := path(t, "container", "selected")
	set := nativeSet(t, codec, target.ScopeProject, ".codex/extension.data", key, "true")
	before := observe(t, codec, original, selectSets(t, set))
	plan := desiredPlan(t, before, map[output.KeyPath]*aggregate.ContributionSet{key: &set})
	change, err := compose.NewProjectionEdit(codec, plan, configdocument.JSON, []output.KeyPath{path(t, "container")})
	if err != nil {
		t.Fatal(err)
	}
	result, err := compose.Render(t.Context(), original, limits, []compose.ProjectionEdit{change})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := compose.NewProjectionBaseline(codec, before, configdocument.JSON, []output.KeyPath{path(t, "container")})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := compose.Restore(t.Context(), result.Document(), limits, []compose.ProjectionBaseline{baseline})
	if err != nil {
		t.Fatal(err)
	}
	for phase, document := range map[string]aggregate.Document{"forward": result.Document(), "inverse": restored.Document()} {
		if !strings.Contains(string(document.Content()), foreign) {
			t.Fatalf("%s rewrote a non-selected value inside the rewrite envelope: %s", phase, document.Content())
		}
	}
	if !observe(t, codec, restored.Document(), selectSets(t, set)).Equal(before) {
		t.Fatal("projection inverse did not restore its selected baseline")
	}
}

func TestMCPCompositionPreservesForeignEntriesInsideParentEnvelope(t *testing.T) {
	placement, found := aggregate.MCPPlacementForID(aggregate.MCPPlacementPiProject)
	if !found {
		t.Fatal("Pi MCP fixture placement is unavailable")
	}
	pi, found := mcpcodec.For(placement.CodecContractID())
	if !found {
		t.Fatal("Pi MCP fixture codec is unavailable")
	}
	canonical, err := mcpcodec.CanonicalPiMCPAdapterServerEntry(mcpcodec.PiMCPAdapterServerProjection{
		ServerID: "selected", Command: "node", AdapterContract: aggregate.PiMCPAdapterStdioV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := placement.Contribution("selected", string(canonical))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), "selected")
	if err != nil {
		t.Fatal(err)
	}
	item, err := aggregate.NewSubjectContribution(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	piSet, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	codex, codexSet := mcpSet(t, "selected")
	for _, test := range []struct {
		name     string
		codec    aggregate.Codec
		set      aggregate.ContributionSet
		syntax   configdocument.Syntax
		area     string
		original string
		foreign  string
	}{
		{
			"Pi", pi, piSet, configdocument.JSON, "mcpServers",
			`{"mcpServers":{"manual": {"future":[1e+03,"literal"]}}}`, `"manual": {"future":[1e+03,"literal"]}`,
		},
		{
			"Codex", codex, codexSet, configdocument.TOML, "mcp_servers",
			"[mcp_servers.manual]\ncommand = 'keep'\nargs = ['literal']\n", "command = 'keep'\nargs = ['literal']",
		},
		{
			"Codex inline", codex, codexSet, configdocument.TOML, "mcp_servers",
			"mcp_servers = { manual = { command = 'keep', args = ['literal'] } }\n", "manual = { command = 'keep', args = ['literal'] }",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := aggregate.ExistingDocument([]byte(test.original))
			before := observe(t, test.codec, original, selectSets(t, test.set))
			plan := desiredPlan(t, before, map[output.KeyPath]*aggregate.ContributionSet{test.set.Address().KeyPath(): &test.set})
			areas := []output.KeyPath{path(t, test.area)}
			change, err := compose.NewProjectionEdit(test.codec, plan, test.syntax, areas)
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := compose.Render(t.Context(), original, limits, []compose.ProjectionEdit{change})
			if err != nil {
				t.Fatal(err)
			}
			baseline, err := compose.NewProjectionBaseline(test.codec, before, test.syntax, areas)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := compose.Restore(t.Context(), candidate.Document(), limits, []compose.ProjectionBaseline{baseline})
			if err != nil {
				t.Fatal(err)
			}
			for _, document := range []aggregate.Document{candidate.Document(), restored.Document()} {
				if !strings.Contains(string(document.Content()), test.foreign) {
					t.Fatal("MCP composition rewrote an unselected native entry")
				}
			}
		})
	}
}
