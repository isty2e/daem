package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
