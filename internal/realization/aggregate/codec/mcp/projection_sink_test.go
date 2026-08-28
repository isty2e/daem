package mcpcodec

import (
	"errors"
	"testing"
)

func TestMCPProjectionSinkStopsEveryBulkExtractorAtFirstError(t *testing.T) {
	t.Run("Claude project", func(t *testing.T) {
		assertProjectionSinkStops(t, func(project MCPProjectionSink[ClaudeProjectMCPServerProjection]) error {
			return ExtractClaudeProjectMCPServerProjections(t.Context(), []byte(`{
  "mcpServers": {
    "c": {"type": "stdio", "command": "node"},
    "a": {"type": "stdio", "command": "node"},
    "b": {"type": "stdio", "command": "node"}
  }
}`), project, discardMCPProjectionRejection)
		})
	})
	t.Run("Claude global", func(t *testing.T) {
		assertProjectionSinkStops(t, func(project MCPProjectionSink[ClaudeGlobalMCPServerProjection]) error {
			return ExtractClaudeGlobalMCPServerProjections(t.Context(), []byte(`{
  "mcpServers": {
    "c": {"type": "stdio", "command": "node"},
    "a": {"type": "stdio", "command": "node"},
    "b": {"type": "stdio", "command": "node"}
  }
}`), project, discardMCPProjectionRejection)
		})
	})
	t.Run("OpenCode project", func(t *testing.T) {
		assertProjectionSinkStops(t, func(project MCPProjectionSink[MCPNoEnvServerProjection]) error {
			return ExtractOpenCodeProjectMCPServerProjections(t.Context(), []byte(`{
  "mcp": {
    "c": {"type": "local", "command": ["node"]},
    "a": {"type": "local", "command": ["node"]},
    "b": {"type": "local", "command": ["node"]}
  }
}`), project, discardMCPProjectionRejection)
		})
	})
	t.Run("OpenCode global", func(t *testing.T) {
		assertProjectionSinkStops(t, func(project MCPProjectionSink[OpenCodeGlobalMCPServerProjection]) error {
			return ExtractOpenCodeGlobalMCPServerProjections(t.Context(), []byte(`{
  "mcp": {
    "c": {"type": "local", "command": ["node"]},
    "a": {"type": "local", "command": ["node"]},
    "b": {"type": "local", "command": ["node"]}
  }
}`), project, discardMCPProjectionRejection)
		})
	})
	t.Run("Codex project", func(t *testing.T) {
		assertProjectionSinkStops(t, func(project MCPProjectionSink[MCPNoEnvServerProjection]) error {
			return ExtractCodexProjectMCPServerProjections(t.Context(), []byte(`
[mcp_servers.c]
command = "node"
[mcp_servers.a]
command = "node"
[mcp_servers.b]
command = "node"
`), project, discardMCPProjectionRejection)
		})
	})
	t.Run("Codex global", func(t *testing.T) {
		assertProjectionSinkStops(t, func(project MCPProjectionSink[CodexGlobalMCPServerProjection]) error {
			return ExtractCodexGlobalMCPServerProjections(t.Context(), []byte(`
[mcp_servers.c]
command = "node"
[mcp_servers.a]
command = "node"
[mcp_servers.b]
command = "node"
`), project, discardMCPProjectionRejection)
		})
	})
	t.Run("Antigravity global", func(t *testing.T) {
		assertProjectionSinkStops(t, func(project MCPProjectionSink[AntigravityGlobalMCPServerProjection]) error {
			return ExtractAntigravityGlobalMCPServerProjections(t.Context(), []byte(`{
  "mcpServers": {
    "c": {"command": "node"},
    "a": {"command": "node"},
    "b": {"command": "node"}
  }
}`), project, discardMCPProjectionRejection)
		})
	})
}

func TestMCPProjectionSinksAreRequired(t *testing.T) {
	if err := ExtractClaudeProjectMCPServerProjections(
		t.Context(),
		[]byte(`{"mcpServers":{}}`),
		nil,
		discardMCPProjectionRejection,
	); err == nil {
		t.Fatal("bulk extraction accepted a nil projection sink")
	}
	if err := ExtractClaudeProjectMCPServerProjections(
		t.Context(),
		[]byte(`{"mcpServers":{}}`),
		func(ClaudeProjectMCPServerProjection) error { return nil },
		nil,
	); err == nil {
		t.Fatal("bulk extraction accepted a nil rejection sink")
	}
}

func assertProjectionSinkStops[T any](t *testing.T, extract func(MCPProjectionSink[T]) error) {
	t.Helper()
	cause := errors.New("projection admission stopped")
	seen := 0
	err := extract(func(T) error {
		seen++
		if seen == 2 {
			return cause
		}
		return nil
	})
	if !errors.Is(err, cause) {
		t.Fatalf("extraction error = %v, want %v", err, cause)
	}
	if seen != 2 {
		t.Fatalf("seen projections = %d, want first two only", seen)
	}
}

func discardMCPProjectionRejection(MCPProjectionRejection) error { return nil }
