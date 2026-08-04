package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestFlagSyntaxErrorsUseNearestScopedCorrection(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		helpPath string
	}{
		{name: "init", args: []string{"init", "--unknown"}, helpPath: "init"},
		{name: "import", args: []string{"import", "--unknown"}, helpPath: "import"},
		{name: "lock", args: []string{"lock", "--unknown"}, helpPath: "lock"},
		{name: "outdated", args: []string{"outdated", "--unknown"}, helpPath: "outdated"},
		{name: "list resources", args: []string{"list", "resources", "--unknown"}, helpPath: "list resources"},
		{name: "list outputs", args: []string{"list", "outputs", "--unknown"}, helpPath: "list outputs"},
		{name: "list paths", args: []string{"list", "paths", "--unknown"}, helpPath: "list paths"},
		{name: "status", args: []string{"status", "--unknown"}, helpPath: "status"},
		{name: "doctor", args: []string{"doctor", "--unknown"}, helpPath: "doctor"},
		{name: "probe mcp-server", args: []string{"probe", "mcp-server", "example", "--unknown"}, helpPath: "probe mcp-server"},
		{name: "apply", args: []string{"apply", "--unknown"}, helpPath: "apply"},
		{name: "recover", args: []string{"recover", "--unknown"}, helpPath: "recover"},
		{name: "add extension", args: []string{"add", "extension", "example", "source", "--unknown"}, helpPath: "add extension"},
		{name: "add instruction", args: []string{"add", "instruction", "example", "source", "--unknown"}, helpPath: "add instruction"},
		{name: "add hook", args: []string{"add", "hook", "example", "event", "command", "--unknown"}, helpPath: "add hook"},
		{name: "add mcp-server", args: []string{"add", "mcp-server", "example", "command", "--unknown"}, helpPath: "add mcp-server"},
		{name: "add skill", args: []string{"add", "skill", "owner/repo", "--unknown"}, helpPath: "add skill"},
		{name: "add skill-group", args: []string{"add", "skill-group", "owner/repo", "--member", "example", "--unknown"}, helpPath: "add skill-group"},
		{name: "remove extension", args: []string{"remove", "extension", "example", "--unknown"}, helpPath: "remove extension"},
		{name: "remove instruction", args: []string{"remove", "instruction", "example", "--unknown"}, helpPath: "remove instruction"},
		{name: "remove hook", args: []string{"remove", "hook", "example", "--unknown"}, helpPath: "remove hook"},
		{name: "remove mcp-server", args: []string{"remove", "mcp-server", "example", "--unknown"}, helpPath: "remove mcp-server"},
		{name: "remove skill", args: []string{"remove", "skill", "example", "--unknown"}, helpPath: "remove skill"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertScopedFlagCorrection(t, test.args, test.helpPath, "flag provided but not defined")
		})
	}
}

