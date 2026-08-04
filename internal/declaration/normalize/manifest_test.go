package normalize

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/target"
)

func TestManifestNormalizesSkillsWithoutSourceListing(t *testing.T) {
	raw := baseManifest(target.TargetClaudeCode)
	raw.Skills = []declaration.Skill{{
		ID: "direct-id", Name: "direct", Source: localSource("skills/direct"), CompatRepair: true,
	}}
	raw.SkillGroups = []declaration.SkillGroup{
		{Names: []string{"named"}, Source: gitSource("skills")},
		{Include: []string{"glob:team-*"}, Exclude: []string{"regex:.*-old$"}, Source: localSource("skills")},
	}

	environment, err := Manifest(raw)
	if err != nil {
		t.Fatalf("Manifest returned error: %v", err)
	}
	if got := environment.Skills(); len(got) != 2 || got[0].ID().Name() != "direct-id" || got[0].InstallName() != "direct" || got[1].ID().Name() != "named" {
		t.Fatalf("skills = %#v, want direct and named declarations", got)
	} else if got[0].Scope() != target.ScopeProject || got[0].InstallMode() != desiredskill.InstallModeCopy {
		t.Fatalf("direct skill defaults = %s/%s, want project/copy", got[0].Scope(), got[0].InstallMode())
	}
	if got := environment.SkillSets(); len(got) != 1 || got[0].Include()[0].Expression() != "glob:team-*" || got[0].Exclude()[0].Expression() != "regex:.*-old$" {
		t.Fatalf("skill sets = %#v, want one unresolved selector-backed set", got)
	}
}

func TestManifestBuildsHookAndInstructionAggregates(t *testing.T) {
	raw := baseManifest(target.TargetClaudeCode)
	raw.HookAssets = map[string]declaration.HookAsset{
		"runner": {
			Source: declaration.HookAssetSource{Set: true, Source: localSource("hooks/runner.sh")},
			Kind:   "file", Executable: true,
		},
	}
	raw.Hooks = []declaration.Hook{{
		Name: "preflight", Event: "BeforeTool", Command: "{hook_file:runner} --check",
		TargetOverrides: []declaration.HookTargetOverride{{Target: "claude-code", Condition: "always", Matcher: "Bash"}},
	}}
	raw.Instructions = map[string]declaration.Instructions{
		"zeta":  instruction("ZETA.md", "symlink"),
		"alpha": instruction("AGENTS.md", "copy"),
	}

	environment, err := Manifest(raw)
	if err != nil {
		t.Fatalf("Manifest returned error: %v", err)
	}
	if got := environment.Hooks(); len(got) != 1 || got[0].TargetOverrides()[target.TargetClaudeCode].Matcher() != "Bash" {
		t.Fatalf("hooks = %#v, want canonical hook with target override", got)
	}
	if got := environment.HookAssets(); len(got) != 1 || got[0].ID().Name() != "runner" || !got[0].Executable() {
		t.Fatalf("hook assets = %#v, want runner", got)
	}
	gotInstructions := environment.Instructions()
	if len(gotInstructions) != 2 || gotInstructions[0].ID().Name() != "alpha" || gotInstructions[1].ID().Name() != "zeta" {
		t.Fatalf("instructions order = %#v, want deterministic alpha,zeta", gotInstructions)
	}
	alphaRendering := gotInstructions[0].Renderings()[target.TargetClaudeCode]
	if alphaRendering.RenderTo() != "AGENTS.md" || alphaRendering.Mode() != "copy" {
		t.Fatalf("alpha rendering = %#v, want AGENTS.md/copy", alphaRendering)
	}
}

