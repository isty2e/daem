package mcpcodec

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestPiMCPPlacementsShareCodecAndKeepDistinctDocuments(t *testing.T) {
	project, projectOK := ImplementedMCPPlacementOperationsForPlacement(
		aggregate.MCPPlacementPiProject,
	)
	global, globalOK := ImplementedMCPPlacementOperationsForPlacement(
		aggregate.MCPPlacementPiGlobal,
	)
	if !projectOK || !globalOK {
		t.Fatalf("Pi operations present = project:%t global:%t", projectOK, globalOK)
	}
	if project.Placement().CodecContractID() != aggregate.MCPCodecPiAdapterStdio ||
		global.Placement().CodecContractID() != aggregate.MCPCodecPiAdapterStdio {
		t.Fatalf(
			"Pi codec contracts = %q/%q, want shared %q",
			project.Placement().CodecContractID(),
			global.Placement().CodecContractID(),
			aggregate.MCPCodecPiAdapterStdio,
		)
	}
	if project.Placement().ConfigPath() == global.Placement().ConfigPath() {
		t.Fatalf("Pi project/global placements share config path %q", project.Placement().ConfigPath())
	}
	if _, ok := For(aggregate.MCPCodecPiAdapterStdio); !ok {
		t.Fatal("shared Pi MCP codec is not registered")
	}
}

func TestPiMCPAdapterCanonicalEntryOwnsSafetyConstants(t *testing.T) {
	canonical, err := CanonicalPiMCPAdapterServerEntry(PiMCPAdapterServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            nil,
		Env:             map[string]string{"API_TOKEN": "${CONTEXT7_API_TOKEN}"},
		AdapterContract: aggregate.PiMCPAdapterStdioV1,
	})
	if err != nil {
		t.Fatalf("CanonicalPiMCPAdapterServerEntry returned error: %v", err)
	}
	for _, expected := range []string{
		`"command": "npx"`,
		`"args": []`,
		`"API_TOKEN": "${CONTEXT7_API_TOKEN}"`,
		`"lifecycle": "lazy"`,
		`"disabled": false`,
	} {
		if !strings.Contains(string(canonical), expected) {
			t.Fatalf("canonical entry = %s, want %q", canonical, expected)
		}
	}
}

func TestPiMCPAdapterDefaultedEntryNormalizesToCanonicalProfile(t *testing.T) {
	operations := mustPiMCPPlacementOperations(t, aggregate.MCPPlacementPiProject)
	document := []byte(`{"mcpServers":{"context7":{"command":"npx"}}}`)

	canonical, present, err := operations.ExtractCanonicalEntry(document, "context7")
	if err != nil {
		t.Fatalf("ExtractCanonicalEntry returned error: %v", err)
	}
	if !present {
		t.Fatal("defaulted Pi entry is absent")
	}
	for _, expected := range []string{
		`"args": []`,
		`"lifecycle": "lazy"`,
		`"disabled": false`,
	} {
		if !strings.Contains(string(canonical), expected) {
			t.Fatalf("canonical entry = %s, want %q", canonical, expected)
		}
	}
}

func TestPiMCPAdapterMutationPreservesUnownedDocumentContent(t *testing.T) {
	operations := mustPiMCPPlacementOperations(t, aggregate.MCPPlacementPiGlobal)
	existing := []byte(`{
	  "settings": {"hostConfigDiscovery": false, "token": "SECRET_CANARY"},
	  "imports": ["./shared.json"],
	  "mcpServers": {
	    "sibling": {"url": "https://example.invalid/mcp", "auth": "SECRET_CANARY"}
	  }
	}`)
	canonical := mustPiMCPAdapterCanonical(t, "context7")

	merged, err := operations.MergeCanonicalEntry(existing, "context7", canonical)
	if err != nil {
		t.Fatalf("MergeCanonicalEntry returned error: %v", err)
	}
	for _, expected := range []string{
		`"settings": {`,
		`"hostConfigDiscovery": false`,
		`"imports": [`,
		`"sibling": {`,
		`"SECRET_CANARY"`,
		`"context7": {`,
		`"lifecycle": "lazy"`,
	} {
		if !strings.Contains(string(merged), expected) {
			t.Fatalf("merged document = %s, want %q", merged, expected)
		}
	}

	removed, err := operations.RemoveProjection(merged, "context7")
	if err != nil {
		t.Fatalf("RemoveProjection returned error: %v", err)
	}
	if strings.Contains(string(removed), `"context7"`) ||
		!containsAll(string(removed), `"settings": {`, `"imports": [`, `"sibling": {`, `"SECRET_CANARY"`) {
		t.Fatalf("removed document did not preserve only unowned content: %s", removed)
	}
}

