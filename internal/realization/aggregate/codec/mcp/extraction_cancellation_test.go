package mcpcodec

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBulkMCPExtractionRejectsPreCanceledContextAcrossEveryImportRoute(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name    string
		extract func(context.Context) error
	}{
		{
			name: "Claude project",
			extract: func(ctx context.Context) error {
				_, _, err := collectClaudeProjectMCPServerProjections(ctx, []byte(`{"mcpServers":{}}`))
				return err
			},
		},
		{
			name: "Claude global",
			extract: func(ctx context.Context) error {
				_, _, err := collectClaudeGlobalMCPServerProjections(ctx, []byte(`{"mcpServers":{}}`))
				return err
			},
		},
		{
			name: "OpenCode project",
			extract: func(ctx context.Context) error {
				_, _, err := collectOpenCodeProjectMCPServerProjections(ctx, []byte(`{"mcp":{}}`))
				return err
			},
		},
		{
			name: "OpenCode global",
			extract: func(ctx context.Context) error {
				_, _, err := collectOpenCodeGlobalMCPServerProjections(ctx, []byte(`{"mcp":{}}`))
				return err
			},
		},
		{
			name: "Codex project",
			extract: func(ctx context.Context) error {
				_, _, err := collectCodexProjectMCPServerProjections(ctx, []byte("[mcp_servers]\n"))
				return err
			},
		},
		{
			name: "Codex global",
			extract: func(ctx context.Context) error {
				_, _, err := collectCodexGlobalMCPServerProjections(ctx, []byte("[mcp_servers]\n"))
				return err
			},
		},
		{
			name: "Antigravity global",
			extract: func(ctx context.Context) error {
				_, _, err := collectAntigravityGlobalMCPServerProjections(ctx, []byte(`{"mcpServers":{}}`))
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.extract(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("bulk extraction error = %v, want context.Canceled", err)
			}
		})
	}
}

func TestBulkMCPExtractionPreservesCancellationDuringJSONAndTOMLAdmission(t *testing.T) {
	tests := []struct {
		name    string
		extract func(context.Context) error
	}{
		{
			name: "JSON",
			extract: func(ctx context.Context) error {
				_, _, err := collectClaudeProjectMCPServerProjections(ctx, []byte(`{"mcpServers":{"context7":{"type":"stdio","command":"node"}}}`))
				return err
			},
		},
		{
			name: "TOML",
			extract: func(ctx context.Context) error {
				_, _, err := collectCodexProjectMCPServerProjections(ctx, []byte("[mcp_servers.context7]\ncommand = \"node\"\n"))
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := &cancelAfterMCPExtractionChecks{cancelAt: 3}
			if err := test.extract(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("bulk extraction error = %v, want context.Canceled", err)
			}
		})
	}
}

type cancelAfterMCPExtractionChecks struct {
	calls    int
	cancelAt int
}

func (ctx *cancelAfterMCPExtractionChecks) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *cancelAfterMCPExtractionChecks) Done() <-chan struct{}       { return nil }
func (ctx *cancelAfterMCPExtractionChecks) Value(any) any               { return nil }
func (ctx *cancelAfterMCPExtractionChecks) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}
