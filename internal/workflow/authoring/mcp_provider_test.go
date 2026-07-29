package authoring

import (
	"strings"
	"testing"
)

func TestPiMCPAuthoringAddsExplicitScopedProviderAndBinding(t *testing.T) {
	for _, test := range []struct {
		name            string
		scope           string
		wantExtensionID string
		wantWarning     string
	}{
		{
			name:            "project",
			scope:           "project",
			wantExtensionID: `id = "pi-mcp-adapter-project"`,
			wantWarning:     "may not activate project package/config changes until the project is trusted",
		},
		{
			name:            "global",
			scope:           "global",
			wantExtensionID: `id = "pi-mcp-adapter-global"`,
			wantWarning:     "shared across projects and may read project MCP config before project trust",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			change, err := BuildAddMCPServerChange(
				ManifestDocument{Content: []byte("version = 1\ntargets = [\"pi\"]\n")},
				AddMCPServerRequest{
					Name:    "context7",
					Command: "npx",
					Args:    []string{"-y", "@upstash/context7-mcp@1.2.3"},
					Targets: []string{"pi"},
					Scope:   test.scope,
					Env: []MCPServerEnvAssignment{
						{Name: "API_TOKEN", FromEnv: "CONTEXT7_API_TOKEN"},
					},
				},
			)
			if err != nil {
				t.Fatalf("BuildAddMCPServerChange returned error: %v", err)
			}
			if change.ChangeKind != "append extension and mcp_server resources" {
				t.Fatalf("ChangeKind = %q", change.ChangeKind)
			}
			for _, want := range []string{
				"[[extension]]",
				test.wantExtensionID,
				`carrier = "pi-package"`,
				`targets = ["pi"]`,
				`scope = "` + test.scope + `"`,
				`source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }`,
				"[[mcp_server]]",
				`name = "context7"`,
			} {
				if !strings.Contains(string(change.Content), want) {
					t.Fatalf("Content = %s, want %q", change.Content, want)
				}
				if !strings.Contains(change.ManifestBlock, want) {
					t.Fatalf("ManifestBlock = %s, want %q", change.ManifestBlock, want)
				}
			}
			if len(change.Warnings) != 1 ||
				!strings.Contains(change.Warnings[0], test.wantWarning) {
				t.Fatalf("Warnings = %#v, want %q", change.Warnings, test.wantWarning)
			}
		})
	}
}

func TestPiMCPAuthoringUsesProjectFirstAndExplicitGlobalFallback(t *testing.T) {
	global := piProviderExtensionBlock("global-provider", "global", "npm:pi-mcp-adapter@2.15.0")
	project := piProviderExtensionBlock("project-provider", "project", "npm:pi-mcp-adapter@^2.13.0")

	projectPreferred, err := BuildAddMCPServerChange(
		ManifestDocument{Content: []byte("version = 1\ntargets = [\"pi\"]\n\n" + global + "\n" + project)},
		AddMCPServerRequest{
			Name: "context7", Command: "node", Args: []string{"server.js"},
			Targets: []string{"pi"}, Scope: "project",
		},
	)
	if err != nil {
		t.Fatalf("project-preferred BuildAddMCPServerChange returned error: %v", err)
	}
	if strings.Count(string(projectPreferred.Content), "[[extension]]") != 2 {
		t.Fatalf("Content = %s, want no generated provider", projectPreferred.Content)
	}
	if len(projectPreferred.Warnings) != 1 ||
		!strings.Contains(projectPreferred.Warnings[0], "project pi-mcp-adapter package") {
		t.Fatalf("Warnings = %#v, want project-provider warning", projectPreferred.Warnings)
	}

	globalFallback, err := BuildAddMCPServerChange(
		ManifestDocument{Content: []byte("version = 1\ntargets = [\"pi\"]\n\n" + global)},
		AddMCPServerRequest{
			Name: "context7", Command: "node", Args: []string{"server.js"},
			Targets: []string{"pi"}, Scope: "project",
		},
	)
	if err != nil {
		t.Fatalf("global-fallback BuildAddMCPServerChange returned error: %v", err)
	}
	if strings.Count(string(globalFallback.Content), "[[extension]]") != 1 {
		t.Fatalf("Content = %s, want existing global provider only", globalFallback.Content)
	}
	if len(globalFallback.Warnings) != 1 ||
		!strings.Contains(globalFallback.Warnings[0], "global pi-mcp-adapter package") {
		t.Fatalf("Warnings = %#v, want global-reuse warning", globalFallback.Warnings)
	}
}

func TestPiMCPAuthoringCreatesMissingGlobalAlongsideProjectProvider(t *testing.T) {
	project := piProviderExtensionBlock(
		"pi-mcp-adapter-project",
		"project",
		"npm:pi-mcp-adapter@^2.13.0",
	)
	change, err := BuildAddMCPServerChange(
		ManifestDocument{Content: []byte("version = 1\ntargets = [\"pi\"]\n\n" + project)},
		AddMCPServerRequest{
			Name: "context7", Command: "node", Args: []string{"server.js"},
			Targets: []string{"pi"}, Scope: "global",
		},
	)
	if err != nil {
		t.Fatalf("BuildAddMCPServerChange returned error: %v", err)
	}
	if strings.Count(string(change.Content), "[[extension]]") != 2 ||
		!strings.Contains(string(change.Content), `id = "pi-mcp-adapter-global"`) {
		t.Fatalf("Content = %s, want distinct global provider", change.Content)
	}
}

