package mcp

import (
	"context"
	"errors"
	"os"
	"testing"

	adopt "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestCandidatesCancellationAfterSnapshotReturnsNoPartialResult(t *testing.T) {
	withWorkingDirectory(t, t.TempDir())
	writeCancellationMCPConfig(t)
	cause := errors.New("cancel after MCP snapshot")
	ctx, cancel := context.WithCancelCause(t.Context())

	servers, authorities, skipped, err := candidatesWithHooks(
		ctx,
		target.TargetClaudeCode,
		target.ScopeProject,
		candidateHooks{afterSnapshot: func() { cancel(cause) }},
	)
	assertCanceledCandidates(t, servers, authorities, skipped, err, cause)
}

func TestCandidatesCancellationDuringArgumentAdmissionReturnsNoPartialResult(t *testing.T) {
	withWorkingDirectory(t, t.TempDir())
	writeCancellationMCPConfig(t)
	cause := errors.New("cancel during MCP argument admission")
	ctx, cancel := context.WithCancelCause(t.Context())

	servers, authorities, skipped, err := candidatesWithHooks(
		ctx,
		target.TargetClaudeCode,
		target.ScopeProject,
		candidateHooks{beforeArgumentAdmission: func(int) { cancel(cause) }},
	)
	assertCanceledCandidates(t, servers, authorities, skipped, err, cause)
}

func TestCandidatesCancellationBeforeSuccessReturnsNoPartialResult(t *testing.T) {
	withWorkingDirectory(t, t.TempDir())
	writeCancellationMCPConfig(t)
	cause := errors.New("cancel before MCP candidate success")
	ctx, cancel := context.WithCancelCause(t.Context())

	servers, authorities, skipped, err := candidatesWithHooks(
		ctx,
		target.TargetClaudeCode,
		target.ScopeProject,
		candidateHooks{beforeSuccess: func() { cancel(cause) }},
	)
	assertCanceledCandidates(t, servers, authorities, skipped, err, cause)
}

func writeCancellationMCPConfig(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(
		aggregate.ClaudeProjectMCPConfigPath,
		[]byte(`{"mcpServers":{"context7":{"type":"stdio","command":"node","args":["server.js"]}}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
}

func assertCanceledCandidates(
	t *testing.T,
	servers []adopt.MCPServer,
	authorities []adopt.MCPSourceAuthority,
	skipped []adopt.Skipped,
	err error,
	cause error,
) {
	t.Helper()
	if !errors.Is(err, cause) || servers != nil || authorities != nil || skipped != nil {
		t.Fatalf(
			"Candidates = (%#v, %#v, %#v, %v), want nil result axes and cause %v",
			servers,
			authorities,
			skipped,
			err,
			cause,
		)
	}
}
