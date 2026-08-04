package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunAddMCPServerDryRunPlansWithoutHostWrites(t *testing.T) {
	project := newMCPCLIProject(t)
	original := "version = 1\ntargets = [\"claude-code\"]\n"
	testkit.WriteFile(t, project.root, "daem.toml", original)
	probePath := installMCPProbeCommand(t, project.root, "npx")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"add: mcp_server/context7",
		"change: append mcp_server resource",
		"[[mcp_server]]",
		`name = "context7"`,
		`targets = ["claude-code"]`,
		`scope = "project"`,
		`transport = "stdio"`,
		`command = "npx"`,
		`args = ["-y", "@upstash/context7-mcp@1.2.3"]`,
		"manifest diff:",
		"lockfile: would write " + project.lockfilePath,
		"note: add updates the manifest and lockfile only; MCP config changes only when apply reconciles the locked projection",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertPathMissing(t, filepath.Join(project.root, aggregate.ClaudeProjectMCPConfigPath))
	testkit.AssertPathMissing(t, probePath)
}

func TestRunAddMCPServerYesWritesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")
	hostConfigPath := filepath.Join(project.root, aggregate.ClaudeProjectMCPConfigPath)
	testkit.WriteFile(t, project.root, aggregate.ClaudeProjectMCPConfigPath, `{"mcpServers":{"existing":{"command":"node"}}}`)
	probePath := installMCPProbeCommand(t, project.root, "npx")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: mcp_server/context7",
		"change: append mcp_server resource",
		"lockfile: wrote " + project.lockfilePath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.MCPServers()) != 1 {
		t.Fatalf("MCPServers = %#v, want context7", normalized.MCPServers())
	}
	testkit.AssertSingleMCPStdioBinding(t, normalized.MCPServers()[0], "context7", target.TargetClaudeCode, target.ScopeProject, "npx", []string{"-y", "@upstash/context7-mcp@1.2.3"})
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	assertLockedMCPSubject(t, locked, "context7")
	testkit.AssertFileContent(t, hostConfigPath, `{"mcpServers":{"existing":{"command":"node"}}}`)
	testkit.AssertPathMissing(t, probePath)
}

func TestRunAddCodexMCPServerYesWritesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	hostConfigPath := filepath.Join(project.root, aggregate.CodexProjectMCPConfigPath)
	hostConfig := `mcp_servers = [`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)
	probePath := installMCPProbeCommand(t, project.root, "npx")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--target", "codex",
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: mcp_server/context7",
		"change: append mcp_server resource",
		"lockfile: wrote " + project.lockfilePath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.MCPServers()) != 1 {
		t.Fatalf("MCPServers = %#v, want Codex project context7 without env", normalized.MCPServers())
	}
	stdio := testkit.AssertSingleMCPStdioBinding(t, normalized.MCPServers()[0], "context7", target.TargetCodex, target.ScopeProject, "npx", []string{"-y", "@upstash/context7-mcp@1.2.3"})
	if len(stdio.Env()) != 0 {
		t.Fatalf("MCP server env = %#v, want empty", stdio.Env())
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	assertLockedCodexMCPSubject(t, locked, "context7")
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
	testkit.AssertPathMissing(t, probePath)
}

func TestRunAddCodexGlobalMCPServerYesWritesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	hostConfigPath := filepath.Join(project.root, "home", ".codex", "config.toml")
	hostConfig := `mcp_servers = [`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)
	probePath := installMCPProbeCommand(t, project.root, "npx")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--target", "codex",
		"--scope", "global",
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: mcp_server/context7",
		"change: append mcp_server resource",
		"lockfile: wrote " + project.lockfilePath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.MCPServers()) != 1 {
		t.Fatalf("MCPServers = %#v, want Codex global context7 without env", normalized.MCPServers())
	}
	stdio := testkit.AssertSingleMCPStdioBinding(t, normalized.MCPServers()[0], "context7", target.TargetCodex, target.ScopeGlobal, "npx", []string{"-y", "@upstash/context7-mcp@1.2.3"})
	if len(stdio.Env()) != 0 {
		t.Fatalf("MCP server env = %#v, want empty", stdio.Env())
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	assertLockedCodexGlobalMCPSubject(t, locked, "context7")
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
	testkit.AssertPathMissing(t, probePath)
}

