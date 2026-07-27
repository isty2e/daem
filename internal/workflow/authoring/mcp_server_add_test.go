package authoring

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestMCPServerAddBehaviorAppendsFirstSliceDeclaration(t *testing.T) {
	original := []byte("version = 1\ntargets = [\"codex\"]\n")
	server, err := MCPServerFromAddRequest(AddMCPServerRequest{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
		Env: []MCPServerEnvAssignment{
			{Name: "API_TOKEN", FromEnv: "CONTEXT7_API_TOKEN"},
		},
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}}, daempaths.ManifestOriginExplicit)
	if err != nil {
		t.Fatalf("MCPServerFromAddRequest returned error: %v", err)
	}
	updated, changeKind, err := ApplyAddMCPServerToManifest(original, server, declaration.ManifestHeader{})
	if err != nil {
		t.Fatalf("ApplyAddMCPServerToManifest returned error: %v", err)
	}
	if changeKind != "append mcp_server resource" {
		t.Fatalf("changeKind = %q, want append", changeKind)
	}
	for _, want := range []string{
		"[[mcp_server]]",
		`name = "context7"`,
		`targets = ["claude-code"]`,
		`scope = "project"`,
		`transport = "stdio"`,
		`command = "npx"`,
		`args = ["-y", "@upstash/context7-mcp@1.2.3"]`,
		`env = { API_TOKEN = { from_env = "CONTEXT7_API_TOKEN" } }`,
	} {
		if !strings.Contains(string(updated), want) {
			t.Fatalf("updated = %q, want %q", updated, want)
		}
	}
}

func TestMCPServerAddBehaviorWarnsForFloatingDelegatePackage(t *testing.T) {
	document := ManifestDocument{
		Content: []byte("version = 1\ntargets = [\"claude-code\"]\n"),
	}
	change, err := BuildAddMCPServerChange(document, AddMCPServerRequest{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp"},
		Env: []MCPServerEnvAssignment{
			{Name: "API_TOKEN", FromEnv: "CONTEXT7_API_TOKEN"},
		},
	})
	if err != nil {
		t.Fatalf("BuildAddMCPServerChange returned error: %v", err)
	}
	if len(change.Warnings) != 1 {
		t.Fatalf("Warnings = %#v, want one floating package warning", change.Warnings)
	}
	warning := change.Warnings[0]
	for _, want := range []string{
		`mcp_server "context7"`,
		"floating delegated npm package",
		`"@upstash/context7-mcp"`,
		"pin the package selector",
	} {
		if !strings.Contains(warning, want) {
			t.Fatalf("warning = %q, want %q", warning, want)
		}
	}
	if strings.Contains(warning, "CONTEXT7_API_TOKEN") || strings.Contains(warning, "API_TOKEN") {
		t.Fatalf("warning = %q, want no env names or values", warning)
	}
}

func TestMCPServerAddBehaviorDoesNotWarnForPinnedOrPlainDelegate(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
	}{
		{
			name:    "pinned npx",
			command: "npx",
			args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
		},
		{
			name:    "plain command",
			command: "node",
			args:    []string{"scripts/mcp-server.js"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := ManifestDocument{
				Content: []byte("version = 1\ntargets = [\"claude-code\"]\n"),
			}
			change, err := BuildAddMCPServerChange(document, AddMCPServerRequest{
				Name:    "context7",
				Command: test.command,
				Args:    test.args,
			})
			if err != nil {
				t.Fatalf("BuildAddMCPServerChange returned error: %v", err)
			}
			if len(change.Warnings) != 0 {
				t.Fatalf("Warnings = %#v, want none", change.Warnings)
			}
		})
	}
}

func TestMCPServerAddBehaviorKeepsAbsolutePathSyntaxManifestOnly(t *testing.T) {
	_, err := MCPServerFromAddRequest(
		AddMCPServerRequest{
			Name:    "codegraph",
			Command: filepath.Join(t.TempDir(), "bin", "codegraph"),
			Args:    []string{"serve", "--mcp"},
			Targets: []string{"antigravity-cli"},
			Scope:   "global",
		},
		declaration.ManifestHeader{Targets: []string{"antigravity-cli"}},
		daempaths.ManifestOriginExplicit,
	)
	if err == nil || !strings.Contains(err.Error(), "portable command token") {
		t.Fatalf("MCPServerFromAddRequest error = %v, want portable command diagnostic", err)
	}
}

