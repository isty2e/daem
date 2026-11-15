package mcpcodec

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPJSONLayoutEmptyParentAndDirectBirthAgree(t *testing.T) {
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject)
	canonical := mustCanonicalClaudeProjectMCPServerEntry(t, ClaudeProjectMCPServerProjection{
		ServerID: "selected", Command: "node", Args: []string{"new.js"},
		Env: map[string]string{"MODE": "${MODE}"}, AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	var first []byte
	for _, source := range []string{"{\n  \"outside\": 1\n}\n", "{\n  \"outside\": 1,\n  \"mcpServers\": {}\n}\n"} {
		got, err := operations.mergeCanonicalEntry([]byte(source), "selected", canonical)
		if err != nil {
			t.Fatal(err)
		}
		observed, present, err := operations.extractCanonicalEntry(got, "selected")
		if err != nil || !present || !bytes.Equal(observed, canonical) {
			t.Fatal("styled body changed the canonical selected server", err)
		}
		if first == nil {
			first = got
		} else if !bytes.Equal(first, got) {
			t.Fatalf("placeholder history changed server layout:\nbirth %s\nfilled %s", first, got)
		}
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(first, &root); err != nil {
		t.Fatal(err)
	}
	want := "{\n    \"selected\": {\n      \"args\": [\"new.js\"],\n      \"command\": \"node\",\n      \"env\": {\n        \"MODE\": \"${MODE}\"\n      },\n      \"type\": \"stdio\"\n    }\n  }"
	if string(root["mcpServers"]) != want || string(root["outside"]) != "1" {
		t.Fatalf("MCP producer ignored surrounding layout: %s", first)
	}
}

func TestMCPJSONLayoutUpdateKeepsNestedInlineStyle(t *testing.T) {
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject)
	source := []byte("{\n  \"outside\":1e+02,\n  \"mcpServers\": {\n    \"selected\": {\n      \"type\":\"stdio\",\n      \"command\":\"node\",\n      \"args\":[\"old.js\"],\n      \"env\": { \"MODE\": \"${MODE}\" }\n    }\n  }\n}\n")
	canonical := mustCanonicalClaudeProjectMCPServerEntry(t, ClaudeProjectMCPServerProjection{
		ServerID: "selected", Command: "node", Args: []string{"new.js"},
		Env: map[string]string{"MODE": "${MODE}"}, AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	got, err := operations.mergeCanonicalEntry(source, "selected", canonical)
	if err != nil {
		t.Fatal(err)
	}
	var root, servers, body map[string]json.RawMessage
	if err := json.Unmarshal(got, &root); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(root["mcpServers"], &servers); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(servers["selected"], &body); err != nil {
		t.Fatal(err)
	}
	if string(body["env"]) != `{ "MODE": "${MODE}" }` || string(body["args"]) != `["new.js"]` || string(root["outside"]) != "1e+02" {
		t.Fatalf("MCP update lost local style or outside bytes: %s", got)
	}
	stable, err := operations.mergeCanonicalEntry(got, "selected", canonical)
	if err != nil || !bytes.Equal(stable, got) {
		t.Fatal("same-valued MCP update did not stabilize", err)
	}
}
