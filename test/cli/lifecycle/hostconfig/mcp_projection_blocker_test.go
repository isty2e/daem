package cli_test

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestMCPPublicCLIBlocksProjectionProblemStates(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		state     string
		reason    string
		wantCheck bool
	}{
		{
			name:      "malformed",
			config:    `{"mcpServers":`,
			state:     "malformed",
			reason:    "CONFIG_MALFORMED",
			wantCheck: true,
		},
		{
			name:      "unsupported managed field",
			config:    `{"mcpServers":{"context7":{"type":"stdio","command":"npx","args":[],"env":{},"cwd":"/tmp"}}}`,
			state:     "unsupported",
			reason:    "UNSUPPORTED_MANAGED_FIELD",
			wantCheck: true,
		},
		{
			name:      "unmanaged same name",
			config:    `{"mcpServers":{"context7":{"type":"stdio","command":"node","args":["server.js"],"env":{}}}}`,
			state:     "unmanaged_same_name",
			reason:    "ROUTE_PREEXISTING_UNOWNED",
			wantCheck: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			writeMCPManifest(t, project.root, mcpManifestSpec{
				Command: "must-not-run-daem-test",
				Args:    []string{"--serve", "context7"},
			})
			runMCPLock(t, project)
			testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, test.config)

			exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--dry-run", "--json")
			if exitCode != 1 || stderr != "" {
				t.Fatalf("apply dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			applyPayload := clijson.DecodePlan(t, []byte(stdout))
			assertMCPJSONDimension(t, applyPayload, "project_projection", test.state, test.reason)
			assertNoPublicMCPOutputLeaks(t, stdout)

			exitCode, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--target", "claude-code", "--check", "--json")
			if test.wantCheck && exitCode != 1 {
				t.Fatalf("status check exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			if stderr != "" {
				t.Fatalf("status stderr = %q, want empty structured output", stderr)
			}
			statusPayload := clijson.DecodePlan(t, []byte(stdout))
			assertMCPJSONDimension(t, statusPayload, "project_projection", test.state, test.reason)
			assertNoPublicMCPOutputLeaks(t, stdout)
		})
	}
}
