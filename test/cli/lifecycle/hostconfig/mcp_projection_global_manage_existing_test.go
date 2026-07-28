package cli_test

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestMCPPublicCLIManageExistingAdoptsExactCodexGlobalEntry(t *testing.T) {
	project := newMCPCLIProject(t)
	homeDir := filepath.Join(project.root, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_TOKEN", "runtime-only")
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "codex",
		Scope:   "global",
		Command: "npx",
		Args:    []string{"-y", "@example/mcp-server"},
		Env:     map[string]string{"CODEX_TOKEN": "CODEX_TOKEN"},
	})
	hostConfigPath := filepath.Join(homeDir, ".codex", "config.toml")
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), `
model = "keep"

[mcp_servers.context7]
command = "npx"
args = ["-y", "@example/mcp-server"]
env_vars = ["CODEX_TOKEN"]

[mcp_servers.manual]
command = "node"
args = ["manual.js"]
env = { MANUAL_TOKEN = "keep-literal" }
`)
	beforeConfig := testkit.ReadFile(t, hostConfigPath)
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "codex", "--dry-run", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("apply without manage-existing exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	ordinary := clijson.DecodePlan(t, []byte(stdout))
	assertGlobalPlanSubjectActionReason(t, ordinary, "error", "unmanaged_output_exists", globalMCPActionWant{
		namespace:   "codex.global.mcp-server",
		configPath:  aggregate.CodexGlobalMCPConfigPath,
		contentPath: mcpcodec.CodexGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "codex", "--manage-existing", "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply manage-existing dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	manageDryRun := clijson.DecodePlan(t, []byte(stdout))
	assertGlobalPlanSubjectActionReason(t, manageDryRun, "record", "managed_existing", globalMCPActionWant{
		namespace:   "codex.global.mcp-server",
		configPath:  aggregate.CodexGlobalMCPConfigPath,
		contentPath: mcpcodec.CodexGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertBytesEqual(t, testkit.ReadFile(t, hostConfigPath), beforeConfig, "Codex global manage-existing dry-run config")
	assertMCPStatefileMissing(t, project.root, "Codex global manage-existing dry-run")
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "codex", "--manage-existing", "--yes", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply manage-existing yes exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	manageResult := clijson.DecodeApplyResult(t, []byte(stdout))
	assertGlobalApplyResultSubjectActionReason(t, manageResult, "record", "managed_existing", globalMCPActionWant{
		namespace:   "codex.global.mcp-server",
		configPath:  aggregate.CodexGlobalMCPConfigPath,
		contentPath: mcpcodec.CodexGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertBytesEqual(t, testkit.ReadFile(t, hostConfigPath), beforeConfig, "Codex global manage-existing yes config")
	assertGlobalMCPStateSubject(t, loadMCPStatefile(t, project.root), globalMCPStateWant{
		namespace:   "codex.global.mcp-server",
		target:      target.TargetCodex,
		scope:       target.ScopeGlobal,
		path:        aggregate.CodexGlobalMCPConfigPath,
		contentPath: mcpcodec.CodexGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertNoPublicMCPOutputLeaks(t, stdout)
}

func TestMCPPublicCLIManageExistingAdoptsExactClaudeGlobalEntry(t *testing.T) {
	const sourceName = "DAEM_TEST_CLAUDE_GLOBAL_TOKEN"
	t.Setenv(sourceName, "claude-global-manage-existing-secret")
	project := newMCPCLIProject(t)
	homeDir := filepath.Join(project.root, "home")
	t.Setenv("HOME", homeDir)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "claude-code",
		Scope:   "global",
		Command: "npx",
		Args:    []string{"-y", "@example/mcp-server"},
		Env:     map[string]string{"API_TOKEN": sourceName},
	})
	hostConfigPath := filepath.Join(homeDir, ".claude.json")
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), `{
  "mcpServers": {
    "context7": {"type": "stdio", "command": "npx", "args": ["-y", "@example/mcp-server"], "env": {"API_TOKEN": "${DAEM_TEST_CLAUDE_GLOBAL_TOKEN}"}},
    "manual": {"type": "http", "url": "https://example.invalid/mcp", "headers": {"Authorization": "Bearer SECRET_CANARY"}}
  },
  "projects": {
    "/repo": {
      "mcpServers": {
        "context7": {"type": "stdio", "command": "node", "args": ["project-shadow.js"]}
      }
    }
  },
  "oauth": {"keep": true},
  "trust": {"keep": true}
}`)
	beforeConfig := testkit.ReadFile(t, hostConfigPath)
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--manage-existing", "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply manage-existing dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	manageDryRun := clijson.DecodePlan(t, []byte(stdout))
	assertGlobalPlanSubjectActionReason(t, manageDryRun, "record", "managed_existing", globalMCPActionWant{
		namespace:   "claude-code.global.mcp-server",
		configPath:  aggregate.ClaudeGlobalMCPConfigPath,
		contentPath: mcpcodec.ClaudeGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertBytesEqual(t, testkit.ReadFile(t, hostConfigPath), beforeConfig, "Claude global manage-existing dry-run config")
	assertMCPStatefileMissing(t, project.root, "Claude global manage-existing dry-run")
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--manage-existing", "--yes", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply manage-existing yes exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	manageResult := clijson.DecodeApplyResult(t, []byte(stdout))
	assertGlobalApplyResultSubjectActionReason(t, manageResult, "record", "managed_existing", globalMCPActionWant{
		namespace:   "claude-code.global.mcp-server",
		configPath:  aggregate.ClaudeGlobalMCPConfigPath,
		contentPath: mcpcodec.ClaudeGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertBytesEqual(t, testkit.ReadFile(t, hostConfigPath), beforeConfig, "Claude global manage-existing yes config")
	assertGlobalMCPStateSubject(t, loadMCPStatefile(t, project.root), globalMCPStateWant{
		namespace:   "claude-code.global.mcp-server",
		target:      target.TargetClaudeCode,
		scope:       target.ScopeGlobal,
		path:        aggregate.ClaudeGlobalMCPConfigPath,
		contentPath: mcpcodec.ClaudeGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertNoPublicMCPOutputLeaks(t, stdout)
}

func TestMCPPublicCLIManageExistingAdoptsExactOpenCodeGlobalEntry(t *testing.T) {
	project := newMCPCLIProject(t)
	homeDir := filepath.Join(project.root, "home")
	t.Setenv("HOME", homeDir)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "opencode",
		Scope:   "global",
		Command: "npx",
		Args:    []string{"-y", "@example/mcp-server"},
	})
	hostConfigPath := filepath.Join(homeDir, ".config", "opencode", "opencode.json")
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), `{
  "model": "keep",
  "mcp": {
    "context7": {"type": "local", "command": ["npx", "-y", "@example/mcp-server"]},
    "manual": {"type": "remote", "url": "https://example.invalid/mcp", "headers": {"Authorization": "Bearer SECRET_CANARY"}}
  }
}`)
	beforeConfig := testkit.ReadFile(t, hostConfigPath)
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "opencode", "--dry-run", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("apply without manage-existing exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	ordinary := clijson.DecodePlan(t, []byte(stdout))
	assertGlobalPlanSubjectActionReason(t, ordinary, "error", "unmanaged_output_exists", globalMCPActionWant{
		namespace:   "opencode.global.mcp-server",
		configPath:  aggregate.OpenCodeGlobalMCPConfigPath,
		contentPath: mcpcodec.OpenCodeGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "opencode", "--manage-existing", "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply manage-existing dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	manageDryRun := clijson.DecodePlan(t, []byte(stdout))
	assertGlobalPlanSubjectActionReason(t, manageDryRun, "record", "managed_existing", globalMCPActionWant{
		namespace:   "opencode.global.mcp-server",
		configPath:  aggregate.OpenCodeGlobalMCPConfigPath,
		contentPath: mcpcodec.OpenCodeGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertBytesEqual(t, testkit.ReadFile(t, hostConfigPath), beforeConfig, "OpenCode global manage-existing dry-run config")
	assertMCPStatefileMissing(t, project.root, "OpenCode global manage-existing dry-run")
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "opencode", "--manage-existing", "--yes", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply manage-existing yes exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	manageResult := clijson.DecodeApplyResult(t, []byte(stdout))
	assertGlobalApplyResultSubjectActionReason(t, manageResult, "record", "managed_existing", globalMCPActionWant{
		namespace:   "opencode.global.mcp-server",
		configPath:  aggregate.OpenCodeGlobalMCPConfigPath,
		contentPath: mcpcodec.OpenCodeGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertBytesEqual(t, testkit.ReadFile(t, hostConfigPath), beforeConfig, "OpenCode global manage-existing yes config")
	assertGlobalMCPStateSubject(t, loadMCPStatefile(t, project.root), globalMCPStateWant{
		namespace:   "opencode.global.mcp-server",
		target:      target.TargetOpenCode,
		scope:       target.ScopeGlobal,
		path:        aggregate.OpenCodeGlobalMCPConfigPath,
		contentPath: mcpcodec.OpenCodeGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertNoPublicMCPOutputLeaks(t, stdout)
}

func TestMCPPublicCLIManageExistingAdoptsExactAntigravityGlobalEntry(t *testing.T) {
	project := newMCPCLIProject(t)
	homeDir := filepath.Join(project.root, "home")
	t.Setenv("HOME", homeDir)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "antigravity-cli",
		Scope:   "global",
		Command: "npx",
		Args:    []string{"-y", "@example/mcp-server"},
	})
	hostConfigPath := filepath.Join(homeDir, ".gemini", "config", "mcp_config.json")
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), `{
  "mcpServers": {
    "context7": {"command": "npx", "args": ["-y", "@example/mcp-server"]},
    "manual": {"serverUrl": "https://example.invalid/mcp", "headers": {"Authorization": "Bearer SECRET_CANARY"}}
  },
  "global": "keep"
}`)
	beforeConfig := testkit.ReadFile(t, hostConfigPath)
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "antigravity-cli", "--dry-run", "--json")
	if exitCode != 1 || stderr != "" {
		t.Fatalf("apply without manage-existing exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	ordinary := clijson.DecodePlan(t, []byte(stdout))
	assertGlobalPlanSubjectActionReason(t, ordinary, "error", "unmanaged_output_exists", globalMCPActionWant{
		namespace:   "antigravity-cli.global.mcp-server",
		configPath:  aggregate.AntigravityGlobalMCPConfigPath,
		contentPath: mcpcodec.AntigravityGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "antigravity-cli", "--manage-existing", "--dry-run", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply manage-existing dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	manageDryRun := clijson.DecodePlan(t, []byte(stdout))
	assertGlobalPlanSubjectActionReason(t, manageDryRun, "record", "managed_existing", globalMCPActionWant{
		namespace:   "antigravity-cli.global.mcp-server",
		configPath:  aggregate.AntigravityGlobalMCPConfigPath,
		contentPath: mcpcodec.AntigravityGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertBytesEqual(t, testkit.ReadFile(t, hostConfigPath), beforeConfig, "Antigravity global manage-existing dry-run config")
	assertMCPStatefileMissing(t, project.root, "Antigravity global manage-existing dry-run")
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "antigravity-cli", "--manage-existing", "--yes", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply manage-existing yes exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	manageResult := clijson.DecodeApplyResult(t, []byte(stdout))
	assertGlobalApplyResultSubjectActionReason(t, manageResult, "record", "managed_existing", globalMCPActionWant{
		namespace:   "antigravity-cli.global.mcp-server",
		configPath:  aggregate.AntigravityGlobalMCPConfigPath,
		contentPath: mcpcodec.AntigravityGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertBytesEqual(t, testkit.ReadFile(t, hostConfigPath), beforeConfig, "Antigravity global manage-existing yes config")
	assertGlobalMCPStateSubject(t, loadMCPStatefile(t, project.root), globalMCPStateWant{
		namespace:   "antigravity-cli.global.mcp-server",
		target:      target.TargetAntigravityCLI,
		scope:       target.ScopeGlobal,
		path:        aggregate.AntigravityGlobalMCPConfigPath,
		contentPath: mcpcodec.AntigravityGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertNoPublicMCPOutputLeaks(t, stdout)
}

func TestMCPPublicCLIManageExistingRejectsNonEquivalentGlobalEntries(t *testing.T) {
	tests := []struct {
		name        string
		targetName  string
		namespace   string
		configPath  string
		hostRelPath string
		contentPath string
		hostContent string
	}{
		{
			name:        "codex global",
			targetName:  "codex",
			namespace:   "codex.global.mcp-server",
			configPath:  aggregate.CodexGlobalMCPConfigPath,
			hostRelPath: filepath.Join(".codex", "config.toml"),
			contentPath: mcpcodec.CodexGlobalMCPContentPath("context7"),
			hostContent: `
[mcp_servers.context7]
command = "node"
args = ["server.js"]

[mcp_servers.manual]
command = "npx"
args = ["manual", "SECRET_CANARY"]
`,
		},
		{
			name:        "opencode global",
			targetName:  "opencode",
			namespace:   "opencode.global.mcp-server",
			configPath:  aggregate.OpenCodeGlobalMCPConfigPath,
			hostRelPath: filepath.Join(".config", "opencode", "opencode.json"),
			contentPath: mcpcodec.OpenCodeGlobalMCPContentPath("context7"),
			hostContent: `{
  "mcp": {
    "context7": {"type": "local", "command": ["node", "server.js"]},
    "manual": {"type": "remote", "url": "https://example.invalid/mcp", "headers": {"Authorization": "Bearer SECRET_CANARY"}}
  }
}`,
		},
		{
			name:        "antigravity global",
			targetName:  "antigravity-cli",
			namespace:   "antigravity-cli.global.mcp-server",
			configPath:  aggregate.AntigravityGlobalMCPConfigPath,
			hostRelPath: filepath.Join(".gemini", "config", "mcp_config.json"),
			contentPath: mcpcodec.AntigravityGlobalMCPContentPath("context7"),
			hostContent: `{
  "mcpServers": {
    "context7": {"command": "node", "args": ["server.js"]},
    "manual": {"serverUrl": "https://example.invalid/mcp", "headers": {"Authorization": "Bearer SECRET_CANARY"}}
  }
}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project := newMCPCLIProject(t)
			homeDir := filepath.Join(project.root, "home")
			t.Setenv("HOME", homeDir)
			writeMCPManifest(t, project.root, mcpManifestSpec{
				Target:  test.targetName,
				Scope:   "global",
				Command: "npx",
				Args:    []string{"-y", "@example/mcp-server"},
			})
			hostConfigPath := filepath.Join(homeDir, test.hostRelPath)
			testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), test.hostContent)
			beforeConfig := testkit.ReadFile(t, hostConfigPath)
			runMCPLock(t, project)

			exitCode, stdout, stderr := runMCPCLI(
				t,
				"apply",
				"--manifest", project.manifestPath,
				"--target", test.targetName,
				"--manage-existing",
				"--dry-run",
				"--json",
			)
			if exitCode != 1 || stderr != "" {
				t.Fatalf("manage-existing dry-run exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
			payload := clijson.DecodePlan(t, []byte(stdout))
			assertGlobalPlanSubjectActionReason(t, payload, "error", "unmanaged_output_exists", globalMCPActionWant{
				namespace:   test.namespace,
				configPath:  test.configPath,
				contentPath: test.contentPath,
				serverID:    "context7",
			})
			assertMCPJSONDimension(t, payload, "global_projection", "unmanaged_same_name", "ROUTE_PREEXISTING_UNOWNED")
			assertBytesEqual(t, testkit.ReadFile(t, hostConfigPath), beforeConfig, test.name+" config")
			assertMCPStatefileMissing(t, project.root, test.name+" failed manage-existing")
			assertNoPublicMCPOutputLeaks(t, stdout)
		})
	}
}

type globalMCPActionWant struct {
	namespace   string
	configPath  string
	contentPath string
	serverID    string
}

type globalMCPStateWant struct {
	namespace   string
	target      target.Target
	scope       target.Scope
	path        string
	contentPath string
	serverID    string
}

func assertGlobalPlanSubjectActionReason(
	t *testing.T,
	payload clijson.Plan,
	kind string,
	reason string,
	want globalMCPActionWant,
) {
	t.Helper()
	for _, action := range payload.Actions {
		if action.Kind != kind || action.Reason != reason || action.Subject == nil {
			continue
		}
		if action.Subject.Kind == string(topology.SubjectProjection) &&
			action.Subject.Namespace == want.namespace &&
			action.Subject.Name == want.serverID &&
			action.Projection != nil &&
			action.Projection.ConfigPath == want.configPath &&
			action.Projection.ContentPath == want.contentPath {
			return
		}
	}
	t.Fatalf("actions = %#v, want %s/%s global MCP subject action %#v", payload.Actions, kind, reason, want)
}

func assertGlobalApplyResultSubjectActionReason(
	t *testing.T,
	payload clijson.ApplyResult,
	kind string,
	reason string,
	want globalMCPActionWant,
) {
	t.Helper()
	for _, action := range payload.Actions {
		if action.Kind != kind || action.Reason != reason || action.Subject == nil {
			continue
		}
		if action.Subject.Kind == string(topology.SubjectProjection) &&
			action.Subject.Namespace == want.namespace &&
			action.Subject.Name == want.serverID &&
			action.Projection != nil &&
			action.Projection.ConfigPath == want.configPath &&
			action.Projection.ContentPath == want.contentPath {
			return
		}
	}
	t.Fatalf("actions = %#v, want %s/%s global MCP subject action %#v", payload.Actions, kind, reason, want)
}

func assertGlobalMCPStateSubject(t *testing.T, file durable.Snapshot, want globalMCPStateWant) {
	t.Helper()
	for _, state := range file.ManagedAggregates() {
		subject := state.Subject()
		if subject.Kind() != topology.SubjectProjection ||
			subject.Namespace() != want.namespace ||
			subject.Key() != want.serverID {
			continue
		}
		contribution := state.Contribution()
		if contribution.Target() != want.target ||
			contribution.Scope() != want.scope ||
			contribution.AggregateRoot().String() != want.path ||
			contribution.ContentPath() != want.contentPath ||
			contribution.CanonicalContribution() == "" {
			t.Fatalf("MCP aggregate state = %#v, want global MCP state subject %#v", state, want)
		}
		return
	}
	t.Fatalf("MCP state subject %#v not found in %#v", want, file.ManagedAggregates())
}

func assertBytesEqual(t *testing.T, got []byte, want []byte, label string) {
	t.Helper()
	if string(got) != string(want) {
		t.Fatalf("%s changed:\ngot  %s\nwant %s", label, got, want)
	}
}
