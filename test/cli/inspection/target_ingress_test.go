package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestTargetSyntaxFailsAtCLIIngressAcrossCommandFamilies(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "status human", args: []string{"status", "--target", "unknown"}},
		{name: "status JSON", args: []string{"status", "--target", "unknown", "--json"}},
		{name: "apply human", args: []string{"apply", "--target", "unknown", "--dry-run"}},
		{name: "apply JSON", args: []string{"apply", "--target", "unknown", "--dry-run", "--json"}},
		{name: "doctor", args: []string{"doctor", "--target", "unknown", "--json"}},
		{name: "probe", args: []string{"probe", "mcp-server", "example", "--target", "unknown", "--dry-run", "--json"}},
		{name: "import", args: []string{"import", "--target", "unknown", "--dry-run", "--json"}},
		{name: "list resources", args: []string{"list", "resources", "--target", "unknown", "--json"}},
		{name: "list outputs", args: []string{"list", "outputs", "--target", "unknown", "--json"}},
		{name: "list paths", args: []string{"list", "paths", "--target", "unknown", "--json"}},
		{name: "add", args: []string{"add", "skill", "owner/repo", "--target", "unknown", "--dry-run", "--json"}},
		{name: "remove", args: []string{"remove", "skill", "example", "--target", "unknown", "--dry-run", "--json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			testkit.WithWorkingDirectory(t, root)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunCLI(test.args, &stdout, &stderr)
			if exitCode != 2 {
				t.Fatalf("exitCode = %d, want 2, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), `unknown target "unknown"`) {
				t.Fatalf("stderr = %q, want unknown target diagnostic", stderr.String())
			}
			for _, forbidden := range []string{"invalid manifest", "no such file or directory"} {
				if strings.Contains(stderr.String(), forbidden) {
					t.Fatalf("stderr = %q, target misuse reached workflow failure %q", stderr.String(), forbidden)
				}
			}
		})
	}
}

func TestStatusRejectsMalformedTargetTokensBeforeManifestSelection(t *testing.T) {
	for _, targetValue := range []string{"", " ", " codex", "codex ", "codex,claude-code", "CODEX", "codex/claude-code"} {
		for _, jsonMode := range []bool{false, true} {
			name := targetValue
			if name == "" {
				name = "empty"
			}
			if jsonMode {
				name += " JSON"
			}
			t.Run(name, func(t *testing.T) {
				args := []string{"status", "--target", targetValue}
				if jsonMode {
					args = append(args, "--json")
				}
				var stdout bytes.Buffer
				var stderr bytes.Buffer
				exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
				if exitCode != 2 || stdout.Len() != 0 {
					t.Fatalf("exitCode = %d, stdout = %q, stderr = %q; want misuse with empty stdout", exitCode, stdout.String(), stderr.String())
				}
				if !strings.Contains(stderr.String(), "unknown target") {
					t.Fatalf("stderr = %q, want target diagnostic", stderr.String())
				}
			})
		}
	}
}

func TestTargetIngressRejectsMissingAndExcessDistinctValues(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing human value", args: []string{"status", "--target"}, want: "flag needs an argument"},
		{name: "missing JSON value", args: []string{"status", "--json", "--target"}, want: "flag needs an argument"},
		{
			name: "probe distinct cardinality",
			args: []string{
				"probe", "mcp-server", "example", "--dry-run", "--json",
				"--target", "codex", "--target", "claude-code",
			},
			want: "probe accepts at most one distinct --target",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunCLI(test.args, &stdout, &stderr)
			if exitCode != 2 || stdout.Len() != 0 {
				t.Fatalf("exitCode = %d, stdout = %q, stderr = %q; want misuse with empty stdout", exitCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
		})
	}
}

func TestTargetIngressPreservesDuplicateAndUnavailableSemantics(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	testkit.WithWorkingDirectory(t, root)
	manifestPath := filepath.Join(root, "daem.toml")
	testkit.WriteFile(t, root, "instructions.md", "project instructions\n")
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "instructions.md"

[instructions.project.target.codex]
render_to = "AGENTS.md"
`)

	t.Run("duplicate canonical target", func(t *testing.T) {
		var singleStdout bytes.Buffer
		var singleStderr bytes.Buffer
		singleExitCode := testkit.RunVerboseCLI([]string{
			"status", "--manifest", manifestPath, "--target", "codex", "--json",
		}, &singleStdout, &singleStderr)

		var duplicateStdout bytes.Buffer
		var duplicateStderr bytes.Buffer
		duplicateExitCode := testkit.RunVerboseCLI([]string{
			"status", "--manifest", manifestPath,
			"--target", "codex", "--target", "codex", "--json",
		}, &duplicateStdout, &duplicateStderr)
		if duplicateExitCode != singleExitCode || duplicateStdout.String() != singleStdout.String() || duplicateStderr.String() != singleStderr.String() {
			t.Fatalf(
				"duplicate result = (%d, %q, %q), single result = (%d, %q, %q)",
				duplicateExitCode,
				duplicateStdout.String(),
				duplicateStderr.String(),
				singleExitCode,
				singleStdout.String(),
				singleStderr.String(),
			)
		}
		if duplicateExitCode != 0 || duplicateStderr.Len() != 0 || !json.Valid(duplicateStdout.Bytes()) {
			t.Fatalf("exitCode = %d, stdout = %q, stderr = %q; want one valid result", duplicateExitCode, duplicateStdout.String(), duplicateStderr.String())
		}
	})

	t.Run("supported but unavailable target", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := testkit.RunVerboseCLI([]string{
			"status", "--manifest", manifestPath, "--target", "claude-code", "--json",
		}, &stdout, &stderr)
		if exitCode != 1 || stdout.Len() != 0 {
			t.Fatalf("exitCode = %d, stdout = %q, stderr = %q; want operational failure", exitCode, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), `target "claude-code" does not match any manifest resource`) {
			t.Fatalf("stderr = %q, want availability diagnostic", stderr.String())
		}
		if strings.Contains(stderr.String(), "invalid value") {
			t.Fatalf("stderr = %q, supported target was misclassified as syntax", stderr.String())
		}
	})

	t.Run("list resources supported but unavailable target", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := testkit.RunVerboseCLI([]string{
			"list", "resources", "--manifest", manifestPath, "--target", "claude-code", "--json",
		}, &stdout, &stderr)
		if exitCode != 1 || stdout.Len() != 0 {
			t.Fatalf("exitCode = %d, stdout = %q, stderr = %q; want operational failure", exitCode, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), `target "claude-code" does not match any manifest resource`) {
			t.Fatalf("stderr = %q, want availability diagnostic", stderr.String())
		}
		if strings.Contains(stderr.String(), "invalid value") {
			t.Fatalf("stderr = %q, supported target was misclassified as syntax", stderr.String())
		}
	})

	t.Run("list paths supported but unavailable target", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := testkit.RunVerboseCLI([]string{
			"list", "paths", "--manifest", manifestPath, "--target", "claude-code", "--json",
		}, &stdout, &stderr)
		if exitCode != 1 || stdout.Len() != 0 {
			t.Fatalf("exitCode = %d, stdout = %q, stderr = %q; want operational failure", exitCode, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), `target "claude-code" does not match any manifest resource`) {
			t.Fatalf("stderr = %q, want availability diagnostic", stderr.String())
		}
	})
}
