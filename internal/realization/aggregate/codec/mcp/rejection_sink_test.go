package mcpcodec

import (
	"errors"
	"testing"
)

func TestMCPProjectionRejectionSinkStopsEveryBulkExtractorAtFirstError(t *testing.T) {
	tests := []struct {
		name    string
		extract func(MCPProjectionRejectionSink) error
	}{
		{
			name: "Claude project",
			extract: func(reject MCPProjectionRejectionSink) error {
				return ExtractClaudeProjectMCPServerProjections(t.Context(), []byte(`{
  "mcpServers": {
    "c": {"type": "http", "command": "node"},
    "a": {"type": "http", "command": "node"},
    "b": {"type": "http", "command": "node"}
  }
}`), discardProjection[ClaudeProjectMCPServerProjection], reject)
			},
		},
		{
			name: "Claude global",
			extract: func(reject MCPProjectionRejectionSink) error {
				return ExtractClaudeGlobalMCPServerProjections(t.Context(), []byte(`{
  "mcpServers": {
    "c": {"type": "http", "command": "node"},
    "a": {"type": "http", "command": "node"},
    "b": {"type": "http", "command": "node"}
  }
}`), discardProjection[ClaudeGlobalMCPServerProjection], reject)
			},
		},
		{
			name: "OpenCode project",
			extract: func(reject MCPProjectionRejectionSink) error {
				return ExtractOpenCodeProjectMCPServerProjections(t.Context(), []byte(`{
  "mcp": {
    "c": {"type": "remote", "command": ["node"]},
    "a": {"type": "remote", "command": ["node"]},
    "b": {"type": "remote", "command": ["node"]}
  }
}`), discardProjection[MCPNoEnvServerProjection], reject)
			},
		},
		{
			name: "OpenCode global",
			extract: func(reject MCPProjectionRejectionSink) error {
				return ExtractOpenCodeGlobalMCPServerProjections(t.Context(), []byte(`{
  "mcp": {
    "c": {"type": "remote", "command": ["node"]},
    "a": {"type": "remote", "command": ["node"]},
    "b": {"type": "remote", "command": ["node"]}
  }
}`), discardProjection[OpenCodeGlobalMCPServerProjection], reject)
			},
		},
		{
			name: "Codex project",
			extract: func(reject MCPProjectionRejectionSink) error {
				return ExtractCodexProjectMCPServerProjections(t.Context(), []byte(`
[mcp_servers.c]
url = "https://example.invalid/c"
[mcp_servers.a]
url = "https://example.invalid/a"
[mcp_servers.b]
url = "https://example.invalid/b"
`), discardProjection[MCPNoEnvServerProjection], reject)
			},
		},
		{
			name: "Codex global",
			extract: func(reject MCPProjectionRejectionSink) error {
				return ExtractCodexGlobalMCPServerProjections(t.Context(), []byte(`
[mcp_servers.c]
url = "https://example.invalid/c"
[mcp_servers.a]
url = "https://example.invalid/a"
[mcp_servers.b]
url = "https://example.invalid/b"
`), discardProjection[CodexGlobalMCPServerProjection], reject)
			},
		},
		{
			name: "Antigravity global",
			extract: func(reject MCPProjectionRejectionSink) error {
				return ExtractAntigravityGlobalMCPServerProjections(t.Context(), []byte(`{
  "mcpServers": {
    "c": {"serverUrl": "https://example.invalid/c"},
    "a": {"serverUrl": "https://example.invalid/a"},
    "b": {"serverUrl": "https://example.invalid/b"}
  }
}`), discardProjection[AntigravityGlobalMCPServerProjection], reject)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("rejection admission stopped")
			seen := make([]string, 0, 2)
			err := test.extract(func(rejection MCPProjectionRejection) error {
				seen = append(seen, string(rejection.ContentPath()))
				if len(seen) == 2 {
					return cause
				}
				return nil
			})
			if !errors.Is(err, cause) {
				t.Fatalf("extraction error = %v, want %v", err, cause)
			}
			if len(seen) != 2 {
				t.Fatalf("seen rejections = %#v, want first two only", seen)
			}
		})
	}
}

func discardProjection[T any](T) error { return nil }
