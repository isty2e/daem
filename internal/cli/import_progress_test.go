package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunImportProgressIsTTYOnlyAndPreservesFinalOutput(t *testing.T) {
	root := enterImportProgressDirectory(t)
	manifestPath := filepath.Join(root, "daem.toml")
	args := []string{
		"--target", "codex",
		"--scope", "project",
		"--manifest", manifestPath,
		"--dry-run",
	}

	var terminalStdout bytes.Buffer
	var terminalStderr bytes.Buffer
	if exitCode := runImport(
		args,
		&terminalStdout,
		&terminalStderr,
		commandOptions{context: t.Context(), stderrIsTerminal: true},
	); exitCode != 0 {
		t.Fatalf("terminal exit = %d; stdout=%q stderr=%q", exitCode, terminalStdout.String(), terminalStderr.String())
	}
	if !strings.Contains(terminalStderr.String(), "Discovering import candidates") ||
		!strings.HasSuffix(terminalStderr.String(), "\r\x1b[2K") {
		t.Fatalf("terminal stderr = %q, want cleared discovery progress", terminalStderr.String())
	}

	var redirectedStdout bytes.Buffer
	var redirectedStderr bytes.Buffer
	if exitCode := runImport(
		args,
		&redirectedStdout,
		&redirectedStderr,
		commandOptions{context: t.Context()},
	); exitCode != 0 {
		t.Fatalf("redirected exit = %d; stdout=%q stderr=%q", exitCode, redirectedStdout.String(), redirectedStderr.String())
	}
	if redirectedStderr.Len() != 0 {
		t.Fatalf("redirected stderr = %q, want no progress", redirectedStderr.String())
	}
	if !bytes.Equal(terminalStdout.Bytes(), redirectedStdout.Bytes()) {
		t.Fatalf("terminal progress changed final stdout:\nterminal=%q\nredirected=%q", terminalStdout.String(), redirectedStdout.String())
	}

	var jsonStdout bytes.Buffer
	var jsonStderr bytes.Buffer
	jsonArgs := append(append([]string(nil), args...), "--json")
	if exitCode := runImport(
		jsonArgs,
		&jsonStdout,
		&jsonStderr,
		commandOptions{context: t.Context(), stderrIsTerminal: true},
	); exitCode != 0 {
		t.Fatalf("json exit = %d; stdout=%q stderr=%q", exitCode, jsonStdout.String(), jsonStderr.String())
	}
	if jsonStderr.Len() != 0 {
		t.Fatalf("json stderr = %q, want no progress", jsonStderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(jsonStdout.Bytes(), &envelope); err != nil {
		t.Fatalf("json output = %q: %v", jsonStdout.String(), err)
	}
}

func TestRunImportWriteProgressDistinguishesRevalidationAndPublication(t *testing.T) {
	root := enterImportProgressDirectory(t)
	manifestPath := filepath.Join(root, "daem.toml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runImport(
		[]string{
			"--target", "codex",
			"--scope", "project",
			"--manifest", manifestPath,
		},
		&stdout,
		&stderr,
		commandOptions{context: t.Context(), stderrIsTerminal: true},
	)
	if exitCode != 0 {
		t.Fatalf("exit = %d; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"Discovering import candidates",
		"Revalidating import sources",
		"Publishing import changes",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("published manifest: %v", err)
	}
}

func enterImportProgressDirectory(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "xdg-state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	if err := os.WriteFile("AGENTS.md", []byte("# Agents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}
