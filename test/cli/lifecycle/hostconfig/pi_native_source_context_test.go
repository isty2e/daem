package cli_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativeAliasedRootsRefuseBeforeQualificationAndPublication(t *testing.T) {
	for _, scope := range []string{"global", "project"} {
		t.Run(scope, func(t *testing.T) {
			project := newMCPCLIProject(t)
			log := installNativeVersionFixture(t, project.root, "1.0.2")
			t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(project.root, ".pi"))
			manifest := fmt.Sprintf("version = 1\ntargets = [\"pi\"]\n\n[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = %q\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = \"node\"\n", scope)
			config := `{"unmanaged":true,"mcpServers":{"manual":{"command":"echo"}}}`
			testkit.WriteFile(t, project.root, "daem.toml", manifest)
			testkit.WriteFile(t, project.root, ".pi/mcp.json", config)
			runMCPLock(t, project)
			beforeLock := string(testkit.ReadFile(t, project.lockfilePath))

			exit, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes")
			if exit == 0 {
				t.Fatalf("aliased-root apply was accepted: %s/%s", stdout, stderr)
			}
			testkit.AssertFileContent(t, filepath.Join(project.root, ".pi/mcp.json"), config)
			testkit.AssertFileContent(t, project.lockfilePath, beforeLock)
			testkit.AssertPathMissing(t, log)
			testkit.AssertPathMissing(t, filepath.Join(project.root, ".daem", "state.json"))
		})
	}
}
