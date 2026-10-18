package cli_test

import (
	"fmt"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/execcheck"
)

func TestMCPProbeSelectionFailuresUsePublicResourceNames(t *testing.T) {
	for _, test := range []struct {
		name      string
		targets   []string
		query     string
		selectors []string
		detail    string
	}{
		{
			name: "missing literal name", targets: []string{"claude-code"},
			query:  "instructions/mcp_server:%25",
			detail: "has no admitted runtime-probe row matching target= scope=",
		},
		{
			name: "target mismatch", targets: []string{"claude-code"}, query: "context7",
			selectors: []string{"--target", "opencode", "--scope", "project"},
			detail:    "has no admitted runtime-probe row matching target=opencode scope=project",
		},
		{
			name: "ambiguous", targets: []string{"claude-code", "opencode"}, query: "context7",
			detail: "is ambiguous across 2 runtime-probe rows; narrow with --target and --scope",
		},
		{
			name: "single rejected binding", targets: []string{"codex"}, query: "context7",
			detail: "runtime-probe admission: target=codex scope=project is not admitted for runtime probe",
		},
		{
			name: "multiple rejected bindings", targets: []string{"codex", "antigravity-cli"}, query: "context7",
			detail: "has no admitted runtime-probe row matching target= scope=",
		},
	} {
		for _, mode := range []string{"--dry-run", "--yes"} {
			for _, jsonOutput := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/json=%t", test.name, mode, jsonOutput), func(t *testing.T) {
					canary := execcheck.New(t, "must-not-run-probe-selection")
					project := newMCPCLIProject(t)
					var manifest strings.Builder
					manifest.WriteString("version = 1\ntargets = [\"claude-code\"]\n")
					for _, selectedTarget := range test.targets {
						fmt.Fprintf(&manifest, `

[[mcp_server]]
name = "context7"
targets = [%q]
transport = "stdio"
command = "must-not-run-probe-selection"
args = []
`, selectedTarget)
					}
					testkit.WriteFile(t, project.root, "daem.toml", manifest.String())
					executor := &fakeCLIRuntimeProbeExecutor{}
					args := []string{"probe", "mcp-server", test.query, "--manifest", project.manifestPath, mode}
					args = append(args, test.selectors...)
					if jsonOutput {
						args = append(args, "--json")
					}
					exitCode, stdout, stderr := runMCPCLIWithOptions(t, args, clipkg.RunOptions{ProbeExecutor: executor})
					diagnostic := clijson.FailureText(t, stdout, stderr, "probe mcp-server", "operation", jsonOutput)
					want := fmt.Sprintf("probe failed: mcp-server %q %s", test.query, test.detail)
					if exitCode != 1 || !strings.Contains(diagnostic, want) {
						t.Fatalf("probe failure = exit %d, %q, want exit 1 and %q", exitCode, diagnostic, want)
					}
					if executor.called {
						t.Fatal("selection failure invoked the probe executor")
					}
					execcheck.AssertClean(t, canary, "probe selection failure")
				})
			}
		}
	}
}
