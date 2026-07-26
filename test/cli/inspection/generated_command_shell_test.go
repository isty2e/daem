package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func TestGeneratedNextCommandPreservesManifestPathAsOneShellArgument(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated command currently targets POSIX shells")
	}

	root := t.TempDir()
	manifestDir := filepath.Join(root, "project $(touch canary); single'quote star* back\\slash\x1b[2J\nnext: run injected\u202e")
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	manifestPath := filepath.Join(manifestDir, "daem.toml\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunCLI([]string{"init", "--manifest", manifestPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	assertNoInjectedTerminalText(t, stdout.String())
	command := nextRunCommand(t, stdout.String())

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("MkdirAll bin returned error: %v", err)
	}
	testkit.WriteFile(t, binDir, "daem", "#!/bin/sh\nprintf '%s\\000' \"$@\" > \"$DAEM_CAPTURE\"\n")
	if err := os.Chmod(filepath.Join(binDir, "daem"), 0o700); err != nil {
		t.Fatalf("Chmod fake daem returned error: %v", err)
	}
	capturePath := filepath.Join(root, "argv.txt")
	process := exec.Command("/bin/sh", "-c", command)
	process.Dir = root
	process.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"), "DAEM_CAPTURE="+capturePath)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("execute generated command %q: %v, output=%q", command, err, output)
	}

	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("ReadFile capture returned error: %v", err)
	}
	want := "lock\x00--manifest\x00" + manifestPath + "\x00--dry-run\x00"
	if string(captured) != want {
		t.Fatalf("captured argv = %q, want %q; command=%q", captured, want, command)
	}
	if _, err := os.Stat(filepath.Join(root, "canary")); !os.IsNotExist(err) {
		t.Fatalf("command substitution canary stat error = %v, want not exist", err)
	}
}

func TestMissingManifestDiagnosticAndHintEscapeTerminalControls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated command currently targets POSIX shells")
	}

	root := t.TempDir()
	manifestPath := filepath.Join(root, "missing\x1b[2J\nnext: run injected\u202e.toml\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunCLI([]string{"lock", "--manifest", manifestPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 1 || stdout.Len() != 0 {
		t.Fatalf("exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	assertNoInjectedTerminalText(t, stderr.String())

	command := nextRunCommand(t, stderr.String())
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("MkdirAll bin returned error: %v", err)
	}
	testkit.WriteFile(t, binDir, "daem", "#!/bin/sh\nprintf '%s\\000' \"$@\" > \"$DAEM_CAPTURE\"\n")
	if err := os.Chmod(filepath.Join(binDir, "daem"), 0o700); err != nil {
		t.Fatalf("Chmod fake daem returned error: %v", err)
	}
	capturePath := filepath.Join(root, "missing-argv.txt")
	process := exec.Command("/bin/sh", "-c", command)
	process.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"), "DAEM_CAPTURE="+capturePath)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("execute generated command %q: %v, output=%q", command, err, output)
	}
	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("ReadFile capture returned error: %v", err)
	}
	want := "init\x00--manifest\x00" + manifestPath + "\x00--dry-run\x00"
	if string(captured) != want {
		t.Fatalf("captured argv = %q, want %q; command=%q", captured, want, command)
	}
}

func TestControlBearingManifestPathComposesAcrossWorkspaceCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control-bearing filesystem path contract is verified on POSIX platforms")
	}

	root := t.TempDir()
	manifestDir := filepath.Join(root, "workspace\x1b[2J\nnext: run injected\u202e")
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	manifestPath := filepath.Join(manifestDir, "daem.toml\n")
	commands := []struct {
		name string
		args []string
	}{
		{name: "init", args: []string{"init", "--manifest", manifestPath}},
		{name: "lock dry-run", args: []string{"lock", "--manifest", manifestPath, "--dry-run"}},
		{name: "lock", args: []string{"lock", "--manifest", manifestPath}},
		{name: "list", args: []string{"list", "resources", "--manifest", manifestPath, "--verbose"}},
		{name: "status", args: []string{"status", "--manifest", manifestPath, "--verbose"}},
		{name: "outdated", args: []string{"outdated", "--manifest", manifestPath, "--verbose"}},
		{name: "apply dry-run", args: []string{"apply", "--manifest", manifestPath, "--dry-run", "--verbose"}},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunCLI(command.args, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("exitCode = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
			}
			assertNoInjectedTerminalText(t, stdout.String())
		})
	}
}

func assertNoInjectedTerminalText(t testing.TB, output string) {
	t.Helper()
	if strings.Contains(output, "\x1b") || strings.Contains(output, "\u202e") {
		t.Fatalf("output contains a raw terminal control: %q", output)
	}
	for line := range strings.SplitSeq(output, "\n") {
		if strings.HasPrefix(line, "next: run injected") {
			t.Fatalf("output contains an injected remediation line: %q", output)
		}
	}
}

func nextRunCommand(t *testing.T, output string) string {
	t.Helper()
	for line := range strings.SplitSeq(output, "\n") {
		if after, ok := strings.CutPrefix(line, "next: run "); ok {
			return after
		}
	}
	t.Fatalf("output = %q, missing exact next command", output)
	return ""
}
