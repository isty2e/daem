package cli_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestPiNativeMCPNamespaceLockAdmissionPreservesHostFiles(t *testing.T) {
	for _, test := range []struct {
		name       string
		globalName string
		localName  string
		wantError  bool
	}{
		{"normalized collision", "foo-bar", "foo_bar", true},
		{"exact-name override", "foo-bar", "foo-bar", false},
		{"distinct names", "foo-bar", "foo_baz", false},
	} {
		for _, reversed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reversed=%t", test.name, reversed), func(t *testing.T) {
				project := newMCPCLIProject(t)
				t.Setenv("PATH", t.TempDir())
				testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n")
				runMCPLock(t, project)
				priorLock := string(testkit.ReadFile(t, project.lockfilePath))
				config := `{"unmanaged":true,"mcpServers":{"manual":{"command":"echo"}}}`
				projectPath := filepath.Join(project.root, aggregate.PiProjectMCPConfigPath)
				globalPath := filepath.Join(project.root, "home", ".pi", "agent", "mcp.json")
				testkit.WriteFile(t, project.root, aggregate.PiProjectMCPConfigPath, config)
				testkit.WriteFile(t, project.root, "home/.pi/agent/mcp.json", config)

				binding := func(name, scope string) string {
					return fmt.Sprintf("\n[[mcp_server]]\nname = %q\ntargets = [\"pi\"]\nscope = %q\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = \"node\"\n", name, scope)
				}
				global := binding(test.globalName, "global")
				local := binding(test.localName, "project")
				entries := global + local
				if reversed {
					entries = local + global
				}
				testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"pi\"]\n"+entries)
				exit, stdout, stderr := runMCPCLI(t, "lock", "--manifest", project.manifestPath)
				if (exit != 0) != test.wantError {
					t.Fatalf("lock = %d, want rejection=%t: %s/%s", exit, test.wantError, stdout, stderr)
				}
				testkit.AssertFileContent(t, projectPath, config)
				testkit.AssertFileContent(t, globalPath, config)
				if test.wantError {
					testkit.AssertFileContent(t, project.lockfilePath, priorLock)
					return
				}
				locked, err := lockfile.Load(t.Context(), project.lockfilePath)
				if err != nil || locked.Locked.Len() != 2 {
					t.Fatalf("admitted scoped bindings = %d, %v", locked.Locked.Len(), err)
				}
			})
		}
	}
}