func TestPiMCPAuthoringRejectsAmbiguousWinningProviders(t *testing.T) {
	first := piProviderExtensionBlock("provider-a", "project", "npm:pi-mcp-adapter@^2.13.0")
	second := piProviderExtensionBlock("provider-b", "project", "npm:pi-mcp-adapter@2.15.0")
	_, err := BuildAddMCPServerChange(
		ManifestDocument{Content: []byte("version = 1\ntargets = [\"pi\"]\n\n" + first + "\n" + second)},
		AddMCPServerRequest{
			Name: "context7", Command: "node", Args: []string{"server.js"},
			Targets: []string{"pi"}, Scope: "project",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "ambiguous provider contributions") {
		t.Fatalf("error = %v, want ambiguous provider diagnostic", err)
	}
}

func TestPiMCPAuthoringIgnoresUnrelatedPiPackage(t *testing.T) {
	unrelated := piProviderExtensionBlock(
		"pi-theme",
		"project",
		"npm:pi-theme@1.0.0",
	)
	change, err := BuildAddMCPServerChange(
		ManifestDocument{Content: []byte("version = 1\ntargets = [\"pi\"]\n\n" + unrelated)},
		AddMCPServerRequest{
			Name: "context7", Command: "node", Args: []string{"server.js"},
			Targets: []string{"pi"}, Scope: "project",
		},
	)
	if err != nil {
		t.Fatalf("BuildAddMCPServerChange returned error: %v", err)
	}
	if strings.Count(string(change.Content), "[[extension]]") != 2 ||
		!strings.Contains(string(change.Content), `id = "pi-theme"`) ||
		!strings.Contains(string(change.Content), `id = "pi-mcp-adapter-project"`) {
		t.Fatalf("Content = %s, want unrelated package plus generated provider", change.Content)
	}
}

func TestPiMCPAuthoringRejectsInvalidExplicitProviderSources(t *testing.T) {
	for _, source := range []string{
		"npm:pi-mcp-adapter",
		"npm:pi-mcp-adapter@latest",
		"npm:pi-mcp-adapter@2.13.0-beta.1",
		"npm:pi-mcp-adapter@^3.0.0",
		"github:acme/pi-mcp-adapter",
	} {
		t.Run(source, func(t *testing.T) {
			provider := piProviderExtensionBlock("provider", "project", source)
			_, err := BuildAddMCPServerChange(
				ManifestDocument{Content: []byte("version = 1\ntargets = [\"pi\"]\n\n" + provider)},
				AddMCPServerRequest{
					Name: "context7", Command: "node", Args: []string{"server.js"},
					Targets: []string{"pi"}, Scope: "project",
				},
			)
			if err == nil || !strings.Contains(err.Error(), "Pi MCP provider") {
				t.Fatalf("error = %v, want exact provider-source rejection", err)
			}
		})
	}
}

func TestPiMCPAuthoringRejectsDuplicateBindingWithoutAddingProvider(t *testing.T) {
	original := []byte("version = 1\ntargets = [\"pi\"]\n\n" +
		piProviderExtensionBlock(
			"pi-mcp-adapter-project",
			"project",
			"npm:pi-mcp-adapter@^2.13.0",
		) + `
[[mcp_server]]
name = "context7"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`)
	_, err := BuildAddMCPServerChange(
		ManifestDocument{Content: original},
		AddMCPServerRequest{
			Name: "context7", Command: "node", Args: []string{"server.js"},
			Targets: []string{"pi"}, Scope: "project",
		},
	)
	if err == nil ||
		!strings.Contains(err.Error(), `mcp_server "context7" already has the selected targets`) {
		t.Fatalf("error = %v, want duplicate Pi binding rejection", err)
	}
}

func TestPiMCPRemovalLeavesProviderDeclarationExplicit(t *testing.T) {
	original := []byte("version = 1\ntargets = [\"pi\"]\n\n" +
		piProviderExtensionBlock(
			"pi-mcp-adapter-project",
			"project",
			"npm:pi-mcp-adapter@^2.13.0",
		) + `
[[mcp_server]]
name = "context7"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["server.js"]
`)
	content, _, err := ApplyRemoveMCPServerToManifest(
		original,
		RemoveMCPServerRequest{
			Name: "context7", Targets: []string{"pi"}, Scope: "project",
		},
	)
	if err != nil {
		t.Fatalf("ApplyRemoveMCPServerToManifest returned error: %v", err)
	}
	if strings.Contains(string(content), "[[mcp_server]]") ||
		!strings.Contains(string(content), "[[extension]]") ||
		!strings.Contains(string(content), `id = "pi-mcp-adapter-project"`) {
		t.Fatalf("Content = %s, want only provider declaration retained", content)
	}
}

func piProviderExtensionBlock(id string, scope string, source string) string {
	return `[[extension]]
id = "` + id + `"
carrier = "pi-package"
targets = ["pi"]
scope = "` + scope + `"
source = { host_source = "` + source + `" }
`
}