func TestFlagValueErrorsPreserveMisuseStreamsAndScopedCorrection(t *testing.T) {
	for _, test := range []struct {
		name        string
		args        []string
		helpPath    string
		wantProblem string
	}{
		{
			name:        "custom target value in JSON invocation",
			args:        []string{"status", "--json", "--target", "unknown"},
			helpPath:    "status",
			wantProblem: `unknown target "unknown"`,
		},
		{
			name:        "custom scope value in JSON invocation",
			args:        []string{"import", "--json", "--scope", "unknown"},
			helpPath:    "import",
			wantProblem: `unknown scope "unknown" (accepted scopes: global, project)`,
		},
		{
			name:        "standard missing value",
			args:        []string{"status", "--target"},
			helpPath:    "status",
			wantProblem: "flag needs an argument",
		},
		{
			name:        "invalid boolean value",
			args:        []string{"status", "--json=maybe"},
			helpPath:    "status",
			wantProblem: "invalid boolean value",
		},
		{
			name:        "authoring pre-parser missing value",
			args:        []string{"add", "skill", "owner/repo", "--target"},
			helpPath:    "add skill",
			wantProblem: "flag needs an argument",
		},
		{
			name:        "bad long flag syntax",
			args:        []string{"status", "---bad"},
			helpPath:    "status",
			wantProblem: "bad flag syntax",
		},
		{
			name:        "next flag consumed as custom value",
			args:        []string{"status", "--target", "--verbose"},
			helpPath:    "status",
			wantProblem: `unknown target "--verbose"`,
		},
		{
			name:        "next flag consumed as scope value",
			args:        []string{"import", "--scope", "--verbose"},
			helpPath:    "import",
			wantProblem: `unknown scope "--verbose"`,
		},
		{
			name:        "empty inline custom value",
			args:        []string{"status", "--target="},
			helpPath:    "status",
			wantProblem: `unknown target ""`,
		},
		{
			name:        "empty inline scope value",
			args:        []string{"import", "--scope="},
			helpPath:    "import",
			wantProblem: `unknown scope ""`,
		},
		{
			name:        "comma joined custom value",
			args:        []string{"status", "--target=codex,claude-code"},
			helpPath:    "status",
			wantProblem: `unknown target "codex,claude-code"`,
		},
		{
			name:        "comma joined scope value",
			args:        []string{"import", "--scope=project,global"},
			helpPath:    "import",
			wantProblem: `unknown scope "project,global"`,
		},
		{
			name:        "authoring inline misspelling",
			args:        []string{"add", "skill", "owner/repo", "--unknown=value"},
			helpPath:    "add skill",
			wantProblem: "flag provided but not defined",
		},
		{
			name:        "authoring short misspelling",
			args:        []string{"remove", "skill", "example", "-x"},
			helpPath:    "remove skill",
			wantProblem: "flag provided but not defined",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertScopedFlagCorrection(t, test.args, test.helpPath, test.wantProblem)
		})
	}
}

func TestFlagCorrectionPolicyDoesNotReplaceExplicitHelpOrSpecializedCorrection(t *testing.T) {
	for _, test := range []struct {
		name      string
		args      []string
		wantUsage string
	}{
		{name: "root leaf help", args: []string{"status", "--help"}, wantUsage: "Usage: daem status"},
		{name: "grouped leaf help", args: []string{"add", "skill", "--help"}, wantUsage: "Usage: daem add skill"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunCLI(test.args, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), test.wantUsage) {
				t.Fatalf("exitCode = %d, stdout = %q, stderr = %q; want successful scoped help", exitCode, stdout.String(), stderr.String())
			}
		})
	}

	t.Run("specialized target-list correction remains singular", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := testkit.RunCLI([]string{
			"add", "skill", "owner/repo", "--target", "codex", "claude-code",
		}, &stdout, &stderr)
		if exitCode != 2 || stdout.Len() != 0 || strings.Count(stderr.String(), "next:") != 1 {
			t.Fatalf("exitCode = %d, stdout = %q, stderr = %q; want one specialized correction", exitCode, stdout.String(), stderr.String())
		}
		if strings.Contains(stderr.String(), "next: run daem help") {
			t.Fatalf("stderr = %q, generic correction duplicated specialized guidance", stderr.String())
		}
	})
}

func assertScopedFlagCorrection(t *testing.T, args []string, helpPath string, wantProblem string) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunCLI(args, &stdout, &stderr)
	if exitCode != 2 || stdout.Len() != 0 {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q; want misuse with empty stdout", exitCode, stdout.String(), stderr.String())
	}

	output := stderr.String()
	if !strings.Contains(output, wantProblem) {
		t.Fatalf("stderr = %q, want problem containing %q", output, wantProblem)
	}
	correction := "next: run daem help " + helpPath + "\n"
	if strings.Count(output, correction) != 1 || !strings.HasSuffix(output, correction) {
		t.Fatalf("stderr = %q, want one terminal correction %q", output, correction)
	}
	for _, forbidden := range []string{"Usage of ", "\n  -manifest", "invalid manifest", "no such file or directory"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("stderr = %q, contains forbidden parse-failure output %q", output, forbidden)
		}
	}
}
