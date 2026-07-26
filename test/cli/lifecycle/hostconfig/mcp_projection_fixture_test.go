package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/test/testkit"
)

type mcpCLIProject struct {
	root         string
	manifestPath string
	lockfilePath string
}

type mcpManifestSpec struct {
	Target  string
	Scope   string
	Command string
	Args    []string
	Env     map[string]string
}

func newMCPCLIProject(t *testing.T) mcpCLIProject {
	t.Helper()
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	return mcpCLIProject{
		root:         root,
		manifestPath: filepath.Join(root, "daem.toml"),
		lockfilePath: filepath.Join(root, "daem.lock.toml"),
	}
}

func writeMCPManifest(t *testing.T, root string, spec mcpManifestSpec) {
	t.Helper()
	args, err := json.Marshal(spec.Args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	selectedTarget := spec.Target
	if selectedTarget == "" {
		selectedTarget = "claude-code"
	}
	var scope string
	if spec.Scope != "" {
		scope = "scope = \"" + spec.Scope + "\"\n"
	}
	var env string
	if len(spec.Env) != 0 {
		envParts := make([]string, 0, len(spec.Env))
		for key, fromEnv := range spec.Env {
			envParts = append(envParts, key+` = { from_env = "`+fromEnv+`" }`)
		}
		env = "\nenv = { " + strings.Join(envParts, ", ") + " }"
	}
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["`+selectedTarget+`"]

[[mcp_server]]
name = "context7"
`+scope+`
transport = "stdio"
command = "`+spec.Command+`"
args = `+string(args)+env+`
`)
}

func writeMCPManifestWithoutServers(t *testing.T, root string) {
	t.Helper()
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["claude-code"]
`)
}

func runMCPLock(t *testing.T, project mcpCLIProject) {
	t.Helper()
	exitCode, stdout, stderr := runMCPCLI(t, "lock", "--manifest", project.manifestPath)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

func runMCPCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
	return exitCode, stdout.String(), stderr.String()
}

func writeMCPConfigWithSibling(t *testing.T, root string, context7Entry string) {
	t.Helper()
	entries := []string{
		`"manual":{"type":"stdio","command":"node","args":["manual.js"],"env":{}}`,
	}
	if context7Entry != "" {
		entries = append([]string{context7Entry}, entries...)
	}
	testkit.WriteFile(t, root, aggregate.ClaudeProjectMCPConfigPath, `{
  "project": "keep",
  "hostTrustResidue": {"context7": "leave-alone"},
  "mcpServers": {`+strings.Join(entries, ",")+`}
}`)
}
