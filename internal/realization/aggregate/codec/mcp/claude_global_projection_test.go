package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestClaudeGlobalMCPProjectionFactsAndCanonicalEntry(t *testing.T) {
	projection := validClaudeGlobalMCPProjection("context7")
	projection.Args = nil
	projection.Env = map[string]string{
		"API_TOKEN": "${CONTEXT7_API_TOKEN}",
	}

	entry, err := CanonicalClaudeGlobalMCPServerEntry(projection)
	if err != nil {
		t.Fatalf("CanonicalClaudeGlobalMCPServerEntry returned error: %v", err)
	}
	for _, want := range []string{
		`"type": "stdio"`,
		`"command": "npx"`,
		`"args": []`,
		`"env": {`,
		`"API_TOKEN": "${CONTEXT7_API_TOKEN}"`,
	} {
		if !strings.Contains(string(entry), want) {
			t.Fatalf("canonical entry = %s, want %q", entry, want)
		}
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

func TestClaudeGlobalMCPProjectionEnvComparisonIsOrderIndependentAndValueSensitive(t *testing.T) {
	projection := validClaudeGlobalMCPProjection("context7")
	projection.Env = map[string]string{
		"API_TOKEN":   "${CONTEXT7_API_TOKEN}",
		"TRACE_TOKEN": "${CONTEXT7_TRACE_TOKEN}",
	}
	existing := []byte(`{
	  "mcpServers": {
	    "context7": {
	      "type": "stdio",
	      "command": "npx",
	      "args": [],
	      "env": {
	        "TRACE_TOKEN": "${CONTEXT7_TRACE_TOKEN}",
	        "API_TOKEN": "${CONTEXT7_API_TOKEN}"
	      }
	    }
	  }
	}`)
	comparison, err := compareClaudeGlobalMCPServerProjection(existing, projection)
	if err != nil {
		t.Fatalf("compareClaudeGlobalMCPServerProjection returned error: %v", err)
	}
	if !comparison.Present || !comparison.Equivalent {
		t.Fatalf("comparison = %#v, want order-independent equivalence", comparison)
	}

	projection.Env["TRACE_TOKEN"] = "${OTHER_TRACE_TOKEN}"
	comparison, err = compareClaudeGlobalMCPServerProjection(existing, projection)
	if err != nil {
		t.Fatalf("compare changed Claude global env returned error: %v", err)
	}
	if !comparison.Present || comparison.Equivalent {
		t.Fatalf("comparison = %#v, want changed env reference to be drift", comparison)
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
	canonical, err := CanonicalClaudeGlobalMCPServerEntry(ClaudeGlobalMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y", "@upstash/context7-mcp"},
		Env:             map[string]string{"API_TOKEN": "${CONTEXT7_API_TOKEN}"},
		AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
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
		`"API_TOKEN": "${CONTEXT7_API_TOKEN}"`,
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
	    "context7": {"type":"stdio","command":"npx","args":["-y"],"env":{"API_TOKEN":"${CONTEXT7_API_TOKEN}"}},
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
		{name: "literal env", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":{"TOKEN":"SECRET_CANARY"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "defaulted env", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":{"TOKEN":"${TOKEN:-default}"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "compound env", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":{"TOKEN":"Bearer ${TOKEN}"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "user config env", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":{"TOKEN":"${user_config.token}"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "invalid child name", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":{"BAD-NAME":"${TOKEN}"}}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "env value not string", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":{"TOKEN":1}}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
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

func TestCollectClaudeGlobalMCPServerProjectionsSeparatesAdmittedAndRejectedRows(t *testing.T) {
	document := []byte(`{
  "projects": {
    "/repo": {
      "mcpServers": {
        "projectOnly": {"type": "stdio", "command": "node"}
      }
    }
  },
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx", "args": ["-y"], "env": {"API_TOKEN": "${CONTEXT7_API_TOKEN}"}},
    "remote": {"type": "http", "url": "https://example.invalid/mcp"},
    "secret": {"type": "stdio", "command": "npx", "env": {"TOKEN": "SECRET_CANARY"}}
  }
}`)
	projections, rejections, err := collectClaudeGlobalMCPServerProjections(t.Context(), document)
	if err != nil {
		t.Fatalf("collectClaudeGlobalMCPServerProjections returned error: %v", err)
	}
	if len(projections) != 1 {
		t.Fatalf("projections = %#v, want one admitted row", projections)
	}
	projection := projections[0]
	if projection.ServerID != "context7" ||
		projection.Command != "npx" ||
		len(projection.Args) != 1 ||
		projection.Args[0] != "-y" ||
		len(projection.Env) != 1 ||
		projection.Env["API_TOKEN"] != "${CONTEXT7_API_TOKEN}" {
		t.Fatalf("projection = %#v, want context7 npx -y", projection)
	}
	if len(rejections) != 2 {
		t.Fatalf("rejections = %#v, want two", rejections)
	}
	assertProjectionRejection(t, rejections, "/mcpServers/remote", MCPProjectionReasonUnsupportedTransport)
	assertProjectionRejection(t, rejections, "/mcpServers/secret", MCPProjectionReasonSecretLiteralForbidden)

	projection.Args[0] = "--mutated"
	projection.Env["API_TOKEN"] = "${MUTATED_TOKEN}"
	reextracted, _, err := collectClaudeGlobalMCPServerProjections(t.Context(), document)
	if err != nil {
		t.Fatalf("re-extract Claude global projections returned error: %v", err)
	}
	if reextracted[0].Args[0] != "-y" ||
		reextracted[0].Env["API_TOKEN"] != "${CONTEXT7_API_TOKEN}" {
		t.Fatalf("re-extracted projection aliased prior result: %#v", reextracted[0])
	}
}

func TestClaudeGlobalMCPProjectionRejectsInvalidDesiredProjection(t *testing.T) {
	cases := []struct {
		name       string
		projection ClaudeGlobalMCPServerProjection
		want       MCPProjectionReasonCode
	}{
		{
			name: "stale adapter",
			projection: ClaudeGlobalMCPServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				AdapterContract: "claude-code-user-mcp-stdio-v0",
			},
			want: MCPProjectionReasonStaleAdapterContract,
		},
		{
			name: "absolute command",
			projection: ClaudeGlobalMCPServerProjection{
				ServerID:        "context7",
				Command:         "/usr/bin/../bin/node",
				AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "invalid server id",
			projection: ClaudeGlobalMCPServerProjection{
				ServerID:        "bad/server",
				Command:         "npx",
				AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "shell command",
			projection: ClaudeGlobalMCPServerProjection{
				ServerID:        "context7",
				Command:         "npx --yes",
				AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "literal env",
			projection: ClaudeGlobalMCPServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				Env:             map[string]string{"TOKEN": "SECRET_CANARY"},
				AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
			},
			want: MCPProjectionReasonSecretLiteralForbidden,
		},
		{
			name: "defaulted env",
			projection: ClaudeGlobalMCPServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				Env:             map[string]string{"TOKEN": "${TOKEN:-default}"},
				AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
			},
			want: MCPProjectionReasonSecretLiteralForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalClaudeGlobalMCPServerEntry(tc.projection)
			assertMCPProjectionReason(t, err, tc.want)
		})
	}
}
