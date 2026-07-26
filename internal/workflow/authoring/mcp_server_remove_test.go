package authoring

import (
	"strings"
	"testing"
)

func TestMCPServerRemoveBehaviorRemovesWholeDeclarationBlock(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

[[hook]]
name = "protect-env"
event = "PreToolUse"
command = "scripts/protect-env.sh"
targets = ["claude-code"]
`)

	updated, changeKind, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if changeKind != "remove mcp_server resource" {
		t.Fatalf("changeKind = %q, want remove mcp_server resource", changeKind)
	}
	if strings.Contains(string(updated), "[[mcp_server]]") || strings.Contains(string(updated), "context7") {
		t.Fatalf("updated = %q, want mcp_server block removed", updated)
	}
	if !strings.Contains(string(updated), "[[hook]]") || !strings.Contains(string(updated), "protect-env") {
		t.Fatalf("updated = %q, want sibling hook block preserved", updated)
	}
}

func TestMCPServerRemoveBehaviorRemovesNestedEnvAndPreservesFollowingResource(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]] # user-authored comment
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"

[mcp_server.env.API_TOKEN]
from_env = "CONTEXT7_API_TOKEN"

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]
`)

	updated, changeKind, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if changeKind != "remove mcp_server resource" {
		t.Fatalf("changeKind = %q, want remove mcp_server resource", changeKind)
	}
	if strings.Contains(string(updated), "[[mcp_server]]") || strings.Contains(string(updated), "[mcp_server.env.API_TOKEN]") {
		t.Fatalf("updated = %q, want mcp_server block and nested env removed", updated)
	}
	if !strings.Contains(string(updated), "[[skill]]") || !strings.Contains(string(updated), "oracle") {
		t.Fatalf("updated = %q, want following skill block preserved", updated)
	}
}

func TestMCPServerRemoveBehaviorHandlesQuotedRootAndNoTrailingNewline(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[["mcp_server"]] # user-authored comment
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"

[mcp_server.env.API_TOKEN]
from_env = "CONTEXT7_API_TOKEN"

[[hook]]
name = "protect-env"
event = "PreToolUse"
command = "scripts/protect-env.sh"
targets = ["claude-code"]`)

	updated, changeKind, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if changeKind != "remove mcp_server resource" {
		t.Fatalf("changeKind = %q, want remove mcp_server resource", changeKind)
	}
	if strings.Contains(string(updated), "[[\"mcp_server\"]]") || strings.Contains(string(updated), "[mcp_server.env.API_TOKEN]") {
		t.Fatalf("updated = %q, want quoted mcp_server block and nested env removed", updated)
	}
	if !strings.Contains(string(updated), "[[hook]]") || !strings.Contains(string(updated), "protect-env") {
		t.Fatalf("updated = %q, want following hook block preserved", updated)
	}
}

func TestMCPServerRemoveBehaviorReportsNotFoundAndAmbiguous(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		original := []byte("version = 1\ntargets = [\"claude-code\"]\n")
		_, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "missing"})
		if err == nil || !strings.Contains(err.Error(), `mcp_server resource "missing" not found`) {
			t.Fatalf("err = %v, want not-found diagnostic", err)
		}
	})

	t.Run("ambiguous duplicate blocks", func(t *testing.T) {
		original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "node"
`)
		_, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
		if err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("err = %v, want ambiguity diagnostic", err)
		}
	})

	t.Run("ambiguous quoted root duplicate blocks", func(t *testing.T) {
		original := []byte(`version = 1
targets = ["claude-code"]

[["mcp_server"]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "node"
`)
		_, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
		if err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("err = %v, want ambiguity diagnostic", err)
		}
	})
}

