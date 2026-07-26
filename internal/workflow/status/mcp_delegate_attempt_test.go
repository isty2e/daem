package status

import (
	"context"
	"path/filepath"
	"testing"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestRunReportsDelegateAttemptStaleWhenLaunchIdentityChanges(t *testing.T) {
	tests := []struct {
		name             string
		oldManifest      string
		newManifest      string
		currentCanonical func(*testing.T) []byte
	}{
		{
			name: "command and args",
			oldManifest: `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`,
			newManifest: `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "node"
args = ["server.js"]
`,
			currentCanonical: func(t *testing.T) []byte {
				t.Helper()
				return canonicalStatusMCPEntryWithArgs(t, "context7", "node", []string{"server.js"})
			},
		},
		{
			name: "env refs",
			oldManifest: `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "node"
args = ["server.js"]
env = { API_TOKEN = { from_env = "OLD_CONTEXT7_TOKEN" } }
`,
			newManifest: `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "node"
args = ["server.js"]
env = { API_TOKEN = { from_env = "NEW_CONTEXT7_TOKEN" } }
`,
			currentCanonical: func(t *testing.T) []byte {
				t.Helper()
				return canonicalStatusMCPEntryWithEnv(t, "context7", "node", []string{"server.js"}, map[string]string{
					"API_TOKEN": "${NEW_CONTEXT7_TOKEN}",
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			writeTestFile(t, tempDir, "daem.toml", test.oldManifest)
			if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
				t.Fatalf("old RunLock returned error: %v", err)
			}
			oldRecord := lockedStatusMCPRecord(t, filepath.Join(tempDir, "daem.lock.toml"), "context7")
			oldIdentity, ok := oldRecord.DelegatePlanIdentity()
			if !ok {
				t.Fatal("old locked MCP record missing delegate plan identity")
			}

			writeTestFile(t, tempDir, "daem.toml", test.newManifest)
			if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
				t.Fatalf("new RunLock returned error: %v", err)
			}
			newRecord := lockedStatusMCPRecord(t, filepath.Join(tempDir, "daem.lock.toml"), "context7")
			newIdentity, ok := newRecord.DelegatePlanIdentity()
			if !ok {
				t.Fatal("new locked MCP record missing delegate plan identity")
			}
			if oldIdentity.IdentityKey == newIdentity.IdentityKey {
				t.Fatalf("delegate identity key did not change after %s changed", test.name)
			}

			canonical := test.currentCanonical(t)
			writeTestFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, `{"mcpServers":{"context7":`+string(canonical)+`}}`)
			writeStatusStatefile(
				t,
				filepath.Join(tempDir, ".daem", "state.json"),
				statusMCPStateSnapshot(
					t,
					aggregate.MCPPlacementClaudeProject,
					"context7",
					canonical,
					statusLastDelegateAttempt(
						t,
						newRecord.SubjectID(),
						target.TargetClaudeCode,
						target.ScopeProject,
						oldIdentity.IdentityKey,
						durableattempt.DelegateStatusSucceeded,
						durableattempt.DelegateReasonNone,
					),
				),
			)

			result, err := Run(context.Background(), CommandInput{
				ManifestPath: manifestPath,
				TargetValues: []string{string(target.TargetClaudeCode)},
			})
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			assertStatusMCPDimension(t, result.MCPProjections, "context7", "project_projection", "projected", "")
			assertStatusMCPDimension(t, result.MCPProjections, "context7", "delegate_last_attempt", "stale", string(mcpobserve.ReasonLastDelegateAttemptStale))
		})
	}
}

func canonicalStatusMCPEntryWithEnv(t *testing.T, serverID string, command string, args []string, env map[string]string) []byte {
	t.Helper()
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
		ServerID:        serverID,
		Command:         command,
		Args:            append([]string(nil), args...),
		Env:             env,
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	return canonical
}
