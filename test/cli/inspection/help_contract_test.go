package cli_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
)

type helpCase struct {
	topic      []string
	usage      string
	lineBudget int
	required   []string
	forbidden  []string
}

func TestHelpContractCoversEveryRetainedCommandAtSupportedWidths(t *testing.T) {
	cases := []helpCase{
		{topic: nil, usage: "Usage: daem <command> [options]", lineBudget: 30, required: []string{"First project: daem init", "Existing setup: daem import --target <target>", "More help: daem help <command>"}, forbidden: []string{"Options:", "--manifest", "--json"}},
		{topic: []string{"add"}, usage: "Usage: daem add <resource>", lineBudget: 32, required: []string{"extension", "instruction", "hook", "mcp-server", "skill", "skill-group"}, forbidden: []string{"daem add extension <id>", "--member", "--timeout"}},
		{topic: []string{"remove"}, usage: "Usage: daem remove <resource>", lineBudget: 32, required: []string{"extension", "instruction", "hook", "mcp-server", "skill"}, forbidden: []string{"daem remove hook <name>", "--scope"}},
		{topic: []string{"unmanage"}, usage: "Usage: daem unmanage <resource>", lineBudget: 32, required: []string{"extension", "never invokes a host route"}, forbidden: []string{"--yes", "--prune"}},
		{topic: []string{"list"}, usage: "Usage: daem list <resource>", lineBudget: 32, required: []string{"resources", "outputs"}, forbidden: []string{"--inventory"}},
		{topic: []string{"probe"}, usage: "Usage: daem probe <resource>", lineBudget: 32, required: []string{"mcp-server", "terminal stdin/stdout/stderr or --yes"}},
		{topic: []string{"init"}, usage: "Usage: daem init", lineBudget: 46, required: []string{"--force", "entry-identity revalidation", "create ./daem.toml", "daem init --manifest"}, forbidden: []string{"--yes", "--lockfile"}},
		{topic: []string{"import"}, usage: "Usage: daem import --target <target>", lineBudget: 46, required: []string{"--source-dir", "--merge", "At least one --target is required", "--target codex --target claude-code"}, forbidden: []string{"--output", "--yes", "--lockfile"}},
		{topic: []string{"lock"}, usage: "Usage: daem lock", lineBudget: 46, required: []string{"daem.lock.toml beside", "Writes by default", "daem lock --dry-run --verbose"}, forbidden: []string{"--lockfile", "--yes"}},
		{topic: []string{"outdated"}, usage: "Usage: daem outdated", lineBudget: 46, required: []string{"--check", "daem outdated --check"}},
		{topic: []string{"status"}, usage: "Usage: daem status", lineBudget: 46, required: []string{"--check", "every effective target", "--target codex --check --json"}, forbidden: []string{"--inventory", "--scope"}},
		{topic: []string{"apply"}, usage: "Usage: daem apply", lineBudget: 60, required: []string{"--manage-existing", "eligible external carriers", "terminal stdin/stdout/stderr", "Non-interactive apply requires --yes", "ordinary selected apply work"}, forbidden: []string{"--attempt-delegates", "--lockfile"}},
		{topic: []string{"recover"}, usage: "Usage: daem recover", lineBudget: 46, required: []string{"terminal stdin/stdout/stderr", "--json requires --dry-run or --yes"}},
		{topic: []string{"doctor"}, usage: "Usage: daem doctor", lineBudget: 46, required: []string{"--all-targets", "Doctor never launches", "credential helpers"}},
		{topic: []string{"add", "extension"}, usage: "Usage: daem add extension <id> <source>", lineBudget: 46, required: []string{"opaque host source", "Source registries", "--target claude-code"}, forbidden: []string{"--marketplace", "--host-source", "--yes"}},
		{topic: []string{"add", "instruction"}, usage: "Usage: daem add instruction <name> <source>", lineBudget: 46, required: []string{"preserve manifest inheritance", "manifest-only"}},
		{topic: []string{"add", "hook"}, usage: "Usage: daem add hook <name> <event> <command>", lineBudget: 60, required: []string{"--matcher", "--timeout <duration>", "whole seconds", "PostToolUse"}, forbidden: []string{"--command", "--event", "--status-message", "--target-override"}},
		{topic: []string{"add", "mcp-server"}, usage: "Usage: daem add mcp-server <name> <command>", lineBudget: 60, required: []string{"--arg <value>", "duplicates", "Environment mappings", "npx --arg -y", "local-mcp-server"}, forbidden: []string{"./bin/server", "--command", "--env", "--yes"}},
		{topic: []string{"add", "skill"}, usage: "Usage: daem add skill <source>", lineBudget: 60, required: []string{"--path", "--ref", "--name", "Git repository locator"}, forbidden: []string{"--id", "--mode", "--yes"}},
		{topic: []string{"add", "skill-group"}, usage: "Usage: daem add skill-group <source-root> --member <name>", lineBudget: 46, required: []string{"--member <name>", "Duplicates collapse", "--member review --member test"}, forbidden: []string{"--mode", "--name"}},
		{topic: []string{"remove", "extension"}, usage: "Usage: daem remove extension <id>", lineBudget: 46, required: []string{"daem list resources", "ambiguous matches"}},
		{topic: []string{"remove", "instruction"}, usage: "Usage: daem remove instruction <name>", lineBudget: 46, required: []string{"daem list resources", "ambiguous matches"}},
		{topic: []string{"remove", "hook"}, usage: "Usage: daem remove hook <name>", lineBudget: 46, required: []string{"daem list resources", "ambiguous matches"}},
		{topic: []string{"remove", "mcp-server"}, usage: "Usage: daem remove mcp-server <name>", lineBudget: 46, required: []string{"daem list resources", "Carrier uninstall effects"}},
		{topic: []string{"remove", "skill"}, usage: "Usage: daem remove skill <resource-key>", lineBudget: 46, required: []string{"daem list resources", "ambiguous matches"}},
		{topic: []string{"unmanage", "extension"}, usage: "Usage: daem unmanage extension <id>", lineBudget: 46, required: []string{"--target <target>", "--scope <scope>", "always retains host state", "ambient or manual consumers remain unobservable"}, forbidden: []string{"--yes", "--prune", "--uninstall"}},
		{topic: []string{"list", "resources"}, usage: "Usage: daem list resources", lineBudget: 46, required: []string{"stable remove keys", "--target codex --json"}},
		{topic: []string{"list", "outputs"}, usage: "Usage: daem list outputs", lineBudget: 46, required: []string{"ownership inventory", "use daem status"}},
		{topic: []string{"probe", "mcp-server"}, usage: "Usage: daem probe mcp-server <name>", lineBudget: 60, required: []string{"needed only when the name is ambiguous", "--timeout <duration>", "terminal stdin/stdout/stderr", "never mutates manifest"}},
	}

	for _, width := range []int{80, 120} {
		for _, test := range cases {
			name := "root"
			if len(test.topic) != 0 {
				name = strings.Join(test.topic, "_")
			}
			t.Run(name+"_"+strconv.Itoa(width), func(t *testing.T) {
				stdout, stderr, exitCode := runHelpAtWidth(test.topic, width)
				if exitCode != 0 || stderr != "" {
					t.Fatalf("exitCode=%d stderr=%q stdout=%q", exitCode, stderr, stdout)
				}
				assertHelpContains(t, stdout, append([]string{test.usage}, test.required...)...)
				assertHelpOmits(t, stdout, test.forbidden...)
				assertHelpLayout(t, stdout, width, test.lineBudget)
			})
		}
	}
}