func TestMCPServerRemoveBehaviorRequiresSelectorForSameNameAcrossSubjects(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "project"
transport = "stdio"
command = "npx"
`)

	_, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err = %v, want ambiguity diagnostic", err)
	}

	updated, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{
		Name:    "context7",
		Targets: []string{"opencode"},
		Scope:   "project",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if strings.Contains(string(updated), `targets = ["opencode"]`) {
		t.Fatalf("updated = %q, want opencode block removed", updated)
	}
	if !strings.Contains(string(updated), `targets = ["claude-code"]`) {
		t.Fatalf("updated = %q, want claude block preserved", updated)
	}
}

func TestMCPServerRemoveBehaviorRejectsNameOnlyAcrossNonDefaultSubjects(t *testing.T) {
	original := []byte(`version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "project"
transport = "stdio"
command = "npx"

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "npx"
`)

	_, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err = %v, want ambiguity diagnostic", err)
	}

	updated, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{
		Name:    "context7",
		Targets: []string{"codex"},
		Scope:   "project",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if strings.Contains(string(updated), `targets = ["codex"]`) {
		t.Fatalf("updated = %q, want codex block removed", updated)
	}
	if !strings.Contains(string(updated), `targets = ["opencode"]`) {
		t.Fatalf("updated = %q, want opencode block preserved", updated)
	}
}

func TestMCPServerRemoveBehaviorUsesExactFirstSliceSelectors(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
`)

	updated, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{
		Name:    "context7",
		Targets: []string{"claude-code"},
		Scope:   "project",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if strings.Contains(string(updated), "[[mcp_server]]") {
		t.Fatalf("updated = %q, want whole block removed", updated)
	}

	for _, test := range []struct {
		name    string
		request RemoveMCPServerRequest
		want    string
	}{
		{
			name:    "unsupported target",
			request: RemoveMCPServerRequest{Name: "context7", Targets: []string{"pi"}},
			want:    "supports only --target claude-code, --target antigravity-cli, --target opencode, or --target codex",
		},
		{
			name:    "invalid scope",
			request: RemoveMCPServerRequest{Name: "context7", Scope: "user"},
			want:    "unknown scope",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := ApplyRemoveMCPServerToManifest(original, test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMCPServerRemoveBehaviorMatchesInheritedFirstSliceFacts(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
`)

	updated, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{
		Name:    "context7",
		Targets: []string{"claude-code"},
		Scope:   "project",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if strings.Contains(string(updated), "[[mcp_server]]") {
		t.Fatalf("updated = %q, want inherited first-slice block removed", updated)
	}
}

func TestMCPServerRemoveBehaviorInfersUniqueAntigravityRow(t *testing.T) {
	original := []byte(`version = 1
targets = ["antigravity-cli"]

[[mcp_server]]
name = "context7"
targets = ["antigravity-cli"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`)

	implicit, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
	if err != nil || strings.Contains(string(implicit), "[[mcp_server]]") {
		t.Fatalf("implicit removal = %q, err = %v, want unique Antigravity row removed", implicit, err)
	}

	updated, changeKind, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{
		Name:    "context7",
		Targets: []string{"antigravity-cli"},
		Scope:   "global",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if changeKind != "remove mcp_server resource" {
		t.Fatalf("changeKind = %q, want remove mcp_server resource", changeKind)
	}
	if strings.Contains(string(updated), "[[mcp_server]]") {
		t.Fatalf("updated = %q, want Antigravity block removed", updated)
	}
}

func TestMCPServerRemoveBehaviorInfersUniqueOpenCodeRow(t *testing.T) {
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

	implicit, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
	if err != nil || strings.Contains(string(implicit), "[[mcp_server]]") {
		t.Fatalf("implicit removal = %q, err = %v, want unique OpenCode row removed", implicit, err)
	}

	updated, changeKind, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{
		Name:    "context7",
		Targets: []string{"opencode"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if changeKind != "remove mcp_server resource" {
		t.Fatalf("changeKind = %q, want remove mcp_server resource", changeKind)
	}
	if strings.Contains(string(updated), "[[mcp_server]]") {
		t.Fatalf("updated = %q, want OpenCode block removed", updated)
	}
}

func TestMCPServerRemoveBehaviorInfersGlobalScopeFromUniqueOpenCodeRow(t *testing.T) {
	original := []byte(`version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`)

	implicit, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7", Targets: []string{"opencode"}})
	if err != nil || strings.Contains(string(implicit), "[[mcp_server]]") {
		t.Fatalf("target-only removal = %q, err = %v, want unique global OpenCode row removed", implicit, err)
	}

	updated, changeKind, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{
		Name:    "context7",
		Targets: []string{"opencode"},
		Scope:   "global",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if changeKind != "remove mcp_server resource" {
		t.Fatalf("changeKind = %q, want remove mcp_server resource", changeKind)
	}
	if strings.Contains(string(updated), "[[mcp_server]]") {
		t.Fatalf("updated = %q, want OpenCode global block removed", updated)
	}
}

func TestMCPServerRemoveBehaviorInfersUniqueCodexRow(t *testing.T) {
	original := []byte(`version = 1
targets = ["codex"]

[[mcp_server]]
name = "context7"
targets = ["codex"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`)

	implicit, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
	if err != nil || strings.Contains(string(implicit), "[[mcp_server]]") {
		t.Fatalf("implicit removal = %q, err = %v, want unique Codex row removed", implicit, err)
	}

	updated, changeKind, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{
		Name:    "context7",
		Targets: []string{"codex"},
	})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if changeKind != "remove mcp_server resource" {
		t.Fatalf("changeKind = %q, want remove mcp_server resource", changeKind)
	}
	if strings.Contains(string(updated), "[[mcp_server]]") {
		t.Fatalf("updated = %q, want Codex block removed", updated)
	}
}

func TestMCPServerRemoveBehaviorInfersInheritedOpenCodeFacts(t *testing.T) {
	original := []byte(`version = 1
targets = ["opencode"]

[[mcp_server]]
name = "context7"
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
`)

	implicit, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{Name: "context7"})
	if err != nil || strings.Contains(string(implicit), "[[mcp_server]]") {
		t.Fatalf("implicit removal = %q, err = %v, want inherited OpenCode row removed", implicit, err)
	}

	updated, _, err := ApplyRemoveMCPServerToManifest(original, RemoveMCPServerRequest{
		Name:    "context7",
		Targets: []string{"opencode"},
		Scope:   "project",
	})
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if strings.Contains(string(updated), "[[mcp_server]]") {
		t.Fatalf("updated = %q, want inherited OpenCode block removed", updated)
	}
}