func TestMCPServerAddBehaviorRejectsDuplicateConflict(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)

	_, _, err := ApplyAddMCPServerToManifest(original, declarationcodec.MCPServer{
		Name:      "context7",
		Targets:   []string{"claude-code"},
		Scope:     "project",
		Transport: "stdio",
		Command:   declaration.NewMCPAmbientCommand("node"),
		Args:      []string{"server.js"},
		Env:       map[string]declarationcodec.MCPEnvReference{},
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err == nil || !strings.Contains(err.Error(), `duplicate mcp_server subject "claude-code.project.mcp-server.context7"`) {
		t.Fatalf("err = %v, want duplicate conflict", err)
	}

	_, _, err = ApplyAddMCPServerToManifest(original, declarationcodec.MCPServer{
		Name:      "context7",
		Targets:   []string{"claude-code"},
		Scope:     "project",
		Transport: "stdio",
		Command:   declaration.NewMCPAmbientCommand("npx"),
		Args:      []string{"-y", "@upstash/context7-mcp"},
		Env:       map[string]declarationcodec.MCPEnvReference{},
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err == nil || !strings.Contains(err.Error(), `already has the selected targets`) {
		t.Fatalf("err = %v, want already-present diagnostic", err)
	}
}

func TestMCPServerAddBehaviorRejectsOpenCodeDuplicateConflict(t *testing.T) {
	original := []byte(`version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`)

	_, _, err := ApplyAddMCPServerToManifest(original, declarationcodec.MCPServer{
		Name:      "context7",
		Targets:   []string{"opencode"},
		Scope:     "project",
		Transport: "stdio",
		Command:   declaration.NewMCPAmbientCommand("node"),
		Args:      []string{"server.js"},
	}, declaration.ManifestHeader{Targets: []string{"opencode"}})
	if err == nil || !strings.Contains(err.Error(), `duplicate mcp_server subject "opencode.project.mcp-server.context7"`) {
		t.Fatalf("err = %v, want OpenCode duplicate conflict", err)
	}

	_, _, err = ApplyAddMCPServerToManifest(original, declarationcodec.MCPServer{
		Name:      "context7",
		Targets:   []string{"opencode"},
		Scope:     "project",
		Transport: "stdio",
		Command:   declaration.NewMCPAmbientCommand("npx"),
		Args:      []string{"-y", "@upstash/context7-mcp@1.2.3"},
	}, declaration.ManifestHeader{Targets: []string{"opencode"}})
	if err == nil || !strings.Contains(err.Error(), `already has the selected targets`) {
		t.Fatalf("err = %v, want OpenCode already-present diagnostic", err)
	}
}

func TestMCPServerAddBehaviorAllowsSameNameAcrossSubjects(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)

	updated, changeKind, err := ApplyAddMCPServerToManifest(original, declarationcodec.MCPServer{
		Name:      "context7",
		Targets:   []string{"opencode"},
		Scope:     "project",
		Transport: "stdio",
		Command:   declaration.NewMCPAmbientCommand("npx"),
		Args:      []string{"-y", "@upstash/context7-mcp"},
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err != nil {
		t.Fatalf("ApplyAddMCPServerToManifest returned error: %v", err)
	}
	if changeKind != "append mcp_server resource" {
		t.Fatalf("changeKind = %q, want append mcp_server resource", changeKind)
	}
	if strings.Count(string(updated), "[[mcp_server]]") != 2 {
		t.Fatalf("updated = %q, want two mcp_server blocks", updated)
	}
	if !strings.Contains(string(updated), `targets = ["opencode"]`) {
		t.Fatalf("updated = %q, want opencode subject appended", updated)
	}
}

func TestMCPServerAddBehaviorRejectsInheritedTargets(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)

	_, _, err := ApplyAddMCPServerToManifest(original, declarationcodec.MCPServer{
		Name:      "context7",
		Targets:   []string{"claude-code"},
		Scope:     "project",
		Transport: "stdio",
		Command:   declaration.NewMCPAmbientCommand("npx"),
		Args:      []string{"-y", "@upstash/context7-mcp"},
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err == nil || !strings.Contains(err.Error(), `inherits manifest targets`) {
		t.Fatalf("err = %v, want inherited target diagnostic", err)
	}
}
