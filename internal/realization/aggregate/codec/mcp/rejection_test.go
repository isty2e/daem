package mcpcodec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestValidateMCPCommandPreservesProjectionReasonAndValidationDetail(t *testing.T) {
	absoluteBase, err := filepath.Abs("mcp-command")
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}
	separator := string(filepath.Separator)
	nonCanonicalAbsolute := absoluteBase + separator + ".." + separator + filepath.Base(absoluteBase)

	tests := []struct {
		name    string
		command string
		detail  string
	}{
		{
			name:    "non-canonical absolute path",
			command: nonCanonicalAbsolute,
			detail:  "command path must be canonical",
		},
		{
			name:    "invalid ambient token",
			command: "npx --yes",
			detail:  "command must be a portable command token",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateMCPCommand(test.command)
			assertMCPProjectionReason(t, err, MCPProjectionReasonProjectionEquivalenceUndefined)
			if !strings.Contains(err.Error(), test.detail) {
				t.Fatalf("error = %q, want detail %q", err, test.detail)
			}
		})
	}
}

func TestClaudeProjectMCPProjectionRejectsUnsupportedShapesWithReasonCodes(t *testing.T) {
	projection := validMCPProjection("context7")
	cases := []struct {
		name  string
		input []byte
		want  MCPProjectionReasonCode
	}{
		{name: "malformed json", input: []byte(`{"mcpServers":`), want: MCPProjectionReasonConfigMalformed},
		{name: "multiple json values", input: []byte(`{} {}`), want: MCPProjectionReasonConfigMalformed},
		{name: "trailing malformed json", input: []byte(`{} @`), want: MCPProjectionReasonConfigMalformed},
		{name: "top level array", input: []byte(`[]`), want: MCPProjectionReasonConfigMalformed},
		{name: "top level null", input: []byte(`null`), want: MCPProjectionReasonConfigMalformed},
		{name: "duplicate top level key", input: []byte(`{"mcpServers":{},"mcpServers":{}}`), want: MCPProjectionReasonDuplicateKey},
		{name: "mcpServers non object", input: []byte(`{"mcpServers":[]}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "mcpServers null", input: []byte(`{"mcpServers":null}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "duplicate server key", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx"},"context7":{"type":"stdio","command":"node"}}}`), want: MCPProjectionReasonDuplicateKey},
		{name: "managed entry non object", input: []byte(`{"mcpServers":{"context7":[]}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "missing type", input: []byte(`{"mcpServers":{"context7":{"command":"npx"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "missing command", input: []byte(`{"mcpServers":{"context7":{"type":"stdio"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "duplicate managed field", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","command":"node"}}}`), want: MCPProjectionReasonDuplicateKey},
		{name: "unsupported field", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","headers":{}}}}`), want: MCPProjectionReasonUnsupportedManagedField},
		{name: "unsupported transport", input: []byte(`{"mcpServers":{"context7":{"type":"http","command":"npx"}}}`), want: MCPProjectionReasonUnsupportedTransport},
		{name: "literal env value", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":{"TOKEN":"literal-secret"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "invalid env reference", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":{"TOKEN":"${}"}}}}`), want: MCPProjectionReasonSecretLiteralForbidden},
		{name: "duplicate env key", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":{"TOKEN":"${TOKEN}","TOKEN":"${OTHER_TOKEN}"}}}}`), want: MCPProjectionReasonDuplicateKey},
		{name: "args not array", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","args":"-y"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "args null", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","args":null}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "env null", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx","env":null}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
		{name: "shell command existing", input: []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"npx --yes"}}}`), want: MCPProjectionReasonProjectionEquivalenceUndefined},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compareClaudeProjectMCPServerProjection(tc.input, projection)
			assertMCPProjectionReason(t, err, tc.want)
		})
	}
}

func TestExtractClaudeProjectMCPServerProjectionsSeparatesAdmittedAndRejectedRows(t *testing.T) {
	projections, rejections, err := ExtractClaudeProjectMCPServerProjections(t.Context(), []byte(`{
  "project": "keep",
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx", "args": ["-y"], "env": {"TOKEN": "${TOKEN}"}},
    "remote": {"type": "http", "command": "npx"},
    "secret": {"type": "stdio", "command": "npx", "env": {"TOKEN": "literal"}}
  }
}`))
	if err != nil {
		t.Fatalf("ExtractClaudeProjectMCPServerProjections returned error: %v", err)
	}
	if len(projections) != 1 {
		t.Fatalf("projections = %#v, want one admitted row", projections)
	}
	projection := projections[0]
	if projection.ServerID != "context7" || projection.Command != "npx" || len(projection.Args) != 1 || projection.Args[0] != "-y" {
		t.Fatalf("projection = %#v, want context7 npx -y", projection)
	}
	if projection.Env["TOKEN"] != "${TOKEN}" {
		t.Fatalf("env = %#v, want TOKEN host reference", projection.Env)
	}
	if len(rejections) != 2 {
		t.Fatalf("rejections = %#v, want two", rejections)
	}
	assertProjectionRejection(t, rejections, "/mcpServers/remote", MCPProjectionReasonUnsupportedTransport)
	assertProjectionRejection(t, rejections, "/mcpServers/secret", MCPProjectionReasonSecretLiteralForbidden)
}

func TestExtractOpenCodeProjectMCPServerProjectionsSplitsCommandArray(t *testing.T) {
	projections, rejections, err := ExtractOpenCodeProjectMCPServerProjections(t.Context(), []byte(`{
  "mcp": {
    "context7": {"type": "local", "command": ["npx", "-y", "@upstash/context7-mcp"]},
    "withEnv": {"type": "local", "command": ["npx"], "env": {"TOKEN": "${TOKEN}"}}
  }
}`))
	if err != nil {
		t.Fatalf("ExtractOpenCodeProjectMCPServerProjections returned error: %v", err)
	}
	if len(projections) != 1 {
		t.Fatalf("projections = %#v, want one admitted row", projections)
	}
	if projections[0].Command != "npx" || len(projections[0].Args) != 2 || projections[0].Args[1] != "@upstash/context7-mcp" {
		t.Fatalf("projection = %#v, want split command and args", projections[0])
	}
	assertProjectionRejection(t, rejections, "/mcp/withEnv", MCPProjectionReasonUnsupportedManagedField)
}

func TestExtractAntigravityGlobalMCPServerProjectionsSeparatesUnsupportedFields(t *testing.T) {
	projections, rejections, err := ExtractAntigravityGlobalMCPServerProjections(t.Context(), []byte(`{
  "mcpServers": {
    "context7": {"command": "npx", "args": ["-y"]},
    "remote": {"serverUrl": "https://example.invalid/mcp"}
  }
}`))
	if err != nil {
		t.Fatalf("ExtractAntigravityGlobalMCPServerProjections returned error: %v", err)
	}
	if len(projections) != 1 || projections[0].ServerID != "context7" || projections[0].Command != "npx" {
		t.Fatalf("projections = %#v, want context7 npx", projections)
	}
	if len(projections[0].EnvironmentNames) != 0 {
		t.Fatalf("imported environment names = %#v, want no inferred ambient references", projections[0].EnvironmentNames)
	}
	projections[0].Args[0] = "mutated"
	reimported, _, err := ExtractAntigravityGlobalMCPServerProjections(t.Context(), []byte(`{
  "mcpServers": {
    "context7": {"command": "npx", "args": ["-y"]}
  }
}`))
	if err != nil {
		t.Fatalf("reimport Antigravity projection: %v", err)
	}
	if reimported[0].Args[0] != "-y" {
		t.Fatalf("imported args alias decoder storage: %#v", reimported[0].Args)
	}
	assertProjectionRejection(t, rejections, "/mcpServers/remote", MCPProjectionReasonUnsupportedManagedField)
}

func assertProjectionRejection(t *testing.T, rejections []MCPProjectionRejection, contentPath string, reason MCPProjectionReasonCode) {
	t.Helper()
	for _, rejection := range rejections {
		if rejection.ContentPath == contentPath && rejection.Reason == reason {
			return
		}
	}
	t.Fatalf("rejections = %#v, want %s %s", rejections, contentPath, reason)
}

func TestClaudeProjectMCPProjectionRejectsInvalidDesiredProjection(t *testing.T) {
	cases := []struct {
		name       string
		projection ClaudeProjectMCPServerProjection
		want       MCPProjectionReasonCode
	}{
		{
			name: "stale adapter",
			projection: ClaudeProjectMCPServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				AdapterContract: "claude-project-mcp-stdio-v0",
			},
			want: MCPProjectionReasonStaleAdapterContract,
		},
		{
			name: "absolute command",
			projection: ClaudeProjectMCPServerProjection{
				ServerID:        "context7",
				Command:         "/usr/bin/../bin/node",
				AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "invalid server id",
			projection: ClaudeProjectMCPServerProjection{
				ServerID:        "bad/server",
				Command:         "npx",
				AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "shell command",
			projection: ClaudeProjectMCPServerProjection{
				ServerID:        "context7",
				Command:         "npx --yes",
				AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
			},
			want: MCPProjectionReasonProjectionEquivalenceUndefined,
		},
		{
			name: "literal env",
			projection: ClaudeProjectMCPServerProjection{
				ServerID:        "context7",
				Command:         "npx",
				Env:             map[string]string{"TOKEN": "literal-secret"},
				AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
			},
			want: MCPProjectionReasonSecretLiteralForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalClaudeProjectMCPServerEntry(tc.projection)
			assertMCPProjectionReason(t, err, tc.want)
		})
	}
}

func TestClaudeProjectMCPProjectionMergeBlocksUnsupportedSameNameEntry(t *testing.T) {
	existing := []byte(`{
	  "mcpServers": {
	    "context7": {"type":"stdio","command":"npx","headers":{}},
	    "sibling": {"type":"http","url":"https://example.invalid/mcp"}
	  }
	}`)

	_, err := mergeClaudeProjectMCPServerProjection(existing, validMCPProjection("context7"))
	assertMCPProjectionReason(t, err, MCPProjectionReasonUnsupportedManagedField)

	merged, err := mergeClaudeProjectMCPServerProjection(existing, validMCPProjection("new-server"))
	if err != nil {
		t.Fatalf("mergeClaudeProjectMCPServerProjection for new server returned error: %v", err)
	}
	if !containsAll(string(merged), `"context7": {`, `"headers": {}`, `"sibling": {`, `"type": "http"`) {
		t.Fatalf("merged = %s, want unsupported siblings preserved when unmanaged", merged)
	}
}

func TestClaudeProjectMCPProjectionRemoveBlocksUnsupportedSameNameEntry(t *testing.T) {
	existing := []byte(`{
	  "mcpServers": {
	    "context7": {"type":"stdio","command":"npx","cwd":"/tmp"}
	  }
	}`)
	_, err := removeClaudeProjectMCPServerProjection(existing, "context7")
	assertMCPProjectionReason(t, err, MCPProjectionReasonUnsupportedManagedField)
}
