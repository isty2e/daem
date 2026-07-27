package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestClaudeGlobalMCPProjectionFactsAndCanonicalEntry(t *testing.T) {
	projection := validClaudeGlobalMCPProjection("context7")
	projection.Args = nil

	entry, err := CanonicalClaudeGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalClaudeGlobalMCPServerEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"type": "stdio"`,
		`"command": "npx"`,
		`"args": []`,
	} {
		if !strings.Contains(string(entry), want) {
			t.Fatalf("canonical entry = %s, want %q", entry, want)
		}
	}
	if strings.Contains(string(entry), `"env"`) {
		t.Fatalf("canonical entry = %s, want command/args-only entry without empty env field", entry)
	}
	if aggregate.ClaudeGlobalMCPConfigPath != "~/.claude.json" {
		t.Fatalf("config path = %q", aggregate.ClaudeGlobalMCPConfigPath)
	}
	if got := ClaudeGlobalMCPContentPath("context7"); got != "/mcpServers/context7" {
		t.Fatalf("content path = %q", got)
	}
}

func TestClaudeGlobalMCPProjectionCanonicalizesAbsentArgsAndEnv(t *testing.T) {
	existing := []byte(`{
	  "mcpServers": {
	    "context7": {
	      "type": "stdio",
	      "command": "npx"
	    }
	  }
	}`)
	comparison, err := compareClaudeGlobalMCPServerProjection(existing, validClaudeGlobalMCPProjection("context7"))
	if err != nil {
		t.Fatalf("compareClaudeGlobalMCPServerProjection returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent with absent args/env canonicalized", comparison)
	}
}

func TestClaudeGlobalMCPProjectionMergeCompareAndPreserveUserSiblings(t *testing.T) {
	existing := []byte(`{
	  "projects": {
	    "/repo": {
	      "mcpServers": {
	        "context7": {"type":"stdio","command":"project-shadow"}
	      }
	    }
	  },
	  "oauth": {"refreshToken": "SECRET_CANARY"},
	  "mcpServers": {
	    "sibling": {"type":"http","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer SECRET_CANARY"}}
	  }
	}`)
	canonical, err := CanonicalClaudeGlobalMCPServerEntry(MCPNoEnvServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		AdapterContract: aggregate.ClaudeGlobalMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeGlobalMCPServerEntry returned error: %v", err)
	}

	merged, err := mergeClaudeGlobalMCPServerCanonicalEntry(existing, "context7", canonical)
	if err != nil {
		t.Fatalf("mergeClaudeGlobalMCPServerCanonicalEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"projects": {`,
		`"project-shadow"`,
		`"oauth": {`,
		`"SECRET_CANARY"`,
		`"sibling": {`,
		`"context7": {`,
		`"command": "npx"`,
	} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("merged = %s, want %q", merged, want)
		}
	}

	comparison, err := compareClaudeGlobalMCPServerCanonicalEntry(merged, "context7", canonical)
	if err != nil {
		t.Fatalf("compareClaudeGlobalMCPServerCanonicalEntry returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want present/equivalent", comparison)
	}
}

func TestClaudeGlobalMCPProjectionRemovePreservesSiblingsAndProjects(t *testing.T) {
	existing := []byte(`{
	  "projects": {
	    "/repo": {
	      "mcpServers": {
	        "context7": {"type":"stdio","command":"project-shadow"}
	      }
	    }
	  },
	  "mcpServers": {
	    "context7": {"type":"stdio","command":"npx","args":["-y"],"env":{}},
	    "sibling": {"type":"http","url":"https://example.invalid/mcp"}
	  }
	}`)

	removed, err := removeClaudeGlobalMCPServerProjection(existing, "context7")
	if err != nil {
		t.Fatalf("removeClaudeGlobalMCPServerProjection returned error: %v", err)
	}
	if !containsAll(string(removed), `"projects": {`, `"project-shadow"`, `"sibling": {`, `"url": "https://example.invalid/mcp"`) {
		t.Fatalf("removed = %s, want sibling and project shadow preserved", removed)
	}
	comparison, err := compareClaudeGlobalMCPServerProjection(removed, validClaudeGlobalMCPProjection("context7"))
	if err != nil {
		t.Fatalf("compareClaudeGlobalMCPServerProjection returned error: %v", err)
	}
	if comparison.Present {
		t.Fatalf("comparison = %#v, want absent top-level managed entry", comparison)
	}
}

