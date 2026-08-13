package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestRunImportYesWritesClaudeProjectMCPManifestOnly(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	liveConfig := `{
  "project": "preserve",
  "mcpServers": {
    "context7": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@upstash/context7-mcp"],
      "env": {"CONTEXT7_API_TOKEN": "${CONTEXT7_API_TOKEN}"}
    },
    "remote": {
      "type": "http",
      "command": "npx"
    },
    "secret": {
      "type": "stdio",
      "command": "npx",
      "env": {"TOKEN": "literal-secret"}
    }
  }
}
`
	testkit.WriteFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, liveConfig)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{
		"imported: 1 resources",
		"mcp_servers=1",
		`resource="mcp_server/context7"`,
		`target=claude-code`,
		`scope=project`,
		`live=".mcp.json#/mcpServers/context7"`,
		`command="npx"`,
		`skip live=".mcp.json#/mcpServers/remote" reason=unsupported_mcp_transport`,
		`skip live=".mcp.json#/mcpServers/secret" reason=secret_literal_forbidden`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.lock.toml"))
	testkit.AssertPathMissing(t, filepath.Join(tempDir, ".daem"))
	testkit.AssertFileContent(t, filepath.Join(tempDir, aggregate.ClaudeProjectMCPConfigPath), liveConfig)

	server := readImportedMCPServer(t, outputPath, "context7")
	stdio := testkit.AssertSingleMCPStdioBinding(t, server, "context7", target.TargetClaudeCode, target.ScopeProject, "npx", []string{"-y", "@upstash/context7-mcp"})
	env := stdio.Env()
	envRef, ok := env["CONTEXT7_API_TOKEN"]
	if !ok || envRef.FromEnv() != "CONTEXT7_API_TOKEN" {
		t.Fatalf("env = %#v, want CONTEXT7_API_TOKEN from_env reference", env)
	}
}

func TestRunImportPreservesAbsoluteMCPCommandPathAndRelocksIt(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	absolutePath := filepath.Join(tempDir, "missing executable with spaces", "codegraph")
	testkit.WriteFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "codegraph": {
      "type": "stdio",
      "command": `+strconv.Quote(absolutePath)+`,
      "args": ["serve", "--mcp"]
    }
  }
}`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{"import", "--target", "claude-code", "--manifest", outputPath},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("import exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	imported := string(testkit.ReadFile(t, outputPath))
	wantCommand := `command = { path = ` + strconv.Quote(absolutePath) + ` }`
	if !strings.Contains(imported, wantCommand) {
		t.Fatalf("imported manifest = %q, want %q", imported, wantCommand)
	}
	server := readImportedMCPServer(t, outputPath, "codegraph")
	stdio := testkit.AssertSingleMCPStdioBinding(
		t,
		server,
		"codegraph",
		target.TargetClaudeCode,
		target.ScopeProject,
		absolutePath,
		[]string{"serve", "--mcp"},
	)
	if stdio.Command().Resolution() != desiredmcp.CommandResolutionAbsolutePath {
		t.Fatalf("imported command resolution = %q, want absolute_path", stdio.Command().Resolution())
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"lock", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("lock exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	locked, err := lockfile.Load(t.Context(), filepath.Join(tempDir, "daem.lock.toml"))
	if err != nil {
		t.Fatalf("Load imported lockfile returned error: %v", err)
	}
	subjects := locked.Locked.Subjects()
	if len(subjects) != 1 {
		t.Fatalf("locked subjects = %#v, want one", subjects)
	}
	plan, present := subjects[0].DelegatePlan()
	if !present || plan.Command().Executable() != absolutePath {
		t.Fatalf("delegate plan = %#v, present=%t, want exact path %q", plan, present, absolutePath)
	}
}

func TestRunImportDryRunReportsClaudeMCPWithoutWrites(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	testkit.WriteFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx", "args": ["-y", "@upstash/context7-mcp"]},
    "remote": {"type": "sse", "command": "npx"}
  }
}
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"import: 1 resources",
		"mcp_servers=1",
		`resource="mcp_server/context7"`,
		`skip live=".mcp.json#/mcpServers/remote" reason=unsupported_mcp_transport`,
		"next: rerun daem import without --dry-run",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, outputPath)
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
}

func TestRunImportYesReadsCurrentMCPAfterDryRunDrift(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	testkit.WriteFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx", "args": ["-y", "@old/mcp"]}
  }
}
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--manifest", outputPath, "--dry-run"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("dry-run exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	testkit.AssertPathMissing(t, outputPath)

	testkit.WriteFile(t, tempDir, aggregate.ClaudeProjectMCPConfigPath, `{
  "mcpServers": {
    "context7": {"type": "stdio", "command": "node", "args": ["server.js"]}
  }
}
`)
	stdout.Reset()
	stderr.Reset()
	exitCode = testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("yes exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `command="node"`) {
		t.Fatalf("stdout = %q, want yes run to report drifted command", stdout.String())
	}
	server := readImportedMCPServer(t, outputPath, "context7")
	testkit.AssertSingleMCPStdioBinding(t, server, "context7", target.TargetClaudeCode, target.ScopeProject, "node", []string{"server.js"})
}

func TestRunImportYesWritesOpenCodeProjectMCPManifestOnly(t *testing.T) {
	tempDir := t.TempDir()
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	testkit.WriteFile(t, tempDir, aggregate.OpenCodeProjectMCPConfigPath, `{
  "theme": "keep",
  "mcp": {
    "context7": {"type": "local", "command": ["npx", "-y", "@upstash/context7-mcp"]},
    "remote": {"type": "remote", "command": ["npx"]},
    "withEnv": {"type": "local", "command": ["npx"], "env": {"TOKEN": "${TOKEN}"}}
  }
}
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "opencode", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`resource="mcp_server/context7"`,
		`target=opencode`,
		`live="opencode.json#/mcp/context7"`,
		`skip live="opencode.json#/mcp/remote" reason=unsupported_mcp_transport`,
		`skip live="opencode.json#/mcp/withEnv" reason=unsupported_mcp_managed_field`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
	server := readImportedMCPServer(t, outputPath, "context7")
	stdio := testkit.AssertSingleMCPStdioBinding(t, server, "context7", target.TargetOpenCode, target.ScopeProject, "npx", []string{"-y", "@upstash/context7-mcp"})
	if env := stdio.Env(); len(env) != 0 {
		t.Fatalf("env = %#v, want none for OpenCode import", env)
	}
}

