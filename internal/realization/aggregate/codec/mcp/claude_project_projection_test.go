package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestClaudeProjectMCPProjectionFactsAndCanonicalEntry(t *testing.T) {
	projection := validMCPProjection("context7")
	projection.Args = nil
	projection.Env = nil

	entry, err := CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"type": "stdio"`,
		`"command": "npx"`,
		`"args": []`,
		`"env": {}`,
	} {
		if !strings.Contains(string(entry), want) {
			t.Fatalf("canonical entry = %s, want %q", entry, want)
		}
	}
	if aggregate.ClaudeProjectMCPConfigPath != ".mcp.json" {
		t.Fatalf("config path = %q", aggregate.ClaudeProjectMCPConfigPath)
	}
	if got := ClaudeProjectMCPContentPath("context7"); got != "/mcpServers/context7" {
		t.Fatalf("content path = %q", got)
	}
}

func mergeClaudeProjectMCPServerProjection(existing []byte, projection ClaudeProjectMCPServerProjection) ([]byte, error) {
	canonical, err := CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		return nil, err
	}
	return mergeClaudeProjectMCPServerCanonicalEntry(existing, projection.ServerID, canonical)
}

func TestClaudeProjectMCPProjectionMergeCompareAndPreserveSiblings(t *testing.T) {
	existing := []byte(`{
	  "theme": "dark",
	  "mcpServers": {
	    "sibling": {"type":"stdio","command":"node","args":["server.js"],"env":{"TOKEN":"${HOST_TOKEN}"}}
	  }
	}`)
	projection := validMCPProjection("context7")
	projection.Args = []string{"-y", "@upstash/context7-mcp"}
	projection.Env = map[string]string{"API_TOKEN": "${CONTEXT7_API_TOKEN}"}

	merged, err := mergeClaudeProjectMCPServerProjection(existing, projection)
	if err != nil {
		t.Fatalf("mergeClaudeProjectMCPServerProjection returned error: %v", err)
	}
	for _, want := range []string{
		`"theme": "dark"`,
		`"sibling": {`,
		`"context7": {`,
		`"API_TOKEN": "${CONTEXT7_API_TOKEN}"`,
	} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("merged = %s, want %q", merged, want)
		}
	}

	comparison, err := compareClaudeProjectMCPServerProjection(merged, projection)
	if err != nil {
		t.Fatalf("compareClaudeProjectMCPServerProjection returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent", comparison)
	}
	if comparison.ContentPath != "/mcpServers/context7" {
		t.Fatalf("content path = %q", comparison.ContentPath)
	}
}

func TestClaudeProjectMCPComparisonCanonicalizesFieldOrderAndAbsentOptionalFields(t *testing.T) {
	existing := []byte(`{
	  "mcpServers": {
	    "context7": {
	      "command": "npx",
	      "type": "stdio"
	    }
	  }
	}`)
	comparison, err := compareClaudeProjectMCPServerProjection(existing, validMCPProjection("context7"))
	if err != nil {
		t.Fatalf("compareClaudeProjectMCPServerProjection returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent with absent args/env canonicalized", comparison)
	}
}

func TestClaudeProjectMCPComparisonAcceptsLockedCanonicalEntry(t *testing.T) {
	projection := validMCPProjection("context7")
	canonical, err := CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	existing := []byte(`{
	  "mcpServers": {
	    "context7": {
	      "env": {},
	      "args": [],
	      "command": "npx",
	      "type": "stdio"
	    }
	  }
	}`)

	comparison, err := compareClaudeProjectMCPServerCanonicalEntry(existing, "context7", canonical)
	if err != nil {
		t.Fatalf("compareClaudeProjectMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present equivalent", comparison)
	}
}

func TestClaudeProjectMCPComparisonDetectsSemanticDrift(t *testing.T) {
	projection := validMCPProjection("context7")
	projection.Args = []string{"-y", "@upstash/context7-mcp"}
	projection.Env = map[string]string{"API_TOKEN": "${CONTEXT7_API_TOKEN}"}

	cases := []struct {
		name     string
		existing []byte
	}{
		{
			name: "arg order",
			existing: []byte(`{
			  "mcpServers": {
			    "context7": {
			      "type": "stdio",
			      "command": "npx",
			      "args": ["@upstash/context7-mcp", "-y"],
			      "env": {"API_TOKEN": "${CONTEXT7_API_TOKEN}"}
			    }
			  }
			}`),
		},
		{
			name: "env value",
			existing: []byte(`{
			  "mcpServers": {
			    "context7": {
			      "type": "stdio",
			      "command": "npx",
			      "args": ["-y", "@upstash/context7-mcp"],
			      "env": {"API_TOKEN": "${OTHER_TOKEN}"}
			    }
			  }
			}`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comparison, err := compareClaudeProjectMCPServerProjection(tc.existing, projection)
			if err != nil {
				t.Fatalf("compareClaudeProjectMCPServerProjection returned error: %v", err)
			}
			if !comparison.Present || comparison.Equivalent {
				t.Fatalf("comparison = %#v, want present drifted projection", comparison)
			}
		})
	}
}

func TestClaudeProjectMCPMissingConfigIsDistinctFromMalformedEmptyConfig(t *testing.T) {
	projection := validMCPProjection("context7")
	comparison, err := compareClaudeProjectMCPServerProjection(nil, projection)
	if err != nil {
		t.Fatalf("compareClaudeProjectMCPServerProjection(nil) returned error: %v", err)
	}
	if comparison.Present || comparison.Equivalent {
		t.Fatalf("comparison = %#v, want missing/non-equivalent", comparison)
	}

	merged, err := mergeClaudeProjectMCPServerProjection(nil, projection)
	if err != nil {
		t.Fatalf("mergeClaudeProjectMCPServerProjection(nil) returned error: %v", err)
	}
	if !strings.Contains(string(merged), `"mcpServers": {`) ||
		!strings.Contains(string(merged), `"context7": {`) {
		t.Fatalf("merged missing config = %s, want mcpServers/context7", merged)
	}

	_, err = mergeClaudeProjectMCPServerProjection([]byte("  "), projection)
	assertMCPProjectionReason(t, err, MCPProjectionReasonConfigMalformed)
}

func TestClaudeProjectMCPProjectionTreatsOnlyManagedSubtreeAsOwned(t *testing.T) {
	existing := []byte(`{
	  "comment": {"keep": true},
	  "mcpServers": {
	    "sibling": {"type":"http","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer secret"}}
	  }
	}`)
	projection := validMCPProjection("context7")

	comparison, err := compareClaudeProjectMCPServerProjection(existing, projection)
	if err != nil {
		t.Fatalf("compareClaudeProjectMCPServerProjection returned error: %v", err)
	}
	if comparison.Present || comparison.Equivalent {
		t.Fatalf("comparison = %#v, want unmanaged missing server without sibling interpretation", comparison)
	}

	merged, err := mergeClaudeProjectMCPServerProjection(existing, projection)
	if err != nil {
		t.Fatalf("mergeClaudeProjectMCPServerProjection returned error: %v", err)
	}
	if !containsAll(
		string(merged),
		`"comment": {`,
		`"keep": true`,
		`"sibling": {`,
		`"headers": {`,
		`"context7": {`,
	) {
		t.Fatalf("merged = %s, want unknown top-level and sibling entries preserved", merged)
	}
}

func TestClaudeProjectMCPProjectionCreatesMCPServersWithoutOwningSiblingNames(t *testing.T) {
	existing := []byte(`{
	  "comment": "keep"
	}`)
	projection := validMCPProjection("context7")

	merged, err := mergeClaudeProjectMCPServerProjection(existing, projection)
	if err != nil {
		t.Fatalf("mergeClaudeProjectMCPServerProjection returned error: %v", err)
	}
	if !containsAll(string(merged), `"comment": "keep"`, `"mcpServers": {`, `"context7": {`) {
		t.Fatalf("merged = %s, want top-level fields plus created mcpServers object", merged)
	}

	existingWithOddSibling := []byte(`{
	  "mcpServers": {
	    "bad/server": {"type":"http","url":"https://example.invalid/mcp"}
	  }
	}`)
	merged, err = mergeClaudeProjectMCPServerProjection(existingWithOddSibling, projection)
	if err != nil {
		t.Fatalf("mergeClaudeProjectMCPServerProjection with odd sibling returned error: %v", err)
	}
	if !containsAll(string(merged), `"bad/server": {`, `"context7": {`) {
		t.Fatalf("merged = %s, want unrelated sibling name preserved", merged)
	}
}

func TestClaudeProjectMCPProjectionRemovePreservesSiblingsAndTopLevelFields(t *testing.T) {
	existing := []byte(`{
	  "comment": {"keep": true},
	  "mcpServers": {
	    "context7": {"type":"stdio","command":"npx","args":["-y","@upstash/context7-mcp"],"env":{"API_TOKEN":"${CONTEXT7_API_TOKEN}"}},
	    "sibling": {"type":"http","url":"https://example.invalid/mcp"}
	  }
	}`)

	removed, err := removeClaudeProjectMCPServerProjection(existing, "context7")
	if err != nil {
		t.Fatalf("removeClaudeProjectMCPServerProjection returned error: %v", err)
	}
	if containsAll(string(removed), `"context7":`) {
		t.Fatalf("removed = %s, did not want managed server id", removed)
	}
	if !containsAll(string(removed), `"comment": {`, `"keep": true`, `"sibling": {`) {
		t.Fatalf("removed = %s, want top-level and sibling values preserved", removed)
	}
	comparison, err := compareClaudeProjectMCPServerProjection(removed, validMCPProjection("context7"))
	if err != nil {
		t.Fatalf("compareClaudeProjectMCPServerProjection returned error: %v", err)
	}
	if comparison.Present {
		t.Fatalf("comparison = %#v, want absent managed entry", comparison)
	}
}

func TestClaudeProjectMCPProjectionRemoveKeepsValidAggregateWhenLastServerRemoved(t *testing.T) {
	existing := []byte(`{
	  "mcpServers": {
	    "context7": {"type":"stdio","command":"npx","args":[],"env":{}}
	  }
	}`)

	removed, err := removeClaudeProjectMCPServerProjection(existing, "context7")
	if err != nil {
		t.Fatalf("removeClaudeProjectMCPServerProjection returned error: %v", err)
	}
	if strings.TrimSpace(string(removed)) != "{\n  \"mcpServers\": {}\n}" {
		t.Fatalf("removed = %s, want valid empty mcpServers aggregate", removed)
	}
}
