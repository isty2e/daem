package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAddMCPServerSupportsOpenCodeProjectAuthoring(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version = 1\ntargets = [\"opencode\"]\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", manifestPath,
		"--target", "opencode",
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
		"--dry-run",
		"--verbose",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"add: mcp_server/context7",
		`targets = ["opencode"]`,
		`scope = "project"`,
		`command = "npx"`,
		"lockfile: would write",
		"MCP config changes only when apply reconciles the locked projection",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(tempDir, "daem.lock.toml")); !os.IsNotExist(err) {
		t.Fatalf("dry-run lockfile stat err = %v, want missing lockfile", err)
	}
}

func TestRunAddMCPServerWritesOpenCodeManifestAndLockOnlyByDefault(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("version = 1\ntargets = [\"opencode\"]\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", manifestPath,
		"--target", "opencode",
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	manifestContent, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	for _, want := range []string{
		`targets = ["opencode"]`,
		`scope = "project"`,
		`command = "npx"`,
		`args = ["-y", "@upstash/context7-mcp@1.2.3"]`,
	} {
		if !strings.Contains(string(manifestContent), want) {
			t.Fatalf("manifest = %q, want %q", manifestContent, want)
		}
	}
	lockfileContent, err := os.ReadFile(filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("ReadFile lockfile returned error: %v", err)
	}
	for _, want := range []string{
		`entity_id = "mcp_server:context7"`,
		`subject_id = "projection/opencode.project.mcp-server/context7"`,
		`contribution_cardinality = "exclusive"`,
		`codec_contract = "opencode-project-mcp-local-command-v1"`,
		`aggregate_root = "opencode.json"`,
	} {
		if !strings.Contains(string(lockfileContent), want) {
			t.Fatalf("lockfile = %q, want %q", lockfileContent, want)
		}
	}
	if _, err := os.Stat(filepath.Join(tempDir, "opencode.json")); !os.IsNotExist(err) {
		t.Fatalf("opencode.json stat err = %v, want no host config write from authoring", err)
	}
}

func TestRunAddMCPServerRejectsOpenCodeEnvAuthoring(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"opencode\"]\n"
	if err := os.WriteFile(manifestPath, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", manifestPath,
		"--target", "opencode",
		"--env", "TOKEN=HOST_TOKEN",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -env") {
		t.Fatalf("stderr = %q, want manifest-only env rejection", stderr.String())
	}
	written, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(written) != original {
		t.Fatalf("manifest = %q, want unchanged %q", written, original)
	}
}

func TestRunAddMCPServerSupportsOpenCodeGlobalScopeAuthoring(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := "version = 1\ntargets = [\"opencode\"]\n"
	if err := os.WriteFile(manifestPath, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", manifestPath,
		"--target", "opencode",
		"--scope", "global",
		"--dry-run",
		"--verbose",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"add: mcp_server/context7",
		`targets = ["opencode"]`,
		`scope = "global"`,
		`command = "npx"`,
		"lockfile: would write",
		"MCP config changes only when apply reconciles the locked projection",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	written, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(written) != original {
		t.Fatalf("manifest = %q, want unchanged %q", written, original)
	}
}

func TestRunRemoveMCPServerSupportsOpenCodeProjectSelector(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`
	if err := os.WriteFile(manifestPath, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"remove", "mcp-server", "context7",
		"--manifest", manifestPath,
		"--target", "opencode",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"remove: mcp_server/context7",
		"change: remove mcp_server resource",
		"lockfile: would write",
		"MCP config changes only when apply removes the managed projection",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	written, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(written) != original {
		t.Fatalf("manifest = %q, want unchanged dry-run manifest", written)
	}
}

func TestRunRemoveMCPServerSupportsOpenCodeGlobalScopeSelector(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "daem.toml")
	original := `version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "global"
transport = "stdio"
command = "npx"
`
	if err := os.WriteFile(manifestPath, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithoutTerminal([]string{
		"remove", "mcp-server", "context7",
		"--manifest", manifestPath,
		"--target", "opencode",
		"--scope", "global",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, want := range []string{
		"remove: mcp_server/context7",
		"change: remove mcp_server resource",
		"lockfile: would write",
		"MCP config changes only when apply removes the managed projection",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	written, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(written) != original {
		t.Fatalf("manifest = %q, want unchanged %q", written, original)
	}
}
