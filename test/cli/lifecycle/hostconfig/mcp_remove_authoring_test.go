package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunRemoveMCPServerDryRunPlansWithoutHostWrites(t *testing.T) {
	project := newMCPCLIProject(t)
	original := mcpAuthoringManifest()
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, aggregate.ClaudeProjectMCPConfigPath)
	hostConfig := `{"mcpServers":{"context7":{"type":"stdio","command":"npx"},"existing":{"command":"node"}}}`
	testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"remove: mcp_server/context7",
		"change: remove mcp_server resource",
		"manifest diff:",
		"-[[mcp_server]]",
		`-name = "context7"`,
		"lockfile: would write " + project.lockfilePath,
		"note: remove updates the manifest and lockfile only; MCP config changes only when apply removes the managed projection",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	tomlContent := testkit.ReadFile(t, project.manifestPath)
	if string(tomlContent) != original {
		t.Fatalf("manifest = %q, want original", string(tomlContent))
	}
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveAntigravityMCPServerDryRunPlansWithoutHostReadsOrWrites(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := antigravityMCPAuthoringManifest()
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, "home", ".gemini", "config", "mcp_config.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"remove: mcp_server/context7",
		"change: remove mcp_server resource",
		"manifest diff:",
		"-[[mcp_server]]",
		`-targets = ["antigravity-cli"]`,
		`-scope = "global"`,
		"lockfile: would write " + project.lockfilePath,
		"note: remove updates the manifest and lockfile only; MCP config changes only when apply removes the managed projection",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveMCPServerYesWritesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	testkit.WriteFile(t, project.root, "daem.toml", mcpAuthoringManifest())
	hostConfigPath := filepath.Join(project.root, aggregate.ClaudeProjectMCPConfigPath)
	hostConfig := `{"mcpServers":{"context7":{"type":"stdio","command":"npx"},"existing":{"command":"node"}}}`
	testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"removed: mcp_server/context7",
		"change: remove mcp_server resource",
		"lockfile: wrote " + project.lockfilePath,
		"next: run daem apply --manifest " + project.manifestPath + " --dry-run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.MCPServers()) != 0 {
		t.Fatalf("MCPServers = %#v, want none", normalized.MCPServers())
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(locked.Locked.Subjects()) != 0 {
		t.Fatalf("locked subjects = %#v, want none", locked.Locked.Subjects())
	}
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveCodexMCPServerYesWritesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	original := codexMCPAuthoringManifest()
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, aggregate.CodexProjectMCPConfigPath)
	hostConfig := `mcp_servers = [`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "codex",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"removed: mcp_server/context7",
		"change: remove mcp_server resource",
		"lockfile: wrote " + project.lockfilePath,
		"next: run daem apply --manifest " + project.manifestPath + " --dry-run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.MCPServers()) != 0 {
		t.Fatalf("MCPServers = %#v, want none", normalized.MCPServers())
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(locked.Locked.Subjects()) != 0 {
		t.Fatalf("locked subjects = %#v, want none", locked.Locked.Subjects())
	}
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveCodexGlobalMCPServerYesWritesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := codexGlobalMCPAuthoringManifest()
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, "home", ".codex", "config.toml")
	hostConfig := `mcp_servers = [`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "codex",
		"--scope", "global",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"removed: mcp_server/context7",
		"change: remove mcp_server resource",
		"lockfile: wrote " + project.lockfilePath,
		"next: run daem apply --manifest " + project.manifestPath + " --dry-run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.MCPServers()) != 0 {
		t.Fatalf("MCPServers = %#v, want none", normalized.MCPServers())
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(locked.Locked.Subjects()) != 0 {
		t.Fatalf("locked subjects = %#v, want none", locked.Locked.Subjects())
	}
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveClaudeGlobalMCPServerYesWritesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := claudeGlobalMCPAuthoringManifest()
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, "home", ".claude.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "claude-code",
		"--scope", "global",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"removed: mcp_server/context7",
		"change: remove mcp_server resource",
		"lockfile: wrote " + project.lockfilePath,
		"next: run daem apply --manifest " + project.manifestPath + " --dry-run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.MCPServers()) != 0 {
		t.Fatalf("MCPServers = %#v, want none", normalized.MCPServers())
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(locked.Locked.Subjects()) != 0 {
		t.Fatalf("locked subjects = %#v, want none", locked.Locked.Subjects())
	}
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveAntigravityMCPServerYesWritesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	testkit.WriteFile(t, project.root, "daem.toml", antigravityMCPAuthoringManifest())
	hostConfigPath := filepath.Join(project.root, "home", ".gemini", "config", "mcp_config.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"removed: mcp_server/context7",
		"change: remove mcp_server resource",
		"lockfile: wrote " + project.lockfilePath,
		"next: run daem apply --manifest " + project.manifestPath + " --dry-run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.MCPServers()) != 0 {
		t.Fatalf("MCPServers = %#v, want none", normalized.MCPServers())
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	if len(locked.Locked.Subjects()) != 0 {
		t.Fatalf("locked subjects = %#v, want none", locked.Locked.Subjects())
	}
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveMCPServerYesDoesNotReadMalformedHostConfig(t *testing.T) {
	project := newMCPCLIProject(t)
	testkit.WriteFile(t, project.root, "daem.toml", mcpAuthoringManifest())
	hostConfigPath := filepath.Join(project.root, aggregate.ClaudeProjectMCPConfigPath)
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveAntigravityMCPServerCanRemoveNonAdmittedExistingShape(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := `version = 1
targets = ["antigravity-cli"]

[[mcp_server]]
name = "context7"
targets = ["antigravity-cli"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
env = { API_TOKEN = { from_env = "CONTEXT7_API_TOKEN" } }
`
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, "home", ".gemini", "config", "mcp_config.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "remove: mcp_server/context7") ||
		!strings.Contains(stdout.String(), "change: remove mcp_server resource") {
		t.Fatalf("stdout = %q, want removal plan", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(project.root, ".daem")))

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("write exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	environment, err := declarationmanifest.Load(project.manifestPath)
	if err != nil {
		t.Fatalf("load repaired manifest: %v", err)
	}
	if got := environment.MCPServers(); len(got) != 0 {
		t.Fatalf("repaired manifest MCP servers = %#v, want none", got)
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("load repaired lockfile: %v", err)
	}
	if got := locked.Locked.Subjects(); len(got) != 0 {
		t.Fatalf("repaired locked subjects = %#v, want none", got)
	}
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(project.root, ".daem")))
}

func TestRunRemoveAntigravityMCPServerInfersUniqueRow(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := antigravityMCPAuthoringManifest()
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, "home", ".gemini", "config", "mcp_config.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "remove: mcp_server/context7") {
		t.Fatalf("stdout = %q, want inferred removal plan", stdout.String())
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveMCPServerDryRunJSONDescribesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	original := mcpAuthoringManifest()
	testkit.WriteFile(t, project.root, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	var payload struct {
		Command   string
		Mode      string
		Operation string
		Lockfile  *struct {
			Path   string
			Status string
		}
		Changes []struct {
			ResourceID    string `json:"resource_id"`
			ChangeKind    string `json:"change_kind"`
			ManifestBlock string `json:"manifest_block"`
			Resource      struct {
				Kind string
				Name string
			}
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v; stdout = %q", err, stdout.String())
	}
	if payload.Command != "remove" || payload.Mode != "dry-run" || payload.Operation != "remove" {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.Lockfile == nil || payload.Lockfile.Path != project.lockfilePath || payload.Lockfile.Status != "would_write" {
		t.Fatalf("Lockfile = %#v, want would_write %q", payload.Lockfile, project.lockfilePath)
	}
	if len(payload.Changes) != 1 {
		t.Fatalf("len(Changes) = %d, want 1", len(payload.Changes))
	}
	change := payload.Changes[0]
	if change.ResourceID != "mcp_server/context7" || change.Resource.Kind != "mcp_server" || change.Resource.Name != "context7" {
		t.Fatalf("change resource = %#v", change)
	}
	if change.ChangeKind != "remove mcp_server resource" || change.ManifestBlock != "" {
		t.Fatalf("change = %#v, want remove without manifest block", change)
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
}

func TestRunRemoveAntigravityMCPServerDryRunJSONDescribesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := antigravityMCPAuthoringManifest()
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, "home", ".gemini", "config", "mcp_config.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	var payload struct {
		Command   string
		Mode      string
		Operation string
		Lockfile  *struct {
			Path   string
			Status string
		}
		Changes []struct {
			ResourceID    string `json:"resource_id"`
			ChangeKind    string `json:"change_kind"`
			ManifestBlock string `json:"manifest_block"`
			Resource      struct {
				Kind string
				Name string
			}
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v; stdout = %q", err, stdout.String())
	}
	if payload.Command != "remove" || payload.Mode != "dry-run" || payload.Operation != "remove" {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.Lockfile == nil || payload.Lockfile.Path != project.lockfilePath || payload.Lockfile.Status != "would_write" {
		t.Fatalf("Lockfile = %#v, want would_write %q", payload.Lockfile, project.lockfilePath)
	}
	if len(payload.Changes) != 1 {
		t.Fatalf("len(Changes) = %d, want 1", len(payload.Changes))
	}
	change := payload.Changes[0]
	if change.ResourceID != "mcp_server/context7" || change.Resource.Kind != "mcp_server" || change.Resource.Name != "context7" {
		t.Fatalf("change resource = %#v", change)
	}
	if change.ChangeKind != "remove mcp_server resource" || change.ManifestBlock != "" {
		t.Fatalf("change = %#v, want remove without manifest block", change)
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveMCPServerRejectsUnsupportedInputsBeforeWriting(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
		code int
	}{
		{
			name: "unsupported target",
			args: []string{"remove", "mcp-server", "context7", "--target", "unknown-agent", "--dry-run"},
			want: `unknown target "unknown-agent"`,
			code: 2,
		},
		{
			name: "multiple mcp targets",
			args: []string{"remove", "mcp-server", "context7", "--target", "claude-code", "--target", "antigravity-cli", "--scope", "global", "--dry-run"},
			want: "accepts at most one distinct --target",
			code: 1,
		},
		{
			name: "missing resource",
			args: []string{"remove", "mcp-server", "missing", "--dry-run"},
			want: `mcp_server resource "missing" not found`,
			code: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			original := mcpAuthoringManifest()
			testkit.WriteFile(t, project.root, "daem.toml", original)
			hostConfigPath := filepath.Join(project.root, aggregate.ClaudeProjectMCPConfigPath)
			hostConfig := `{"mcpServers":{"context7":{"type":"stdio","command":"npx"}}}`
			testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, hostConfig)
			args := append([]string{}, test.args...)
			args = append(args, "--manifest", project.manifestPath)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != test.code {
				t.Fatalf("exitCode = %d, want %d, stdout = %q, stderr = %q", exitCode, test.code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
			testkit.AssertFileContent(t, project.manifestPath, original)
			testkit.AssertPathMissing(t, project.lockfilePath)
			testkit.AssertFileContent(t, hostConfigPath, hostConfig)
			testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(project.root, ".daem")))
			if test.name == "missing resource" && !strings.Contains(stderr.String(), "next: inspect declared [[mcp_server]] entries") {
				t.Fatalf("stderr = %q, want not-found next-step hint", stderr.String())
			}
		})
	}
}

func TestRunRemoveMCPServerYesLeavesFilesUnchangedWhenProspectiveLockFails(t *testing.T) {
	project := newMCPCLIProject(t)
	original := mcpAuthoringManifest() + `
[[skill]]
name = "missing-skill"
source = { path = "skills/missing-skill", mode = "vendor" }
targets = ["claude-code"]
`
	testkit.WriteFile(t, project.root, "daem.toml", original)
	testkit.WriteFile(t, project.root, "daem.lock.toml", "lock stays\n")
	hostConfigPath := filepath.Join(project.root, aggregate.ClaudeProjectMCPConfigPath)
	hostConfig := `{"mcpServers":{"context7":{"type":"stdio","command":"npx"}}}`
	testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
	}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "remove failed: lock prospective manifest") {
		t.Fatalf("stderr = %q, want prospective lock failure", stderr.String())
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertFileContent(t, project.lockfilePath, "lock stays\n")
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunRemoveAntigravityMCPServerYesLeavesFilesUnchangedWhenProspectiveLockFails(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := antigravityMCPAuthoringManifest() + `
[[skill]]
name = "missing-skill"
source = { path = "skills/missing-skill", mode = "vendor" }
targets = ["antigravity-cli"]
`
	testkit.WriteFile(t, project.root, "daem.toml", original)
	testkit.WriteFile(t, project.root, "daem.lock.toml", "lock stays\n")
	hostConfigPath := filepath.Join(project.root, "home", ".gemini", "config", "mcp_config.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"remove", "mcp-server", "context7",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
	}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "remove failed: lock prospective manifest") {
		t.Fatalf("stderr = %q, want prospective lock failure", stderr.String())
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertFileContent(t, project.lockfilePath, "lock stays\n")
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(project.root, ".daem")))
}

func mcpAuthoringManifest() string {
	return `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
env = { API_TOKEN = { from_env = "CONTEXT7_API_TOKEN" } }
`
}

func antigravityMCPAuthoringManifest() string {
	return `version = 1
targets = ["antigravity-cli"]

[[mcp_server]]
name = "context7"
targets = ["antigravity-cli"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`
}

func codexMCPAuthoringManifest() string {
	return `version = 1
targets = ["codex"]

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`
}

func codexGlobalMCPAuthoringManifest() string {
	return `version = 1
targets = ["codex"]

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`
}

func claudeGlobalMCPAuthoringManifest() string {
	return `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`
}
