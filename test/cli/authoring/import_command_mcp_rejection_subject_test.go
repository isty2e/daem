package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunImportDoesNotDiscloseRejectedMCPIdentifierInHumanOrJSONOutput(t *testing.T) {
	const canary = "MCP_REJECTION_SUBJECT_LEAK_CANARY"
	for _, jsonOutput := range []bool{false, true} {
		name := "human"
		if jsonOutput {
			name = "JSON"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			testkit.WithWorkingDirectory(t, root)
			outputPath := filepath.Join(root, "daem.toml")
			testkit.WriteFile(t, root, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "token=`+canary+`/path": {"type": "http", "command": "node"},
    "valid": {"type": "stdio", "command": "node"}
  }
}`)

			args := []string{"import", "--target", "claude-code", "--manifest", outputPath, "--dry-run"}
			if jsonOutput {
				args = append(args, "--json")
			}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("import exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String(), canary) {
				t.Fatalf("public output disclosed rejected identifier: %q", stdout.String())
			}
			wantLivePath := aggregate.ClaudeProjectMCPConfigPath + "#/mcpServers"
			if !jsonOutput {
				want := `skip live="` + wantLivePath + `" reason=projection_equivalence_undefined`
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("human output = %q, want %q", stdout.String(), want)
				}
				return
			}
			payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
			found := false
			for _, skipped := range payload.Skipped {
				if skipped.LivePath == wantLivePath && skipped.Reason == "projection_equivalence_undefined" {
					found = true
				}
			}
			if !found {
				t.Fatalf("JSON skipped = %#v, want safe parent subject", payload.Skipped)
			}
		})
	}
}
