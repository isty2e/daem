package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestAntigravityGlobalMCPProjectionFactsAndCanonicalEntry(t *testing.T) {
	projection := validAntigravityMCPProjection("context7")
	projection.Args = nil
	projection.EnvironmentNames = []string{"TOKEN_Z", "TOKEN_A", "TOKEN_Z"}

	entry, err := CanonicalAntigravityGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalAntigravityGlobalMCPServerEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"command": "npx"`,
		`"args": []`,
	} {
		if !strings.Contains(string(entry), want) {
			t.Fatalf("canonical entry = %s, want %q", entry, want)
		}
	}
	for _, forbidden := range []string{`"type"`, `"env"`} {
		if strings.Contains(string(entry), forbidden) {
			t.Fatalf("canonical entry = %s, did not want %q", entry, forbidden)
		}
	}
	if aggregate.AntigravityGlobalMCPConfigPath != "~/.gemini/config/mcp_config.json" {
		t.Fatalf("config path = %q", aggregate.AntigravityGlobalMCPConfigPath)
	}
	if got := AntigravityGlobalMCPContentPath("context7"); got != "/mcpServers/context7" {
		t.Fatalf("content path = %q", got)
	}

	withoutEnvironmentNames := projection
	withoutEnvironmentNames.EnvironmentNames = nil
	withoutEnvironment, err := CanonicalAntigravityGlobalMCPServerEntry(withoutEnvironmentNames)
	if err != nil {
		t.Fatalf("canonicalize projection without environment names: %v", err)
	}
	if string(withoutEnvironment) != string(entry) {
		t.Fatalf(
			"native entries differ by ambient prerequisites:\nwith names: %s\nwithout:    %s",
			entry,
			withoutEnvironment,
		)
	}
}

func TestAntigravityGlobalMCPProjectionMergeCompareAndPreserveSiblings(t *testing.T) {
	existing := []byte(`{
	  "theme": "dark",
	  "mcpServers": {
	    "sibling": {"serverUrl":"https://example.invalid/mcp","headers":{"Authorization":"Bearer secret"}}
	  }
	}`)
	projection := validAntigravityMCPProjection("context7")
	projection.Args = []string{"-y", "@upstash/context7-mcp"}

	merged, err := mergeAntigravityGlobalMCPServerProjectionForTest(existing, projection)
	if err != nil {
		t.Fatalf("MergeAntigravityGlobalMCPServerProjection returned error: %v", err)
	}
	for _, want := range []string{
		`"theme": "dark"`,
		`"sibling": {`,
		`"serverUrl": "https://example.invalid/mcp"`,
		`"context7": {`,
		`"command": "npx"`,
		`"args": [`,
	} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("merged = %s, want %q", merged, want)
		}
	}

	comparison, err := compareAntigravityGlobalMCPServerProjection(merged, projection)
	if err != nil {
		t.Fatalf("compareAntigravityGlobalMCPServerProjection returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent", comparison)
	}
	if comparison.ContentPath != "/mcpServers/context7" {
		t.Fatalf("content path = %q", comparison.ContentPath)
	}
}

func TestAntigravityGlobalMCPComparisonCanonicalizesFieldOrderAndAbsentArgs(t *testing.T) {
	existing := []byte(`{
	  "mcpServers": {
	    "context7": {
	      "command": "npx"
	    }
	  }
	}`)
	comparison, err := compareAntigravityGlobalMCPServerProjection(existing, validAntigravityMCPProjection("context7"))
	if err != nil {
		t.Fatalf("compareAntigravityGlobalMCPServerProjection returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent with absent args canonicalized", comparison)
	}
}

func TestAntigravityGlobalMCPProjectionRemovePreservesSiblingsAndTopLevelFields(t *testing.T) {
	existing := []byte(`{
	  "comment": {"keep": true},
	  "mcpServers": {
	    "context7": {"command":"npx","args":["-y","@upstash/context7-mcp"]},
	    "sibling": {"serverUrl":"https://example.invalid/mcp","headers":{"Authorization":"Bearer secret"}}
	  }
	}`)

	removed, err := removeAntigravityGlobalMCPServerProjection(existing, "context7")
	if err != nil {
		t.Fatalf("removeAntigravityGlobalMCPServerProjection returned error: %v", err)
	}
	if containsAll(string(removed), `"context7":`) {
		t.Fatalf("removed = %s, did not want managed server id", removed)
	}
	if !containsAll(string(removed), `"comment": {`, `"keep": true`, `"sibling": {`, `"serverUrl":`) {
		t.Fatalf("removed = %s, want top-level and sibling values preserved", removed)
	}
	comparison, err := compareAntigravityGlobalMCPServerProjection(removed, validAntigravityMCPProjection("context7"))
	if err != nil {
		t.Fatalf("compareAntigravityGlobalMCPServerProjection returned error: %v", err)
	}
	if comparison.Present {
		t.Fatalf("comparison = %#v, want absent managed entry", comparison)
	}
}

func TestAntigravityGlobalMCPProjectionRejectsUnsupportedSameNameShapes(t *testing.T) {
	projection := validAntigravityMCPProjection("context7")
	cases := []struct {
		name  string
		input []byte
		want  MCPProjectionReasonCode
	}{
		{name: "malformed json", input: []byte(`{"mcpServers":`), want: MCPProjectionReasonConfigMalformed},
		{name: "mcpServers non object", input: []byte(`{"mcpServers":[]}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "managed entry non object", input: []byte(`{"mcpServers":{"context7":[]}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "missing command", input: []byte(`{"mcpServers":{"context7":{"args":[]}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "args not array", input: []byte(`{"mcpServers":{"context7":{"command":"npx","args":"-y"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "args null", input: []byte(`{"mcpServers":{"context7":{"command":"npx","args":null}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "unsupported type field", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","args":[]}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported literal env field", input: []byte(`{"mcpServers":{"context7":{"command":"npx","args":[],"env":{"TOKEN":"secret"}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported braced env field", input: []byte(`{"mcpServers":{"context7":{"command":"npx","args":[],"env":{"TOKEN":"${TOKEN}"}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported dollar env field", input: []byte(`{"mcpServers":{"context7":{"command":"npx","args":[],"env":{"TOKEN":"$TOKEN"}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported opencode env field", input: []byte(`{"mcpServers":{"context7":{"command":"npx","args":[],"env":{"TOKEN":"{env:TOKEN}"}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported empty env field", input: []byte(`{"mcpServers":{"context7":{"command":"npx","args":[],"env":{}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported null env field", input: []byte(`{"mcpServers":{"context7":{"command":"npx","args":[],"env":null}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported serverUrl field", input: []byte(`{"mcpServers":{"context7":{"command":"npx","serverUrl":"https://example.invalid/mcp"}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported headers field", input: []byte(`{"mcpServers":{"context7":{"command":"npx","headers":{"Authorization":"Bearer SECRET_CANARY"}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "shell command existing", input: []byte(`{"mcpServers":{"context7":{"command":"npx --yes"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compareAntigravityGlobalMCPServerProjection(tc.input, projection)
			assertMCPProjectionReason(t, err, tc.want)
			if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("error leaked secret canary: %q", err)
			}
		})
	}
}

func TestAntigravityGlobalMCPProjectionRejectsInvalidDesiredProjection(t *testing.T) {
	cases := []struct {
		name       string
		projection AntigravityGlobalMCPServerProjection
		want       MCPProjectionReasonCode
	}{
		{
			name: "stale adapter",
			projection: AntigravityGlobalMCPServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				AdapterContract: "antigravity-cli-global-mcp-command-v0",
			},
			want: MCPProjectionReasonStaleAdapterContract,
		},
		{
			name: "absolute command",
			projection: AntigravityGlobalMCPServerProjection{
				ServerID:        "context7",
				Command:         "/usr/bin/../bin/node",
				AdapterContract: aggregate.AntigravityGlobalMCPAmbientEnvV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "invalid server id",
			projection: AntigravityGlobalMCPServerProjection{
				ServerID:        "bad/server",
				Command:         "npx",
				AdapterContract: aggregate.AntigravityGlobalMCPAmbientEnvV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "shell command",
			projection: AntigravityGlobalMCPServerProjection{
				ServerID:        "context7",
				Command:         "npx --yes",
				AdapterContract: aggregate.AntigravityGlobalMCPAmbientEnvV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "invalid environment name",
			projection: AntigravityGlobalMCPServerProjection{
				ServerID:         "context7",
				Command:          "npx",
				EnvironmentNames: []string{"VALID_TOKEN", "9INVALID"},
				AdapterContract:  aggregate.AntigravityGlobalMCPAmbientEnvV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalAntigravityGlobalMCPServerEntry(tc.projection)
			assertMCPProjectionReason(t, err, tc.want)
		})
	}
}

func TestAntigravityGlobalMCPProjectionMergeBlocksUnsupportedSameNameEntry(t *testing.T) {
	existing := []byte(`{
	  "mcpServers": {
	    "context7": {"command":"npx","headers":{"Authorization":"Bearer SECRET_CANARY"}},
	    "sibling": {"serverUrl":"https://example.invalid/mcp","headers":{"Authorization":"Bearer SECRET_CANARY"}}
	  }
	}`)

	_, err := mergeAntigravityGlobalMCPServerProjectionForTest(existing, validAntigravityMCPProjection("context7"))
	assertMCPProjectionReason(t, err, MCPProjectionReasonUnsupportedManagedField)
	if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
		t.Fatalf("error leaked secret canary: %q", err)
	}

	merged, err := mergeAntigravityGlobalMCPServerProjectionForTest(existing, validAntigravityMCPProjection("new-server"))
	if err != nil {
		t.Fatalf("MergeAntigravityGlobalMCPServerProjection for new server returned error: %v", err)
	}
	if !containsAll(string(merged), `"context7": {`, `"headers": {`, `"sibling": {`, `"new-server": {`) {
		t.Fatalf("merged = %s, want unsupported siblings preserved when unmanaged", merged)
	}
}

func mergeAntigravityGlobalMCPServerProjectionForTest(existing []byte, projection AntigravityGlobalMCPServerProjection) ([]byte, error) {
	canonical, err := CanonicalAntigravityGlobalMCPServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return mergeAntigravityGlobalMCPServerCanonicalEntry(existing, projection.ServerID, canonical)
}
