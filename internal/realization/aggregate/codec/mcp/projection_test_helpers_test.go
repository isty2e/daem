package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func validMCPProjection(serverID string) ClaudeProjectMCPServerProjection {
	return ClaudeProjectMCPServerProjection{
		ServerID:        serverID,
		Command:         "npx",
		Args:            []string{},
		Env:             map[string]string{},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	}
}

func validClaudeGlobalMCPProjection(serverID string) ClaudeGlobalMCPServerProjection {
	return ClaudeGlobalMCPServerProjection{
		ServerID:        serverID,
		Command:         "npx",
		Args:            []string{},
		Env:             map[string]string{},
		AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
	}
}

func validAntigravityMCPProjection(serverID string) AntigravityGlobalMCPServerProjection {
	return AntigravityGlobalMCPServerProjection{
		ServerID:         serverID,
		Command:          "npx",
		Args:             []string{},
		EnvironmentNames: []string{},
		AdapterContract:  aggregate.AntigravityGlobalMCPAmbientEnvV1,
	}
}

func validOpenCodeMCPProjection(serverID string) MCPNoEnvServerProjection {
	return MCPNoEnvServerProjection{
		ServerID:        serverID,
		Command:         "npx",
		Args:            []string{},
		AdapterContract: aggregate.OpenCodeProjectMCPLocalCommandV1,
	}
}

func validOpenCodeGlobalMCPProjection(serverID string) OpenCodeGlobalMCPServerProjection {
	return OpenCodeGlobalMCPServerProjection{
		ServerID:        serverID,
		Command:         "npx",
		Args:            []string{},
		Environment:     map[string]string{},
		AdapterContract: aggregate.OpenCodeGlobalMCPLocalEnvV1,
	}
}

func validCodexMCPProjection(serverID string) MCPNoEnvServerProjection {
	return MCPNoEnvServerProjection{
		ServerID:        serverID,
		Command:         "npx",
		Args:            []string{},
		AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	}
}

func validCodexGlobalMCPProjection(serverID string) CodexGlobalMCPServerProjection {
	return CodexGlobalMCPServerProjection{
		ServerID:        serverID,
		Command:         "npx",
		Args:            []string{},
		EnvVars:         []string{},
		AdapterContract: aggregate.CodexGlobalMCPStdioEnvVarsV1,
	}
}

func assertMCPProjectionReason(t *testing.T, err error, want MCPProjectionReasonCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want reason %q", want)
	}
	got, ok := MCPProjectionReasonCodeOf(err)
	if !ok || got != want {
		t.Fatalf("error = %v, reason = %q/%t, want %q", err, got, ok, want)
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}