func TestRunAddClaudeGlobalMCPServerYesWritesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")
	hostConfigPath := filepath.Join(project.root, "home", ".claude.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)
	probePath := installMCPProbeCommand(t, project.root, "npx")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--target", "claude-code",
		"--scope", "global",
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: mcp_server/context7",
		"change: append mcp_server resource",
		"lockfile: wrote " + project.lockfilePath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.MCPServers()) != 1 {
		t.Fatalf("MCPServers = %#v, want Claude global context7 without env", normalized.MCPServers())
	}
	stdio := testkit.AssertSingleMCPStdioBinding(t, normalized.MCPServers()[0], "context7", target.TargetClaudeCode, target.ScopeGlobal, "npx", []string{"-y", "@upstash/context7-mcp@1.2.3"})
	if len(stdio.Env()) != 0 {
		t.Fatalf("MCP server env = %#v, want empty", stdio.Env())
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	assertLockedClaudeGlobalMCPSubject(t, locked, "context7")
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
	testkit.AssertPathMissing(t, probePath)
}

func TestRunAddAntigravityMCPServerDryRunPlansWithoutHostReadsOrWrites(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := "version = 1\ntargets = [\"antigravity-cli\"]\n"
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, "home", ".gemini", "config", "mcp_config.json")
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), `{"mcpServers":`)
	probePath := installMCPProbeCommand(t, project.root, "npx")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
		"--dry-run",
		"--diff",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"add: mcp_server/context7",
		"change: append mcp_server resource",
		"[[mcp_server]]",
		`name = "context7"`,
		`targets = ["antigravity-cli"]`,
		`scope = "global"`,
		`transport = "stdio"`,
		`command = "npx"`,
		`args = ["-y", "@upstash/context7-mcp@1.2.3"]`,
		"manifest diff:",
		"lockfile: would write " + project.lockfilePath,
		"note: add updates the manifest and lockfile only; MCP config changes only when apply reconciles the locked projection",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if strings.Contains(stdout.String(), `env =`) {
		t.Fatalf("stdout = %q, Antigravity add must not render env", stdout.String())
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertFileContent(t, hostConfigPath, `{"mcpServers":`)
	testkit.AssertPathMissing(t, probePath)
}

func TestRunAddAntigravityMCPServerYesWritesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"antigravity-cli\"]\n")
	hostConfigPath := filepath.Join(project.root, "home", ".gemini", "config", "mcp_config.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)
	probePath := installMCPProbeCommand(t, project.root, "npx")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"added: mcp_server/context7",
		"change: append mcp_server resource",
		"lockfile: wrote " + project.lockfilePath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	normalized, err := declarationmanifest.Decode(testkit.ReadFile(t, project.manifestPath))
	if err != nil {
		t.Fatalf("declarationmanifest.Decode returned error: %v", err)
	}
	if len(normalized.MCPServers()) != 1 {
		t.Fatalf("MCPServers = %#v, want Antigravity global context7", normalized.MCPServers())
	}
	stdio := testkit.AssertSingleMCPStdioBinding(t, normalized.MCPServers()[0], "context7", target.TargetAntigravityCLI, target.ScopeGlobal, "npx", []string{"-y", "@upstash/context7-mcp@1.2.3"})
	if len(stdio.Env()) != 0 {
		t.Fatalf("MCP server env = %#v, want empty", stdio.Env())
	}
	locked, err := lockfile.Load(project.lockfilePath)
	if err != nil {
		t.Fatalf("lockfile.Load returned error: %v", err)
	}
	assertLockedAntigravityMCPSubject(t, locked, "context7")
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
	testkit.AssertPathMissing(t, probePath)
}

