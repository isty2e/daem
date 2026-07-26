package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunValidateIsNotAPublicCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"validate"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}

	if !strings.Contains(stderr.String(), `unknown command "validate"`) {
		t.Fatalf("stderr = %q, want unknown command diagnostic", stderr.String())
	}
}

func TestRunHelpIncludesPublicCommands(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"help"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}

	if !strings.Contains(stdout.String(), "  lock ") {
		t.Fatalf("stdout = %q, want lock usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "  apply ") {
		t.Fatalf("stdout = %q, want apply usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "  status ") {
		t.Fatalf("stdout = %q, want status usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "  recover ") {
		t.Fatalf("stdout = %q, want recover usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "  doctor ") {
		t.Fatalf("stdout = %q, want doctor usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "  import ") {
		t.Fatalf("stdout = %q, want import usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "  version ") {
		t.Fatalf("stdout = %q, want version usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "  unmanage ") {
		t.Fatalf("stdout = %q, want unmanage usage", stdout.String())
	}
	for _, reject := range []string{
		"daem restore",
		"daem snapshot",
		"daem validate",
	} {
		if strings.Contains(stdout.String(), reject) {
			t.Fatalf("stdout = %q, did not want %q", stdout.String(), reject)
		}
	}
}

func TestRunProbeHelpListsAdmittedRuntimeProbeTargets(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"help", "probe", "mcp-server"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	for _, want := range []string{
		"--target <target>",
		"daem probe mcp-server context7 --target opencode --scope project --yes",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunAddHelpMentionsMCPGlobalScopeConsent(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"help", "add", "mcp-server"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	for _, want := range []string{
		"Target omission succeeds only when the manifest identifies one admitted row.",
		"Environment mappings and non-stdio transports are manifest-only.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"unknown"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), `unknown command "unknown"`) {
		t.Fatalf("stderr = %q, want unknown command diagnostic", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunRejectsUpdateLifecycleAliases(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "lifecycle positional arguments", command: "update", args: []string{"update", "extension", "context7-global"}},
		{name: "help does not reveal hidden route", command: "update", args: []string{"update", "--help"}},
		{name: "json does not reveal hidden route", command: "update", args: []string{"update", "--json"}},
		{name: "manifest flag does not reveal hidden route", command: "update", args: []string{"update", "--manifest", "/missing/daem.toml"}},
		{name: "command matching remains case sensitive", command: "Update", args: []string{"Update", "extension", "context7-global"}},
		{name: "upgrade is not an alias", command: "upgrade", args: []string{"upgrade", "extension", "context7-global"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := runWithoutTerminal(test.args, &stdout, &stderr)
			if exitCode != 2 {
				t.Fatalf("exitCode = %d, want 2", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), `unknown command "`+test.command+`"`) {
				t.Fatalf("stderr = %q, want unknown %s command", stderr.String(), test.command)
			}
		})
	}
}

func TestRunRejectsCarrierRemovalLifecycleAliases(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "uninstall command", args: []string{"uninstall", "extension", "context7-global"}, want: `unknown command "uninstall"`},
		{name: "prune command", args: []string{"prune", "extension", "context7-global"}, want: `unknown command "prune"`},
		{name: "cleanup command", args: []string{"cleanup", "extension", "context7-global"}, want: `unknown command "cleanup"`},
		{name: "plugin remove resource", args: []string{"remove", "plugin", "context7-global"}, want: `unknown remove resource "plugin"`},
		{name: "carrier remove resource", args: []string{"remove", "carrier", "context7-global"}, want: `unknown remove resource "carrier"`},
		{name: "marketplace remove resource", args: []string{"remove", "marketplace", "context7"}, want: `unknown remove resource "marketplace"`},
		{name: "uninstall flag", args: []string{"remove", "extension", "context7-global", "--uninstall"}, want: "flag provided but not defined: -uninstall"},
		{name: "prune flag", args: []string{"remove", "extension", "context7-global", "--prune"}, want: "flag provided but not defined: -prune"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := runWithoutTerminal(test.args, &stdout, &stderr)
			if exitCode != 2 {
				t.Fatalf("exitCode = %d, want 2", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
		})
	}
}

func TestRunRejectsPluginInventoryAndReadinessAliases(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "inventory command", args: []string{"inventory", "extension", "context7-global"}, want: `unknown command "inventory"`},
		{name: "adopt command", args: []string{"adopt", "extension", "context7-global"}, want: `unknown command "adopt"`},
		{name: "readiness command", args: []string{"readiness", "extension", "context7-global"}, want: `unknown command "readiness"`},
		{name: "verify command", args: []string{"verify", "extension", "context7-global"}, want: `unknown command "verify"`},
		{name: "trust command", args: []string{"trust", "extension", "context7-global"}, want: `unknown command "trust"`},
		{name: "extension probe subject", args: []string{"probe", "extension", "context7-global", "--yes"}, want: `unknown probe subject "extension"`},
		{name: "plugin probe subject", args: []string{"probe", "plugin", "context7", "--yes"}, want: `unknown probe subject "plugin"`},
		{name: "extension import subject", args: []string{"import", "extension", "--target", "claude-code", "--dry-run"}, want: `unexpected argument "extension"`},
		{name: "plugin doctor subject", args: []string{"doctor", "plugin", "--target", "claude-code"}, want: `unexpected argument "plugin"`},
		{name: "inventory list flag", args: []string{"list", "--inventory"}, want: `unknown list resource "--inventory"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := runWithoutTerminal(test.args, &stdout, &stderr)
			if exitCode != 2 {
				t.Fatalf("exitCode = %d, want 2", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
		})
	}
}
