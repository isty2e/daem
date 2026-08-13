package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockRejectsFutureExecutableLifecycleSyntaxWithoutWritingLockfile(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name: "command local parameter object",
			manifest: `
version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = { local_parameter = "context7_runner" }
args = ["--stdio"]
`,
			want: "command",
		},
		{
			name: "local parameter table",
			manifest: `
version = 1
targets = ["claude-code"]

[[local_parameter]]
name = "context7_runner"
kind = "project_executable"
path = "tools/context7-server"
executable = true
`,
			want: "unknown manifest key",
		},
		{
			name: "package runner table",
			manifest: `
version = 1
targets = ["claude-code"]

[[package_runner]]
name = "context7_runner"
family = "npm"
runner = "npx"
package = "@upstash/context7-mcp"
version = "1.2.3"
`,
			want: "unknown manifest key",
		},
		{
			name: "executable artifact table",
			manifest: `
version = 1
targets = ["claude-code"]

[[executable_artifact]]
name = "context7_runner"
source = { git = "https://example.invalid/context7.git", ref = "0123456789abcdef" }
path = "dist/context7-server"
digest = "sha256:abc123"
`,
			want: "unknown manifest key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			if err := os.WriteFile(manifestPath, []byte(test.manifest), 0o600); err != nil {
				t.Fatalf("WriteFile manifest returned error: %v", err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
			if _, err := os.Stat(lockfilePath); !os.IsNotExist(err) {
				t.Fatalf("lockfile stat err = %v, want missing lockfile", err)
			}
		})
	}
}

func TestRunLockApplyAndStatusPreserveExplicitAbsoluteMCPCommandPath(t *testing.T) {
	project := newMCPCLIProject(t)
	homeDir := filepath.Join(project.root, "home")
	t.Setenv("HOME", homeDir)
	absolutePath := filepath.Join(project.root, "missing executable with spaces", "codegraph;literal")
	if _, err := os.Stat(absolutePath); !os.IsNotExist(err) {
		t.Fatalf("absolute command path stat error = %v, want missing prerequisite", err)
	}
	testkit.WriteFile(t, project.root, "daem.toml", `version = 1
targets = ["antigravity-cli"]

[[mcp_server]]
name = "codegraph"
targets = ["antigravity-cli"]
scope = "global"
transport = "stdio"
command = { path = `+strconv.Quote(absolutePath)+` }
args = ["serve", "--mcp"]
`)

	runMCPLock(t, project)
	locked, err := lockfile.Load(t.Context(), project.lockfilePath)
	if err != nil {
		t.Fatalf("Load lockfile returned error: %v", err)
	}
	subjects := locked.Locked.Subjects()
	if len(subjects) != 1 {
		t.Fatalf("locked subjects = %#v, want one MCP projection", subjects)
	}
	if _, present := subjects[0].DelegatePlan(); present {
		t.Fatal("Antigravity absolute command unexpectedly gained delegated execution authority")
	}
	contribution := testkit.LockedManagedAggregateContribution(t, subjects[0])
	if !strings.Contains(contribution.CanonicalContribution(), absolutePath) {
		t.Fatalf("locked canonical contribution = %q, want exact path %q", contribution.CanonicalContribution(), absolutePath)
	}

	exitCode, stdout, stderr := runMCPCLI(
		t,
		"apply",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--yes",
		"--json",
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(absolutePath); !os.IsNotExist(err) {
		t.Fatalf("apply changed missing prerequisite path: %v", err)
	}

	hostConfigPath := filepath.Join(homeDir, ".gemini", "config", "mcp_config.json")
	entry, present, err := mcpcodec.ExtractAntigravityGlobalMCPServerProjection(
		testkit.ReadFile(t, hostConfigPath),
		"codegraph",
	)
	if err != nil {
		t.Fatalf("ExtractAntigravityGlobalMCPServerProjection returned error: %v", err)
	}
	if !present || entry.Command != absolutePath {
		t.Fatalf("host entry = %#v, present=%t, want exact path %q", entry, present, absolutePath)
	}
	runMCPCLIExpect(
		t,
		0,
		"status converged absolute command",
		"status",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--check",
		"--json",
	)

	otherPath := filepath.Join(project.root, "other", "codegraph")
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), `{
  "mcpServers": {
    "codegraph": {
      "command": `+strconv.Quote(otherPath)+`,
      "args": ["serve", "--mcp"]
    }
  }
}`)
	runMCPCLIExpect(
		t,
		1,
		"status detects changed absolute command",
		"status",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--check",
		"--json",
	)
}