func TestClaudeGlobalMCPProjectionRejectsUnsupportedSameNameShapes(t *testing.T) {
	projection := validClaudeGlobalMCPProjection("context7")
	cases := []struct {
		name  string
		input []byte
		want  MCPProjectionReasonCode
	}{
		{name: "malformed json", input: []byte(`{"mcpServers":`), want: MCPProjectionReasonConfigMalformed},
		{name: "mcpServers non object", input: []byte(`{"mcpServers":[]}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "managed entry non object", input: []byte(`{"mcpServers":{"context7":[]}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "missing type", input: []byte(`{"mcpServers":{"context7":{"command":"npx"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "missing command", input: []byte(`{"mcpServers":{"context7":{"type":"stdio"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "unsupported transport", input: []byte(`{"mcpServers":{"context7":{"type":"http","url":"https://example.invalid/mcp"}}}`), want: MCPProjectionReasonUnsupportedTransport},
		{name: "non-empty env", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":{"TOKEN":"SECRET_CANARY"}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported headers", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","headers":{"Authorization":"Bearer SECRET_CANARY"}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported cwd", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","cwd":"/tmp"}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported timeout", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","timeout":30}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported alwaysLoad", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","alwaysLoad":true}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "args not array", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","args":"-y"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "env null", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":null}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "shell command existing", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx --yes"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compareClaudeGlobalMCPServerProjection(tc.input, projection)
			assertMCPProjectionReason(t, err, tc.want)
			if err != nil && strings.Contains(err.Error(), "SECRET_CANARY") {
				t.Fatalf("error leaked secret canary: %q", err)
			}
		})
	}
}

func TestExtractClaudeGlobalMCPServerProjectionsSeparatesAdmittedAndRejectedRows(t *testing.T) {
	projections, rejections, err := ExtractClaudeGlobalMCPServerProjections([]byte(`{
  "projects": {
    "/repo": {
      "mcpServers": {
        "projectOnly": {"type": "stdio", "command": "node"}
      }
    }
  },
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx", "args": ["-y"]},
    "remote": {"type": "http", "url": "https://example.invalid/mcp"},
    "secret": {"type": "stdio", "command": "npx", "env": {"TOKEN": "SECRET_CANARY"}}
  }
}`))
	if err != nil {
		t.Fatalf("ExtractClaudeGlobalMCPServerProjections returned error: %v", err)
	}
	if len(projections) != 1 {
		t.Fatalf("projections = %#v, want one admitted row", projections)
	}
	projection := projections[0]
	if projection.ServerID != "context7" || projection.Command != "npx" || len(projection.Args) != 1 || projection.Args[0] != "-y" {
		t.Fatalf("projection = %#v, want context7 npx -y", projection)
	}
	if len(rejections) != 2 {
		t.Fatalf("rejections = %#v, want two", rejections)
	}
	assertProjectionRejection(t, rejections, "/mcpServers/remote", MCPProjectionReasonUnsupportedTransport)
	assertProjectionRejection(t, rejections, "/mcpServers/secret", MCPProjectionReasonUnsupportedManagedField)
}

func TestClaudeGlobalMCPProjectionRejectsInvalidDesiredProjection(t *testing.T) {
	cases := []struct {
		name       string
		projection MCPNoEnvServerProjection
		want       MCPProjectionReasonCode
	}{
		{
			name: "stale adapter",
			projection: MCPNoEnvServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				AdapterContract: "claude-code-user-mcp-stdio-v0",
			},
			want: MCPProjectionReasonStaleAdapterContract,
		},
		{
			name: "absolute command",
			projection: MCPNoEnvServerProjection{
				ServerID:        "context7",
				Command:         "/usr/bin/../bin/node",
				AdapterContract: aggregate.ClaudeGlobalMCPStdioAdapterV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "invalid server id",
			projection: MCPNoEnvServerProjection{
				ServerID:        "bad/server",
				Command:         "npx",
				AdapterContract: aggregate.ClaudeGlobalMCPStdioAdapterV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "shell command",
			projection: MCPNoEnvServerProjection{
				ServerID:        "context7",
				Command:         "npx --yes",
				AdapterContract: aggregate.ClaudeGlobalMCPStdioAdapterV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalClaudeGlobalMCPServerEntry(tc.projection)
			assertMCPProjectionReason(t, err, tc.want)
		})
	}
}