func TestHelpRoutesLeafFlagsAndNearestTopicErrors(t *testing.T) {
	stdout, stderr, exitCode := runCLIHelp([]string{"add", "mcp-server", "--help"})
	if exitCode != 0 || stderr != "" {
		t.Fatalf("exitCode=%d stderr=%q", exitCode, stderr)
	}
	assertHelpContains(t, stdout, "Usage: daem add mcp-server <name> <command>")

	stdout, stderr, exitCode = runCLIHelp([]string{"help", "add", "unknown"})
	if exitCode != 2 || stdout != "" {
		t.Fatalf("exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	assertHelpContains(t, stderr, `unknown help topic "add unknown"`, "next: run daem help add")

	stdout, stderr, exitCode = runCLIHelp([]string{"help", "apply", "extra"})
	if exitCode != 2 || stdout != "" {
		t.Fatalf("exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	assertHelpContains(t, stderr, `unknown help topic "apply extra"`, "next: run daem help apply")
}

func TestLiteralHelpOperandRequiresSeparator(t *testing.T) {
	stdout, stderr, exitCode := runCLIHelp([]string{"remove", "skill", "--dry-run", "--", "--help"})
	if exitCode == 0 {
		t.Fatalf("exitCode=0 stdout=%q stderr=%q, want operand processing rather than help", stdout, stderr)
	}
	if strings.Contains(stdout, "Usage: daem remove skill") {
		t.Fatalf("stdout=%q, literal --help must not route to help", stdout)
	}
}

func runHelpAtWidth(topic []string, width int) (string, string, int) {
	args := []string{"help"}
	if len(topic) != 0 {
		args = append(args, topic...)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := clipkg.RunWithOptions(args, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr, HelpWidth: width})
	return stdout.String(), stderr.String(), exitCode
}

func runCLIHelp(args []string) (string, string, int) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := clipkg.RunWithOptions(args, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr})
	return stdout.String(), stderr.String(), exitCode
}

func assertHelpContains(t *testing.T, output string, values ...string) {
	t.Helper()
	normalizedOutput := strings.Join(strings.Fields(output), " ")
	for _, value := range values {
		if !strings.Contains(normalizedOutput, strings.Join(strings.Fields(value), " ")) {
			t.Fatalf("output=%q, want %q", output, value)
		}
	}
}

func assertHelpOmits(t *testing.T, output string, values ...string) {
	t.Helper()
	normalizedOutput := strings.Join(strings.Fields(output), " ")
	for _, value := range values {
		if strings.Contains(normalizedOutput, strings.Join(strings.Fields(value), " ")) {
			t.Fatalf("output=%q, must omit %q", output, value)
		}
	}
}

func assertHelpLayout(t *testing.T, output string, width int, lineBudget int) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) > lineBudget {
		t.Fatalf("help has %d lines at width %d, budget %d\n%s", len(lines), width, lineBudget, output)
	}
	for index, line := range lines {
		if len(line) > width {
			t.Fatalf("line %d has %d bytes at width %d: %q", index+1, len(line), width, line)
		}
	}
}
