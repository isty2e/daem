package mcpcodec

import (
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestPiNativeCodecCanonicalBytesAndAdapterSeparation(t *testing.T) {
	canonical, err := canonicalPiNativeServerEntry("context7", "node", []string{"server.js", "--flag"}, map[string]string{"TOKEN": "${SOURCE_TOKEN}"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "command": "node",
  "args": [
    "server.js",
    "--flag"
  ],
  "env": {
    "TOKEN": "${SOURCE_TOKEN}"
  },
  "enabled": true,
  "exposure": "codemode"
}
`
	if string(canonical) != want {
		t.Fatalf("canonical = %s, want %s", canonical, want)
	}
	for _, raw := range []string{
		`{"command":"node","lifecycle":"lazy"}`,
		`{"command":"node","disabled":true}`,
		`{"command":"node","enabled":false}`,
		`{"command":"node","cwd":"/tmp"}`,
		`{"command":"node","env":{"TOKEN":"literal-secret"}}`,
		`{"command":"node","toolExposure":{"x":"direct"}}`,
	} {
		if _, err := decodePiNativeServerEntry([]byte(raw), "context7"); err == nil {
			t.Fatalf("unsupported native entry accepted: %s", raw)
		}
	}
}

func TestPiNativeCodecRejectsJSONCAndNamespaceCollision(t *testing.T) {
	operations, ok := ImplementedMCPPlacementOperationsForContract(aggregate.MCPPlacementPiProject, aggregate.MCPCodecPiNativeStdio)
	if !ok {
		t.Fatal("native operations unavailable")
	}
	for _, content := range []string{
		`{// comment
"mcpServers":{"context7":{"command":"node"}}}`,
		`{"mcpServers":{"context7":{"command":"node"},"context7":{"command":"node"}}}`,
		`{"mcpServers":{"foo-bar":{"command":"node"},"foo_bar":{"command":"node"}}}`,
	} {
		if _, err := operations.observeCanonical([]byte(content), []string{"context7"}); err == nil {
			t.Fatalf("ambiguous native document accepted: %s", content)
		}
	}
	if _, err := canonicalPiNativeServerEntry("bad.name", "node", nil, nil); err == nil {
		t.Fatal("invalid upstream name accepted")
	}
	observed, err := operations.observeCanonical([]byte(`{"servers":{"context7":{"command":"echo"}}}`), []string{"context7"})
	_, present, entryErr := observed.CanonicalEntry("context7")
	if err != nil || entryErr != nil || present {
		t.Fatalf("adapter alias became native: %#v, %v", observed, err)
	}
}

func TestPiNativeCodecStoredContractRemovalAndRestorePreserveSiblings(t *testing.T) {
	operations, ok := ImplementedMCPPlacementOperationsForContract(aggregate.MCPPlacementPiProject, aggregate.MCPCodecPiNativeStdio)
	if !ok {
		t.Fatal("native operations unavailable")
	}
	canonical, err := canonicalPiNativeServerEntry("context7", "node", []string{"server.js"}, map[string]string{"TOKEN": "${SOURCE_TOKEN}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operations.mergeCanonicalEntry(nil, "context7", canonical); err != nil {
		t.Fatalf("native canonical merge: %v", err)
	}
	contribution := mcpCodecContribution(t, operations.Placement(), "context7", canonical)
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := For(aggregate.MCPCodecPiNativeStdio)
	desired := mcpCodecExclusiveSet(t, contribution)
	if err := codec.ValidateContributions(desired); err != nil {
		t.Fatal(err)
	}
	initial := aggregate.ExistingDocument([]byte(`{"unmanaged":true,"mcpServers":{"sibling":{"command":"echo","env":{"TOKEN":"host-only"}}}}`))
	before, failure := codec.Read(initial, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	intent, err := aggregate.NewProjectionIntent(before.States()[0], &desired)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	written, failure := codec.Render(initial, plan)
	if failure != nil {
		t.Fatal(failure)
	}
	current, failure := codec.Read(written.Document(), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	remove, err := aggregate.NewProjectionIntent(current.States()[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	removePlan, err := aggregate.NewPlan(current, []aggregate.ProjectionIntent{remove})
	if err != nil {
		t.Fatal(err)
	}
	removed, failure := codec.Render(written.Document(), removePlan)
	if failure != nil {
		t.Fatal(failure)
	}
	restored, failure := codec.Restore(written.Document(), before)
	if failure != nil {
		t.Fatal(failure)
	}
	for _, document := range []aggregate.Document{removed.Document(), restored.Document()} {
		var fields struct {
			Unmanaged bool                       `json:"unmanaged"`
			Servers   map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal(document.Content(), &fields); err != nil {
			t.Fatal(err)
		}
		if !fields.Unmanaged || len(fields.Servers) != 1 || string(fields.Servers["sibling"]) == "" {
			t.Fatalf("contribution-local removal changed siblings: %s", document.Content())
		}
	}
}