func TestRunImportYesWritesCodexGlobalMCPManifestOnly(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	livePath := filepath.Join(homeDir, ".codex", "config.toml")
	testkit.WriteFile(t, filepath.Dir(livePath), filepath.Base(livePath), `
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
env_vars = ["CONTEXT7_TOKEN"]

[mcp_servers.withEnv]
command = "npx"
env = { API_TOKEN = "SECRET_CANARY" }
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "codex", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`resource="mcp_server/context7"`,
		`target=codex`,
		`scope=global`,
		`live="` + livePath + `#/mcp_servers/context7"`,
		`skip live="` + livePath + `#/mcp_servers/withEnv" reason=secret_literal_forbidden`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
	server := readImportedMCPServer(t, outputPath, "context7")
	stdio := testkit.AssertSingleMCPStdioBinding(t, server, "context7", target.TargetCodex, target.ScopeGlobal, "npx", []string{"-y", "@upstash/context7-mcp"})
	env := stdio.Env()
	if len(env) != 1 || env["CONTEXT7_TOKEN"].FromEnv() != "CONTEXT7_TOKEN" {
		t.Fatalf("env = %#v, want same-name Codex global reference", env)
	}
}

func TestRunImportWritesSameNameCodexMCPAcrossSelectedScopes(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	testkit.WriteFile(t, tempDir, aggregate.CodexProjectMCPConfigPath, `
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp@project"]
`)
	globalPath := filepath.Join(homeDir, strings.TrimPrefix(aggregate.CodexGlobalMCPConfigPath, "~/"))
	testkit.WriteFile(t, filepath.Dir(globalPath), filepath.Base(globalPath), `
[mcp_servers.context7]
command = "uvx"
args = ["context7-mcp==1.2.3"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"import",
			"--target", "codex",
			"--scope", "project",
			"--scope", "global",
			"--manifest", outputPath,
		},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("import exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	server := readImportedMCPServer(t, outputPath, "context7")
	bindings := server.Bindings()
	if len(bindings) != 2 {
		t.Fatalf("bindings = %#v, want project and global", bindings)
	}
	wantCommands := map[target.Scope]string{
		target.ScopeProject: "npx",
		target.ScopeGlobal:  "uvx",
	}
	for _, binding := range bindings {
		stdio, ok := binding.Transport().Stdio()
		if !ok || binding.Target() != target.TargetCodex {
			t.Fatalf("binding = %#v, want Codex stdio", binding)
		}
		if got := stdio.Command().Executable(); got != wantCommands[binding.Scope()] {
			t.Fatalf("binding %s command = %q, want %q", binding.Scope(), got, wantCommands[binding.Scope()])
		}
	}
	if content := string(testkit.ReadFile(t, outputPath)); strings.Count(content, `name = "context7"`) != 2 {
		t.Fatalf("manifest = %q, want two same-name scoped MCP rows", content)
	}
}

func TestRunImportMergeNoopsExistingScopeAndAddsSameNameMCPAtMissingScope(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@project"]
`)
	testkit.WriteFile(t, tempDir, aggregate.CodexProjectMCPConfigPath, `
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp@project"]
`)
	globalPath := filepath.Join(homeDir, strings.TrimPrefix(aggregate.CodexGlobalMCPConfigPath, "~/"))
	testkit.WriteFile(t, filepath.Dir(globalPath), filepath.Base(globalPath), `
[mcp_servers.context7]
command = "uvx"
args = ["context7-mcp==1.2.3"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"import",
			"--target", "codex",
			"--scope", "project",
			"--scope", "global",
			"--manifest", outputPath,
			"--merge",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("import --merge exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `merge resource="mcp_server/context7" status=noop detail="existing mcp_server projection target=codex scope=project`) ||
		!strings.Contains(stdout.String(), `merge resource="mcp_server/context7" status=add detail="append imported mcp_server projection target=codex scope=global`) {
		t.Fatalf("stdout = %q, want project noop and global add", stdout.String())
	}

	server := readImportedMCPServer(t, outputPath, "context7")
	bindings := server.Bindings()
	if len(bindings) != 2 {
		t.Fatalf("bindings = %#v, want project and global", bindings)
	}
	wantCommands := map[target.Scope]string{
		target.ScopeProject: "npx",
		target.ScopeGlobal:  "uvx",
	}
	for _, binding := range bindings {
		stdio, ok := binding.Transport().Stdio()
		if !ok || binding.Target() != target.TargetCodex {
			t.Fatalf("binding = %#v, want Codex stdio", binding)
		}
		if got := stdio.Command().Executable(); got != wantCommands[binding.Scope()] {
			t.Fatalf("binding %s command = %q, want %q", binding.Scope(), got, wantCommands[binding.Scope()])
		}
	}
	if content := string(testkit.ReadFile(t, outputPath)); strings.Count(content, `name = "context7"`) != 2 {
		t.Fatalf("manifest = %q, want two same-name scoped MCP rows", content)
	}
}

func TestRunImportMergeJSONDistinguishesSameNameMCPProjectionSubjects(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.toml")
	testkit.WriteFile(t, tempDir, "daem.toml", `
version = 1
targets = ["codex"]

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@project"]

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "global"
transport = "stdio"
command = "uvx"
args = ["context7-mcp==1.2.3"]
`)
	testkit.WriteFile(t, tempDir, aggregate.CodexProjectMCPConfigPath, `
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp@project"]
`)
	globalPath := filepath.Join(homeDir, strings.TrimPrefix(aggregate.CodexGlobalMCPConfigPath, "~/"))
	testkit.WriteFile(t, filepath.Dir(globalPath), filepath.Base(globalPath), `
[mcp_servers.context7]
command = "uvx"
args = ["context7-mcp==1.2.3"]
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"import",
			"--target", "codex",
			"--scope", "project",
			"--scope", "global",
			"--manifest", outputPath,
			"--merge",
			"--dry-run",
			"--json",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("import --merge exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}

	payload := clijson.DecodeManifestAuthoring(t, stdout.Bytes())
	if len(payload.MergeResults) != 2 {
		t.Fatalf("merge results = %#v, want project and global rows", payload.MergeResults)
	}
	wantSubjects := []string{
		"projection/codex.project.mcp-server/context7",
		"projection/codex.global.mcp-server/context7",
	}
	for index, result := range payload.MergeResults {
		if result.ResourceID != "mcp_server/context7" || result.Status != "noop" || result.SubjectID != wantSubjects[index] {
			t.Fatalf("merge result %d = %#v, want subject %q", index, result, wantSubjects[index])
		}
	}
}

func TestRunImportMergeRejectsInheritedGlobalMCPScopeWithoutMutation(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.toml")
	original := []byte(`version = 1
targets = ["codex"]

[defaults]
scope = "global"

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
`)
	testkit.WriteFile(t, tempDir, "daem.toml", string(original))
	globalPath := filepath.Join(homeDir, strings.TrimPrefix(aggregate.CodexGlobalMCPConfigPath, "~/"))
	testkit.WriteFile(t, filepath.Dir(globalPath), filepath.Base(globalPath), `
[mcp_servers.context7]
command = "npx"
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI(
		[]string{
			"import",
			"--target", "codex",
			"--scope", "global",
			"--manifest", outputPath,
			"--merge",
		},
		&stdout,
		&stderr,
	)
	if exitCode == 0 || !strings.Contains(stderr.String(), "global MCP projection requires explicit scope") {
		t.Fatalf("import --merge exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no success envelope", stdout.String())
	}
	if got := testkit.ReadFile(t, outputPath); !bytes.Equal(got, original) {
		t.Fatalf("manifest changed after rejected merge:\n%s", got)
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.d"))
}

func TestRunImportYesWritesClaudeGlobalMCPManifestOnly(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	livePath := filepath.Join(homeDir, ".claude.json")
	testkit.WriteFile(t, filepath.Dir(livePath), filepath.Base(livePath), `{
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx", "args": ["-y", "@upstash/context7-mcp"]},
    "remote": {"type": "http", "url": "https://example.invalid/mcp"},
    "withEnv": {"type": "stdio", "command": "npx", "env": {"API_TOKEN": "${CLAUDE_GLOBAL_TOKEN}"}},
    "literalEnv": {"type": "stdio", "command": "npx", "env": {"API_TOKEN": "SECRET_CANARY"}}
  },
  "projects": {
    "/repo": {
      "mcpServers": {
        "projectOnly": {"type": "stdio", "command": "node"}
      }
    }
  }
}
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "claude-code", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`resource="mcp_server/context7"`,
		`target=claude-code`,
		`scope=global`,
		`live="` + livePath + `#/mcpServers/context7"`,
		`resource="mcp_server/withEnv"`,
		`skip live="` + livePath + `#/mcpServers/remote" reason=unsupported_mcp_transport`,
		`skip live="` + livePath + `#/mcpServers/literalEnv" reason=secret_literal_forbidden`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	for _, forbidden := range []string{"SECRET_CANARY", "projectOnly"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("stdout = %q, must not contain %q", stdout.String(), forbidden)
		}
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
	server := readImportedMCPServer(t, outputPath, "context7")
	stdio := testkit.AssertSingleMCPStdioBinding(t, server, "context7", target.TargetClaudeCode, target.ScopeGlobal, "npx", []string{"-y", "@upstash/context7-mcp"})
	if env := stdio.Env(); len(env) != 0 {
		t.Fatalf("env = %#v, want none for env-free Claude global import", env)
	}
	withEnv := readImportedMCPServer(t, outputPath, "withEnv")
	withEnvStdio := testkit.AssertSingleMCPStdioBinding(
		t,
		withEnv,
		"withEnv",
		target.TargetClaudeCode,
		target.ScopeGlobal,
		"npx",
		nil,
	)
	if env := withEnvStdio.Env(); len(env) != 1 ||
		env["API_TOKEN"].FromEnv() != "CLAUDE_GLOBAL_TOKEN" {
		t.Fatalf("env = %#v, want exact Claude global environment reference", env)
	}
}

func TestRunImportYesWritesOpenCodeGlobalMCPManifestOnly(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	livePath := filepath.Join(homeDir, ".config", "opencode", "opencode.json")
	testkit.WriteFile(t, filepath.Dir(livePath), filepath.Base(livePath), `{
  "mcp": {
    "context7": {"type": "local", "command": ["npx", "-y", "@upstash/context7-mcp"]},
    "remote": {"type": "remote", "command": ["npx"]},
    "withAlias": {"type": "local", "command": ["npx"], "environment": {"CHILD_TOKEN": "{env:SOURCE_TOKEN}"}},
    "literalEnv": {"type": "local", "command": ["npx"], "environment": {"TOKEN": "SECRET_CANARY"}}
  }
}
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "opencode", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`resource="mcp_server/context7"`,
		`target=opencode`,
		`scope=global`,
		`live="` + livePath + `#/mcp/context7"`,
		`resource="mcp_server/withAlias"`,
		`skip live="` + livePath + `#/mcp/remote" reason=unsupported_mcp_transport`,
		`skip live="` + livePath + `#/mcp/literalEnv" reason=secret_literal_forbidden`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
	server := readImportedMCPServer(t, outputPath, "context7")
	stdio := testkit.AssertSingleMCPStdioBinding(t, server, "context7", target.TargetOpenCode, target.ScopeGlobal, "npx", []string{"-y", "@upstash/context7-mcp"})
	if env := stdio.Env(); len(env) != 0 {
		t.Fatalf("env = %#v, want none for OpenCode global import", env)
	}
	withAlias := readImportedMCPServer(t, outputPath, "withAlias")
	withAliasStdio := testkit.AssertSingleMCPStdioBinding(
		t,
		withAlias,
		"withAlias",
		target.TargetOpenCode,
		target.ScopeGlobal,
		"npx",
		nil,
	)
	if env := withAliasStdio.Env(); len(env) != 1 ||
		env["CHILD_TOKEN"].FromEnv() != "SOURCE_TOKEN" {
		t.Fatalf("env = %#v, want exact OpenCode global environment reference", env)
	}
}

func TestRunImportYesWritesAntigravityGlobalMCPManifestOnly(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	t.Setenv("HOME", homeDir)
	testkit.WithWorkingDirectory(t, tempDir)
	outputPath := filepath.Join(tempDir, "daem.imported.toml")
	livePath := filepath.Join(homeDir, ".gemini", "config", "mcp_config.json")
	absolutePath := filepath.Join(tempDir, "missing executable with spaces", "codegraph")
	testkit.WriteFile(t, filepath.Dir(livePath), filepath.Base(livePath), `{
  "mcpServers": {
    "context7": {"command": `+strconv.Quote(absolutePath)+`, "args": ["serve", "--mcp"]},
    "withHeaders": {"command": "npx", "headers": {"Authorization": "Bearer secret"}}
  }
}
`)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := testkit.RunVerboseCLI([]string{"import", "--target", "antigravity-cli", "--scope", "global", "--manifest", outputPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %q, stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		`resource="mcp_server/context7"`,
		`target=antigravity-cli`,
		`scope=global`,
		`live="` + livePath + `#/mcpServers/context7"`,
		`command=` + strconv.Quote(absolutePath),
		`skip live="` + livePath + `#/mcpServers/withHeaders" reason=unsupported_mcp_managed_field`,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	testkit.AssertPathMissing(t, filepath.Join(tempDir, "daem.imported.d"))
	if imported := string(testkit.ReadFile(t, outputPath)); !strings.Contains(
		imported,
		`command = { path = `+strconv.Quote(absolutePath)+` }`,
	) {
		t.Fatalf("imported manifest = %q, want exact path object", imported)
	}
	server := readImportedMCPServer(t, outputPath, "context7")
	stdio := testkit.AssertSingleMCPStdioBinding(
		t,
		server,
		"context7",
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		absolutePath,
		[]string{"serve", "--mcp"},
	)
	if stdio.Command().Resolution() != desiredmcp.CommandResolutionAbsolutePath {
		t.Fatalf("command resolution = %q, want absolute_path", stdio.Command().Resolution())
	}
	if env := stdio.Env(); len(env) != 0 {
		t.Fatalf("env = %#v, want none for Antigravity import", env)
	}
}

func readImportedMCPServer(t *testing.T, outputPath string, name string) desiredmcp.Server {
	t.Helper()
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile manifest returned error: %v", err)
	}
	config, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("generated manifest did not parse: %v\n%s", err, content)
	}
	if len(config.Skills()) != 0 || len(config.Hooks()) != 0 || len(config.Instructions()) != 0 {
		t.Fatalf("config imported non-MCP resources: instructions=%#v skills=%#v hooks=%#v", config.Instructions(), config.Skills(), config.Hooks())
	}
	return findImportedMCPServer(t, config.MCPServers(), name)
}