func TestManifestAggregatesMCPBindingsByServerIdentity(t *testing.T) {
	raw := baseManifest(target.TargetClaudeCode, target.TargetCodex)
	raw.MCPServers = []declaration.MCPServer{
		{Name: "repo-tools", Targets: []string{"codex"}, Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx"), Args: []string{"codex-package"}},
		{Name: "repo-tools", Targets: []string{"claude-code"}, Transport: "stdio", Command: declaration.NewMCPAmbientCommand("uvx"), Args: []string{"claude-package"}},
	}

	environment, err := Manifest(raw)
	if err != nil {
		t.Fatalf("Manifest returned error: %v", err)
	}
	servers := environment.MCPServers()
	if len(servers) != 1 || servers[0].ID().Name() != "repo-tools" {
		t.Fatalf("servers = %#v, want one aggregate", servers)
	}
	bindings := servers[0].Bindings()
	if len(bindings) != 2 || bindings[0].Target() != target.TargetCodex || bindings[1].Target() != target.TargetClaudeCode {
		t.Fatalf("bindings = %#v, want authored codex then claude-code order", bindings)
	}
	firstStdio, ok := bindings[0].Transport().Stdio()
	if !ok || firstStdio.Command().Executable() != "npx" || firstStdio.Args()[0] != "codex-package" {
		t.Fatalf("first transport = %#v, want ambient stdio command", bindings[0].Transport())
	}
}

func TestManifestRejectsNonStdioMCPTransport(t *testing.T) {
	for _, transport := range []string{"http", "HTTP", "sse"} {
		t.Run(transport, func(t *testing.T) {
			raw := baseManifest(target.TargetCodex)
			raw.MCPServers = []declaration.MCPServer{{
				Name: "remote", Transport: transport, Command: declaration.NewMCPAmbientCommand("npx"),
			}}

			_, err := Manifest(raw)
			if err == nil || !strings.Contains(err.Error(), `unsupported MCP transport "`+transport+`"`) {
				t.Fatalf("Manifest transport %q error = %v", transport, err)
			}
		})
	}
}

func TestManifestMCPNormalizationDoesNotOwnPlacementAdmission(t *testing.T) {
	raw := baseManifest(target.TargetPi)
	raw.MCPServers = []declaration.MCPServer{{
		Name: "portable-intent", Targets: []string{string(target.TargetPi)},
		Scope: "project", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx"),
		Env: map[string]declaration.MCPEnvReference{
			"TOKEN": {FromEnv: "UPSTREAM_TOKEN"},
		},
	}}

	environment, err := Manifest(raw)
	if err != nil {
		t.Fatalf("Manifest rejected canonical MCP intent before Realization admission: %v", err)
	}
	binding := environment.MCPServers()[0].Bindings()[0]
	stdio, ok := binding.Transport().Stdio()
	if !ok || binding.Target() != target.TargetPi || binding.Scope() != target.ScopeProject ||
		stdio.Env()["TOKEN"].FromEnv() != "UPSTREAM_TOKEN" {
		t.Fatalf("normalized binding = %#v, want preserved canonical intent", binding)
	}
}

func TestManifestMCPNormalizationRequiresExplicitGlobalScope(t *testing.T) {
	raw := baseManifest(target.TargetCodex)
	raw.Defaults.Scope = string(target.ScopeGlobal)
	raw.MCPServers = []declaration.MCPServer{{
		Name: "implicit-global", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx"),
	}}

	_, err := Manifest(raw)
	if err == nil || !strings.Contains(err.Error(), "requires explicit scope") {
		t.Fatalf("Manifest error = %v, want explicit-scope error", err)
	}
}

func TestManifestNormalizesExtensionRelation(t *testing.T) {
	raw := baseManifest(target.TargetCodex)
	raw.Extensions = []declaration.Extension{{
		ID: "documents", Carrier: "codex-plugin", Targets: []string{"codex"}, Scope: "global",
		Source: declaration.ExtensionSource{Marketplace: "documents@openai-primary-runtime"},
	}}

	environment, err := Manifest(raw)
	if err != nil {
		t.Fatalf("Manifest returned error: %v", err)
	}
	extensions := environment.Extensions()
	if len(extensions) != 1 || extensions[0].ID().Name() != "documents" || extensions[0].Source().Ref() != "documents@openai-primary-runtime" {
		t.Fatalf("extensions = %#v, want canonical Codex relation", extensions)
	}
}

func TestManifestRejectsMalformedCanonicalDeclarations(t *testing.T) {
	tests := []struct {
		name string
		raw  declaration.Manifest
		want string
	}{
		{
			name: "mixed skill group modes",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.SkillGroups = []declaration.SkillGroup{{Names: []string{"one"}, Include: []string{"glob:*"}, Source: localSource("skills")}}
				return raw
			}(),
			want: "either names or include",
		},
		{
			name: "duplicate hook override",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.Hooks = []declaration.Hook{{Name: "h", Event: "event", Command: "true", TargetOverrides: []declaration.HookTargetOverride{{Target: "claude-code"}, {Target: "claude-code"}}}}
				return raw
			}(),
			want: "duplicate override",
		},
		{
			name: "duplicate mcp binding",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				server := declaration.MCPServer{Name: "same", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx")}
				raw.MCPServers = []declaration.MCPServer{server, server}
				return raw
			}(),
			want: "duplicate binding",
		},
		{
			name: "inherited global extension authority",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Defaults.Scope = "global"
				raw.Extensions = []declaration.Extension{{ID: "docs", Carrier: "codex-plugin", Source: declaration.ExtensionSource{Marketplace: "docs@market"}}}
				return raw
			}(),
			want: "requires explicit scope",
		},
		{
			name: "missing hook asset reference",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.Hooks = []declaration.Hook{{Name: "h", Event: "event", Command: "{hook_file:missing}"}}
				return raw
			}(),
			want: "is not declared",
		},
		{
			name: "negative hook timeout",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.Hooks = []declaration.Hook{{Name: "h", Event: "event", Command: "true", TimeoutSeconds: -1}}
				return raw
			}(),
			want: "must not be negative",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Manifest(test.raw)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Manifest error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestDeclarationDecodeRejectsUnknownKeysBeforeNormalization(t *testing.T) {
	_, err := declaration.DecodeManifest([]byte("version = 1\ntargets = [\"codex\"]\nunknown = true\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown manifest key") {
		t.Fatalf("Decode error = %v, want unknown key rejection", err)
	}
}

func baseManifest(targets ...target.Target) declaration.Manifest {
	values := make([]string, 0, len(targets))
	for _, selected := range targets {
		values = append(values, string(selected))
	}
	return declaration.Manifest{Version: declaration.CurrentManifestVersion, Targets: values}
}

func localSource(path string) declaration.Source {
	return declaration.Source{Path: path, Mode: "vendor"}
}

func gitSource(path string) declaration.Source {
	return declaration.Source{Git: "https://example.test/skills.git", Path: path, Ref: "main"}
}

func instruction(renderTo string, mode string) declaration.Instructions {
	return declaration.Instructions{
		Source: declaration.InstructionSource{Set: true, Source: localSource(renderTo)},
		Target: map[string]declaration.InstructionTarget{
			"claude-code": {RenderTo: renderTo, Mode: mode},
		},
	}
}