func TestPiMCPAdapterRejectsLossyDocumentsAliasesAndUnsupportedEntries(t *testing.T) {
	operations := mustPiMCPPlacementOperations(t, aggregate.MCPPlacementPiProject)
	cases := []struct {
		name  string
		input []byte
		want  MCPProjectionReasonCode
	}{
		{
			name:  "commented json",
			input: []byte("{\n// provider comment\n\"mcpServers\": {}\n}"),
			want:  MCPProjectionReasonProviderDocumentLossy,
		},
		{
			name:  "trailing comma",
			input: []byte(`{"mcpServers": {},}`),
			want:  MCPProjectionReasonProviderDocumentLossy,
		},
		{
			name:  "provider alias",
			input: []byte(`{"mcp-servers": {}}`),
			want:  MCPProjectionReasonUnsupportedManagedField,
		},
		{
			name:  "both aliases",
			input: []byte(`{"mcpServers": {}, "mcp-servers": {}}`),
			want:  MCPProjectionReasonUnsupportedManagedField,
		},
		{
			name:  "eager lifecycle",
			input: []byte(`{"mcpServers":{"context7":{"command":"npx","lifecycle":"eager"}}}`),
			want:  MCPProjectionReasonUnsupportedManagedField,
		},
		{
			name:  "disabled entry",
			input: []byte(`{"mcpServers":{"context7":{"command":"npx","disabled":true}}}`),
			want:  MCPProjectionReasonUnsupportedManagedField,
		},
		{
			name:  "literal env",
			input: []byte(`{"mcpServers":{"context7":{"command":"npx","env":{"TOKEN":"SECRET_CANARY"}}}}`),
			want:  MCPProjectionReasonSecretLiteralForbidden,
		},
		{
			name:  "compound env",
			input: []byte(`{"mcpServers":{"context7":{"command":"npx","env":{"TOKEN":"Bearer ${TOKEN}"}}}}`),
			want:  MCPProjectionReasonSecretLiteralForbidden,
		},
		{
			name:  "unsupported url",
			input: []byte(`{"mcpServers":{"context7":{"command":"npx","url":"https://example.invalid"}}}`),
			want:  MCPProjectionReasonUnsupportedManagedField,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := operations.ExtractCanonicalEntry(tc.input, "context7")
			assertMCPProjectionReason(t, err, tc.want)
			if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("error leaked secret canary: %q", err)
			}
		})
	}
}

func TestPiMCPAdapterDoesNotMisclassifyWrongShapeJSONCAsLosslessProviderDocument(t *testing.T) {
	operations := mustPiMCPPlacementOperations(t, aggregate.MCPPlacementPiProject)
	for _, input := range [][]byte{
		[]byte("[\n// comment\n]\n"),
		[]byte("\"scalar\" // comment\n"),
	} {
		_, _, err := operations.ExtractCanonicalEntry(input, "context7")
		if err == nil {
			t.Fatalf("ExtractCanonicalEntry(%q) returned nil error", input)
		}
		var projectionError *MCPProjectionError
		if errors.As(err, &projectionError) &&
			projectionError.Code() == MCPProjectionReasonProviderDocumentLossy {
			t.Fatalf("ExtractCanonicalEntry(%q) misclassified shape error as provider-valid JSONC", input)
		}
	}
}

func mustPiMCPPlacementOperations(
	t *testing.T,
	placementID aggregate.MCPPlacementID,
) MCPPlacementOperations {
	t.Helper()
	operations, ok := ImplementedMCPPlacementOperationsForPlacement(placementID)
	if !ok {
		t.Fatalf("Pi placement operations %q are missing", placementID)
	}
	return operations
}

func mustPiMCPAdapterCanonical(t *testing.T, serverID string) []byte {
	t.Helper()
	canonical, err := CanonicalPiMCPAdapterServerEntry(PiMCPAdapterServerProjection{
		ServerID:        serverID,
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		Env:             map[string]string{"API_TOKEN": "${CONTEXT7_API_TOKEN}"},
		AdapterContract: aggregate.PiMCPAdapterStdioV1,
	})
	if err != nil {
		t.Fatalf("CanonicalPiMCPAdapterServerEntry returned error: %v", err)
	}
	return canonical
}