func TestRunAddAntigravityMCPServerYesLeavesFilesUnchangedWhenProspectiveLockFails(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := `version = 1
targets = ["antigravity-cli"]

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
	probePath := installMCPProbeCommand(t, project.root, "npx")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
	}, &stdout, &stderr)
	if exitCode == 0 {
		t.Fatalf("exitCode = 0, stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "add failed: lock prospective manifest") {
		t.Fatalf("stderr = %q, want prospective lock failure", stderr.String())
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertFileContent(t, project.lockfilePath, "lock stays\n")
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
	testkit.AssertPathMissing(t, probePath)
	testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(project.root, ".daem")))
}

func TestRunAddMCPServerPreservesFlagLikeArgs(t *testing.T) {
	project := newMCPCLIProject(t)
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--arg", "--stdio",
		"--arg", "--port=0",
		"--arg", "@upstash/context7-mcp@1.2.3",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`args = ["--stdio", "--port=0", "@upstash/context7-mcp@1.2.3"]`,
		"lockfile: would write " + project.lockfilePath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertPathMissing(t, filepath.Join(project.root, aggregate.ClaudeProjectMCPConfigPath))
}

func TestRunAddAntigravityMCPServerPreservesFlagLikeAndQuotedArgs(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := "version = 1\ntargets = [\"claude-code\"]\n"
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, "home", ".gemini", "config", "mcp_config.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
		"--arg", "--stdio",
		"--arg", "--port=0",
		"--arg", "--label=agent env",
		"--arg", "@upstash/context7-mcp@1.2.3",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`targets = ["antigravity-cli"]`,
		`scope = "global"`,
		`args = ["--stdio", "--port=0", "--label=agent env", "@upstash/context7-mcp@1.2.3"]`,
		"lockfile: would write " + project.lockfilePath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunAddMCPServerDryRunJSONDescribesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	original := "version = 1\ntargets = [\"claude-code\"]\n"
	testkit.WriteFile(t, project.root, "daem.toml", original)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	if payload.Command != "add" || payload.Mode != "dry-run" || payload.Operation != "add" {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.ManifestPath != project.manifestPath {
		t.Fatalf("ManifestPath = %q, want %q", payload.ManifestPath, project.manifestPath)
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
	if change.ChangeKind != "append mcp_server resource" || !strings.Contains(change.ManifestBlock, `command = "npx"`) {
		t.Fatalf("change = %#v", change)
	}
	if len(payload.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none for pinned npx package", payload.Warnings)
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertPathMissing(t, filepath.Join(project.root, aggregate.ClaudeProjectMCPConfigPath))
}

func TestRunAddAntigravityMCPServerDryRunJSONDescribesManifestAndLockOnly(t *testing.T) {
	project := newMCPCLIProject(t)
	t.Setenv("HOME", filepath.Join(project.root, "home"))
	original := "version = 1\ntargets = [\"antigravity-cli\"]\n"
	testkit.WriteFile(t, project.root, "daem.toml", original)
	hostConfigPath := filepath.Join(project.root, "home", ".gemini", "config", "mcp_config.json")
	hostConfig := `{"mcpServers":`
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), hostConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--target", "antigravity-cli",
		"--scope", "global",
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp@1.2.3",
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	if payload.Command != "add" || payload.Mode != "dry-run" || payload.Operation != "add" {
		t.Fatalf("payload header = %#v", payload)
	}
	if payload.ManifestPath != project.manifestPath {
		t.Fatalf("ManifestPath = %q, want %q", payload.ManifestPath, project.manifestPath)
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
	if change.ChangeKind != "append mcp_server resource" ||
		!strings.Contains(change.ManifestBlock, `targets = ["antigravity-cli"]`) ||
		!strings.Contains(change.ManifestBlock, `scope = "global"`) ||
		strings.Contains(change.ManifestBlock, `env =`) {
		t.Fatalf("change = %#v", change)
	}
	if len(payload.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none for pinned npx package", payload.Warnings)
	}
	testkit.AssertFileContent(t, project.manifestPath, original)
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertFileContent(t, hostConfigPath, hostConfig)
}

func TestRunAddMCPServerWarnsForFloatingPackageIdentity(t *testing.T) {
	project := newMCPCLIProject(t)
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`warning: mcp_server "context7" uses floating delegated npm package "@upstash/context7-mcp"`,
		"pin every package selector",
		`args = ["-y", "@upstash/context7-mcp"]`,
		"lockfile: would write " + project.lockfilePath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, project.lockfilePath)
	testkit.AssertPathMissing(t, filepath.Join(project.root, aggregate.ClaudeProjectMCPConfigPath))
}

func TestRunAddMCPServerJSONWarnsForFloatingPackageIdentity(t *testing.T) {
	project := newMCPCLIProject(t)
	testkit.WriteFile(t, project.root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--manifest", project.manifestPath,
		"--arg", "-y",
		"--arg", "@upstash/context7-mcp",
		"--dry-run",
		"--json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}

	var payload struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("Unmarshal returned error: %v; stdout = %q", err, stdout.String())
	}
	if len(payload.Warnings) != 1 {
		t.Fatalf("Warnings = %#v, want one warning", payload.Warnings)
	}
	if !strings.Contains(payload.Warnings[0], `floating delegated npm package "@upstash/context7-mcp"`) {
		t.Fatalf("Warnings = %#v, want floating npm package warning", payload.Warnings)
	}
}

func TestRunAddMCPServerRejectsUnsupportedInputsBeforeWriting(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
		code int
	}{
		{
			name: "missing command",
			args: []string{"add", "mcp-server", "context7", "--dry-run"},
			want: "missing mcp-server command",
			code: 2,
		},
		{
			name: "manifest-only env",
			args: []string{"add", "mcp-server", "context7", "npx", "--env", "API_TOKEN=CONTEXT7_API_TOKEN", "--dry-run"},
			want: "flag provided but not defined: -env",
			code: 2,
		},
		{
			name: "unsupported target",
			args: []string{"add", "mcp-server", "context7", "npx", "--target", "unknown-agent", "--dry-run"},
			want: `unknown target "unknown-agent"`,
			code: 2,
		},
		{
			name: "antigravity missing explicit scope",
			args: []string{"add", "mcp-server", "context7", "npx", "--target", "antigravity-cli", "--dry-run"},
			want: "requires --scope global for --target antigravity-cli",
			code: 1,
		},
		{
			name: "antigravity project scope",
			args: []string{"add", "mcp-server", "context7", "npx", "--target", "antigravity-cli", "--scope", "project", "--dry-run"},
			want: "supports --scope global for --target antigravity-cli",
			code: 1,
		},
		{
			name: "multiple mcp targets",
			args: []string{"add", "mcp-server", "context7", "npx", "--target", "claude-code", "--target", "antigravity-cli", "--scope", "global", "--dry-run"},
			want: "accepts at most one distinct --target",
			code: 1,
		},
		{
			name: "absolute command",
			args: []string{"add", "mcp-server", "context7", "/usr/bin/node", "--dry-run"},
			want: "portable command token",
			code: 1,
		},
		{
			name: "relative path command",
			args: []string{"add", "mcp-server", "context7", "./server", "--dry-run"},
			want: "portable command token",
			code: 1,
		},
		{
			name: "shell command",
			args: []string{"add", "mcp-server", "context7", "node server.js", "--dry-run"},
			want: "portable command token",
			code: 1,
		},
		{
			name: "pipe command",
			args: []string{"add", "mcp-server", "context7", "node|cat", "--dry-run"},
			want: "portable command token",
			code: 1,
		},
		{
			name: "redirect command",
			args: []string{"add", "mcp-server", "context7", "node>out", "--dry-run"},
			want: "stable token",
			code: 1,
		},
		{
			name: "semicolon command",
			args: []string{"add", "mcp-server", "context7", "node;rm", "--dry-run"},
			want: "portable command token",
			code: 1,
		},
		{
			name: "windows path command",
			args: []string{"add", "mcp-server", "context7", `C:\node.exe`, "--dry-run"},
			want: "portable command token",
			code: 1,
		},
		{
			name: "command whitespace",
			args: []string{"add", "mcp-server", "context7", " npx", "--dry-run"},
			want: "must not contain leading or trailing whitespace",
			code: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			original := "version = 1\ntargets = [\"claude-code\"]\n"
			testkit.WriteFile(t, tempDir, "daem.toml", original)
			args := append([]string{}, test.args...)
			args = append(args, "--manifest", manifestPath)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI(args, &stdout, &stderr)
			if exitCode != test.code {
				t.Fatalf("exitCode = %d, want %d, stdout = %q, stderr = %q", exitCode, test.code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
			testkit.AssertFileContent(t, manifestPath, original)
			testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
			testkit.AssertPathMissing(t, filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath))
			testkit.AssertPathMissing(t, testkit.AuthoringTransactionDir(filepath.Join(tempDir, ".daem")))
		})
	}
}

func TestRunAddMCPServerPlansUserConfigGlobalScopeWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(tempDir, "home"))
	paths, err := daempaths.Resolve("")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	original := "version = 1\ntargets = [\"claude-code\"]\n"
	testkit.WriteFile(t, filepath.Dir(paths.ManifestPath), filepath.Base(paths.ManifestPath), original)
	cwd := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	testkit.WithWorkingDirectory(t, cwd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{
		"add", "mcp-server", "context7", "npx",
		"--dry-run",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("exitCode = %d, want 0, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `scope = "global"`) {
		t.Fatalf("stdout = %q, want user-default global scope", stdout.String())
	}
	testkit.AssertFileContent(t, paths.ManifestPath, original)
	testkit.AssertPathMissing(t, paths.LockfilePath)
	testkit.AssertPathMissing(t, filepath.Join(cwd, aggregate.ClaudeProjectMCPConfigPath))
}

func installMCPProbeCommand(t *testing.T, root string, name string) string {
	t.Helper()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	probePath := filepath.Join(root, "probe-ran")
	commandPath := filepath.Join(binDir, name)
	content := "#!/bin/sh\necho ran > " + probePath + "\nexit 99\n"
	if err := os.WriteFile(commandPath, []byte(content), 0o700); err != nil {
		t.Fatalf("WriteFile probe command returned error: %v", err)
	}
	t.Setenv("PATH", binDir)
	return probePath
}
