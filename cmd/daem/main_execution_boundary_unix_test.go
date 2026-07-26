//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestMainConfirmationPTYConfirmedDelegatedCommandSIGINTCleansProcessTree(t *testing.T) {
	const secret = "edge-secret-value"
	manifestPath := writePTYDelegatedApplyFixture(t)
	root := filepath.Dir(manifestPath)
	evidenceDirectory := filepath.Join(root, "evidence")
	if err := os.MkdirAll(evidenceDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	readyPath := filepath.Join(evidenceDirectory, "ready")
	childReadyPath := filepath.Join(evidenceDirectory, "child-ready")
	termPath := filepath.Join(evidenceDirectory, "term")

	binDirectory := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	fakeClaude := filepath.Join(binDirectory, "claude")
	executable := writeSignalLifecycleCommandWrapper(t, fakeClaude)

	process := startConfirmationPTYProcessWithEnv(
		t, manifestPath, nil, true, true,
		"PATH="+binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CLAUDE_CONFIG_DIR="+filepath.Join(root, "claude-config"),
		"DAEM_SIGNAL_TEST_EXECUTABLE="+executable,
		signalLifecycleTreeRole+"=parent",
		signalLifecycleTreeReadyPath+"="+readyPath,
		signalLifecycleTreeChildPath+"="+childReadyPath,
		signalLifecycleTreeTERMPath+"="+termPath,
		signalLifecycleTreeSecret+"="+secret,
	)
	process.WaitForOutput(t, "Proceed with apply? [y/N]:")
	if _, err := process.master.Write([]byte("yes\n")); err != nil {
		t.Fatalf("write PTY answer: %v", err)
	}
	pids := readSignalLifecyclePIDs(t, readyPath)
	t.Cleanup(func() {
		_ = syscall.Kill(-pids[0], syscall.SIGKILL)
	})
	if err := process.command.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send SIGINT: %v", err)
	}
	waitForSignalLifecycleFile(t, termPath)
	if exitCode := process.Wait(t); exitCode != 130 {
		t.Fatalf("exit code = %d, want 130; terminal = %q", exitCode, process.terminal.String())
	}
	assertSignalLifecycleProcessesGone(t, pids)
	if strings.Contains(process.terminal.String(), secret) {
		t.Fatalf("terminal leaked secret: %q", process.terminal.String())
	}
	assertTreeDoesNotContain(t, root, secret)
}

func TestMainYesMCPProbeSIGTERMCleansProcessTree(t *testing.T) {
	root := t.TempDir()
	evidenceDirectory := filepath.Join(root, "evidence")
	if err := os.MkdirAll(evidenceDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	readyPath := filepath.Join(evidenceDirectory, "ready")
	childReadyPath := filepath.Join(evidenceDirectory, "child-ready")
	termPath := filepath.Join(evidenceDirectory, "term")
	binDirectory := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	const serverCommand = "mcp-signal-server"
	serverPath := filepath.Join(binDirectory, serverCommand)
	executable := writeSignalLifecycleCommandWrapper(t, serverPath)
	manifestPath := writePTYMCPProbeFixture(t, root, serverCommand)

	process := startConfirmationPTYCommandProcessWithEnv(
		t, []string{
			"probe", "mcp-server", "context7",
			"--manifest", manifestPath,
			"--target", "claude-code",
			"--scope", "project",
			"--yes",
		}, nil, true, true,
		"PATH="+binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"DAEM_SIGNAL_TEST_EXECUTABLE="+executable,
		signalLifecycleTreeRole+"=parent",
		signalLifecycleTreeReadyPath+"="+readyPath,
		signalLifecycleTreeChildPath+"="+childReadyPath,
		signalLifecycleTreeTERMPath+"="+termPath,
	)
	pids := readSignalLifecyclePIDs(t, readyPath)
	t.Cleanup(func() {
		_ = syscall.Kill(-pids[0], syscall.SIGKILL)
	})
	if err := process.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	waitForSignalLifecycleFile(t, termPath)
	if exitCode := process.Wait(t); exitCode != 143 {
		t.Fatalf("exit code = %d, want 143; terminal = %q", exitCode, process.terminal.String())
	}
	assertSignalLifecycleProcessesGone(t, pids)
	if strings.Contains(process.terminal.String(), "Proceed with probe?") {
		t.Fatalf("--yes probe prompted interactively: %q", process.terminal.String())
	}
}

func writePTYDelegatedApplyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	manifest := `version = 1
targets = ["claude-code"]

[[extension]]
id = "context7-managed"
carrier = "claude-code-plugin"
source = { marketplace = "context7@market" }
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	lockPTYManifest(t, manifestPath)
	return manifestPath
}

func writePTYMCPProbeFixture(t *testing.T, root string, serverCommand string) string {
	t.Helper()
	manifestPath := filepath.Join(root, "daem.toml")
	manifest := `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "` + serverCommand + `"
args = []
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	lockPTYManifest(t, manifestPath)
	return manifestPath
}

func writeSignalLifecycleCommandWrapper(t *testing.T, path string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec \"$DAEM_SIGNAL_TEST_EXECUTABLE\" -test.run=^TestSignalLifecycleProcessTreeHelper$\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return executable
}

func assertTreeDoesNotContain(t *testing.T, root string, forbidden string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), forbidden) {
			return errors.New("forbidden content in " + path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
