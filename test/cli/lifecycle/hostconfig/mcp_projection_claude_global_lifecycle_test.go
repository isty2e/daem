package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestMCPPublicCLIClaudeGlobalProjectionLifecyclePreservesSharedUserState(t *testing.T) {
	project := newMCPCLIProject(t)
	homeDir := filepath.Join(project.root, "home")
	t.Setenv("HOME", homeDir)
	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "claude-code",
		Scope:   "global",
		Command: "npx",
		Args:    []string{"-y", "@example/mcp-server"},
	})
	hostConfigPath := filepath.Join(homeDir, ".claude.json")
	writeClaudeGlobalMCPConfigWithSiblings(t, hostConfigPath, "")
	runMCPLock(t, project)

	exitCode, stdout, stderr := runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply create exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	testkit.AssertClaudeGlobalMCPConfigEquivalent(t, hostConfigPath, "context7", "npx", []string{"-y", "@example/mcp-server"})
	assertClaudeGlobalMCPConfigPreservesSharedUserState(t, hostConfigPath)
	assertGlobalMCPStateSubject(t, loadMCPStatefile(t, project.root), globalMCPStateWant{
		namespace:   "claude-code.global.mcp-server",
		target:      target.TargetClaudeCode,
		scope:       target.ScopeGlobal,
		path:        aggregate.ClaudeGlobalMCPConfigPath,
		contentPath: mcpcodec.ClaudeGlobalMCPContentPath("context7"),
		serverID:    "context7",
	})
	assertNoPublicMCPOutputLeaks(t, stdout)

	exitCode, stdout, stderr = runMCPCLI(t, "status", "--manifest", project.manifestPath, "--target", "claude-code", "--check", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("status clean exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	statusPayload := clijson.DecodePlan(t, []byte(stdout))
	assertMCPJSONDimension(t, statusPayload, "global_projection", "projected", "")
	assertMCPJSONDimension(t, statusPayload, "same_scope_ownership", "managed", "")
	assertMCPJSONDimension(t, statusPayload, "runtime_launcher", "not_probed", "RUNTIME_NOT_PROBED")
	assertNoPublicMCPOutputLeaks(t, stdout)

	writeMCPManifest(t, project.root, mcpManifestSpec{
		Target:  "claude-code",
		Scope:   "global",
		Command: "node",
		Args:    []string{"server.js"},
	})
	runMCPLock(t, project)
	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--target", "claude-code", "--yes", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply update exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	testkit.AssertClaudeGlobalMCPConfigEquivalent(t, hostConfigPath, "context7", "node", []string{"server.js"})
	assertClaudeGlobalMCPConfigPreservesSharedUserState(t, hostConfigPath)
	assertNoPublicMCPOutputLeaks(t, stdout)

	writeMCPManifestWithoutServers(t, project.root)
	runMCPLock(t, project)
	exitCode, stdout, stderr = runMCPCLI(t, "apply", "--manifest", project.manifestPath, "--yes", "--json")
	if exitCode != 0 || stderr != "" {
		t.Fatalf("apply removal exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	testkit.AssertClaudeGlobalMCPConfigMissing(t, hostConfigPath, "context7")
	assertClaudeGlobalMCPConfigPreservesSharedUserState(t, hostConfigPath)
	assertClaudeGlobalMCPStateSubjectMissing(t, loadMCPStatefile(t, project.root), "context7")
	assertNoPublicMCPOutputLeaks(t, stdout)
}

func writeClaudeGlobalMCPConfigWithSiblings(t *testing.T, hostConfigPath string, context7Entry string) {
	t.Helper()
	entries := []string{
		`"manual": {"type": "stdio", "command": "manual", "args": ["keep"]}`,
	}
	if context7Entry != "" {
		entries = append([]string{context7Entry}, entries...)
	}
	testkit.WriteFile(t, filepath.Dir(hostConfigPath), filepath.Base(hostConfigPath), `{
  "mcpServers": {
    `+strings.Join(entries, ",\n    ")+`
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
}

func assertClaudeGlobalMCPConfigPreservesSharedUserState(t *testing.T, hostConfigPath string) {
	t.Helper()
	content := testkit.ReadFile(t, hostConfigPath)
	var config map[string]json.RawMessage
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("decode Claude user config: %v", err)
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(config["mcpServers"], &servers); err != nil {
		t.Fatalf("decode mcpServers object: %v", err)
	}
	if _, ok := servers["manual"]; !ok {
		t.Fatal("manual top-level MCP server key was not preserved")
	}
	var manual mcpcodec.ClaudeGlobalMCPServerEntry
	if entry, present, err := mcpcodec.ExtractClaudeGlobalMCPServerProjection(content, "manual"); err != nil {
		t.Fatalf("manual MCP extraction returned error: %v", err)
	} else if !present {
		t.Fatal("manual top-level MCP server was not preserved")
	} else {
		manual = entry
	}
	if manual.Command != "manual" {
		t.Fatalf("manual entry = %#v, want preserved command", manual)
	}
	var projects map[string]struct {
		MCPServers map[string]mcpcodec.ClaudeGlobalMCPServerEntry `json:"mcpServers"`
	}
	if err := json.Unmarshal(config["projects"], &projects); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	if projects["/repo"].MCPServers["context7"].Command != "node" {
		t.Fatalf("projects = %#v, want project shadow preserved", projects)
	}
	if _, ok := config["oauth"]; !ok {
		t.Fatal("oauth sibling was not preserved")
	}
	if _, ok := config["trust"]; !ok {
		t.Fatal("trust sibling was not preserved")
	}
}

func assertClaudeGlobalMCPStateSubjectMissing(t *testing.T, file durable.Snapshot, serverID string) {
	t.Helper()
	for _, state := range file.ManagedAggregates() {
		subject := state.Subject()
		if subject.Kind() == topology.SubjectProjection &&
			subject.Namespace() == "claude-code.global.mcp-server" &&
			subject.Key() == serverID {
			t.Fatalf("Claude global MCP subject state for %q unexpectedly present: %#v", serverID, state)
		}
	}
}
