package cli_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	clipkg "github.com/isty2e/daem/internal/cli"
)

type helpCase struct {
	topic          []string
	usage          string
	lineBudget     int
	labels         []string
	forbiddenFlags []string
}

func TestHelpContractCoversEveryRetainedCommandAtSupportedWidths(t *testing.T) {
	cases := []helpCase{
		{topic: nil, usage: "Usage: daem <command> [options]", lineBudget: 30, labels: []string{"init", "import", "add", "remove", "lock", "outdated", "list", "status", "version", "doctor", "probe", "refresh", "unmanage", "apply", "recover", "migrate"}, forbiddenFlags: []string{"--manifest", "--json"}},
		{topic: []string{"add"}, usage: "Usage: daem add <resource>", lineBudget: 32, labels: []string{"extension", "instruction", "hook", "mcp-server", "skill", "skill-group"}, forbiddenFlags: []string{"--member", "--timeout"}},
		{topic: []string{"remove"}, usage: "Usage: daem remove <resource>", lineBudget: 32, labels: []string{"extension", "instruction", "hook", "mcp-server", "skill"}, forbiddenFlags: []string{"--scope"}},
		{topic: []string{"unmanage"}, usage: "Usage: daem unmanage <resource>", lineBudget: 32, labels: []string{"extension"}, forbiddenFlags: []string{"--yes", "--prune"}},
		{topic: []string{"list"}, usage: "Usage: daem list <resource>", lineBudget: 36, labels: []string{"resources", "outputs", "paths"}, forbiddenFlags: []string{"--inventory"}},
		{topic: []string{"probe"}, usage: "Usage: daem probe <resource>", lineBudget: 32, labels: []string{"mcp-server"}},
		{topic: []string{"refresh"}, usage: "Usage: daem refresh <resource>", lineBudget: 32, labels: []string{"extension"}},
		{topic: []string{"migrate"}, usage: "Usage: daem migrate <subject>", lineBudget: 32, labels: []string{"state"}},
		{topic: []string{"migrate", "state"}, usage: "Usage: daem migrate state", lineBudget: 48, labels: []string{"--recover", "--dry-run", "--yes", "--json", "--manifest"}},
		{topic: []string{"init"}, usage: "Usage: daem init", lineBudget: 46, labels: []string{"--force", "--manifest"}, forbiddenFlags: []string{"--yes", "--lockfile"}},
		{topic: []string{"import"}, usage: "Usage: daem import --target <target>", lineBudget: 46, labels: []string{"--source-dir", "--merge", "--target"}, forbiddenFlags: []string{"--output", "--yes", "--lockfile"}},
		{topic: []string{"lock"}, usage: "Usage: daem lock", lineBudget: 46, labels: []string{"--dry-run", "--verbose"}, forbiddenFlags: []string{"--lockfile", "--yes"}},
		{topic: []string{"outdated"}, usage: "Usage: daem outdated", lineBudget: 46, labels: []string{"--check"}},
		{topic: []string{"status"}, usage: "Usage: daem status", lineBudget: 46, labels: []string{"--check", "--target", "--json"}, forbiddenFlags: []string{"--inventory", "--scope"}},
		{topic: []string{"version"}, usage: "Usage: daem version", lineBudget: 46, labels: []string{"--json"}},
		{topic: []string{"apply"}, usage: "Usage: daem apply", lineBudget: 64, labels: []string{"--manage-existing", "--yes"}, forbiddenFlags: []string{"--attempt-delegates", "--adopt-existing", "--ensure-runtime", "--execute-mcp", "--lockfile"}},
		{topic: []string{"recover"}, usage: "Usage: daem recover", lineBudget: 48, labels: []string{"--dry-run", "--yes", "--json"}},
		{topic: []string{"doctor"}, usage: "Usage: daem doctor", lineBudget: 46, labels: []string{"--all-targets"}},
		{topic: []string{"add", "extension"}, usage: "Usage: daem add extension <id> <source>", lineBudget: 46, labels: []string{"--target"}, forbiddenFlags: []string{"--marketplace", "--host-source", "--yes"}},
		{topic: []string{"add", "instruction"}, usage: "Usage: daem add instruction <name> <source>", lineBudget: 46, labels: []string{"--target", "--scope"}},
		{topic: []string{"add", "hook"}, usage: "Usage: daem add hook <name> <event> <command>", lineBudget: 60, labels: []string{"--matcher", "--timeout"}, forbiddenFlags: []string{"--command", "--event", "--status-message", "--target-override"}},
		{topic: []string{"add", "mcp-server"}, usage: "Usage: daem add mcp-server <name> <command>", lineBudget: 60, labels: []string{"--arg"}, forbiddenFlags: []string{"--command", "--env", "--yes"}},
		{topic: []string{"add", "skill"}, usage: "Usage: daem add skill <source>", lineBudget: 60, labels: []string{"--path", "--ref", "--name"}, forbiddenFlags: []string{"--id", "--mode", "--yes"}},
		{topic: []string{"add", "skill-group"}, usage: "Usage: daem add skill-group <source-root> --member <name>", lineBudget: 46, labels: []string{"--member"}, forbiddenFlags: []string{"--mode", "--name"}},
		{topic: []string{"remove", "extension"}, usage: "Usage: daem remove extension <id>", lineBudget: 46},
		{topic: []string{"remove", "instruction"}, usage: "Usage: daem remove instruction <name>", lineBudget: 46},
		{topic: []string{"remove", "hook"}, usage: "Usage: daem remove hook <name>", lineBudget: 46},
		{topic: []string{"remove", "mcp-server"}, usage: "Usage: daem remove mcp-server <name>", lineBudget: 46},
		{topic: []string{"remove", "skill"}, usage: "Usage: daem remove skill <resource-key>", lineBudget: 46},
		{topic: []string{"unmanage", "extension"}, usage: "Usage: daem unmanage extension <id>", lineBudget: 46, labels: []string{"--target", "--scope"}, forbiddenFlags: []string{"--yes", "--prune", "--uninstall"}},
		{topic: []string{"list", "resources"}, usage: "Usage: daem list resources", lineBudget: 46, labels: []string{"--target", "--json"}},
		{topic: []string{"list", "outputs"}, usage: "Usage: daem list outputs", lineBudget: 46},
		{topic: []string{"list", "paths"}, usage: "Usage: daem list paths", lineBudget: 46},
		{topic: []string{"probe", "mcp-server"}, usage: "Usage: daem probe mcp-server <name>", lineBudget: 60, labels: []string{"--timeout"}},
		{topic: []string{"refresh", "extension"}, usage: "Usage: daem refresh extension <id>", lineBudget: 60, labels: []string{"--target", "--scope", "--timeout", "--yes"}},
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
				assertHelpContains(t, stdout, test.usage)
				assertHelpLabels(t, stdout, len(test.topic) == 0, test.labels, test.forbiddenFlags)
				assertHelpLayout(t, stdout, width, test.lineBudget)

				if width != 80 {
					return
				}
				for _, flag := range []string{"--help", "-h"} {
					args := append(append([]string(nil), test.topic...), flag)
					var aliasOut bytes.Buffer
					var aliasErr bytes.Buffer
					exitCode := clipkg.RunWithOptions(args, clipkg.RunOptions{Stdout: &aliasOut, Stderr: &aliasErr})
					if exitCode != 0 || aliasErr.Len() != 0 || aliasOut.String() != stdout {
						t.Fatalf("args=%q exitCode=%d stderr=%q stdout=%q, want current topic help %q", args, exitCode, aliasErr.String(), aliasOut.String(), stdout)
					}
				}
				for _, flag := range test.forbiddenFlags {
					args := append(append([]string(nil), test.topic...), flag, "help-contract-input")
					var rejectedOut bytes.Buffer
					var rejectedErr bytes.Buffer
					exitCode := clipkg.RunWithOptions(args, clipkg.RunOptions{Stdout: &rejectedOut, Stderr: &rejectedErr})
					if exitCode != 2 || rejectedOut.Len() != 0 || !strings.Contains(rejectedErr.String(), strings.TrimLeft(flag, "-")) {
						t.Fatalf("args=%q exitCode=%d stderr=%q stdout=%q, want rejected option", args, exitCode, rejectedErr.String(), rejectedOut.String())
					}
				}
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

func assertHelpLabels(t *testing.T, output string, root bool, required []string, forbidden []string) {
	t.Helper()
	labels := make(map[string]bool)
	inLabelSection := root
	for _, line := range strings.Split(output, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") {
			inLabelSection = root || line == "Operands:" || line == "Options:" || line == "Resources:" || line == "Subjects:"
		}
		if !inLabelSection || !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 0 {
			labels[fields[0]] = true
		}
	}

	for _, label := range required {
		if !labels[label] {
			t.Fatalf("output=%q, missing command/option label %q", output, label)
		}
	}
	for _, label := range forbidden {
		if labels[label] {
			t.Fatalf("output=%q, advertised unsupported option %q", output, label)
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
