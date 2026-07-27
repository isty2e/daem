package normalize

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/target"
)

func TestExplicitMCPServerNormalizesOneResolvedDeclarationRow(t *testing.T) {
	server, binding, err := ExplicitMCPServer(declarationcodec.MCPServer{
		Name: "context7", Targets: []string{"codex"}, Scope: "project", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx"),
		Args: []string{"-y", "@upstash/context7-mcp"}, Env: map[string]declarationcodec.MCPEnvReference{"TOKEN": {FromEnv: "TOKEN"}},
	})
	if err != nil {
		t.Fatalf("ExplicitMCPServer: %v", err)
	}
	if server.ID().Name() != "context7" || binding.Target() != target.TargetCodex || binding.Scope() != target.ScopeProject {
		t.Fatalf("server = %#v, binding = %#v", server, binding)
	}
}

func TestNormalizeTargetsPreservesAuthoredOrder(t *testing.T) {
	targets, err := normalizeTargets([]string{"pi", "codex"}, "targets")
	if err != nil {
		t.Fatalf("normalizeTargets returned error: %v", err)
	}
	if !slices.Equal(targets, []target.Target{target.TargetPi, target.TargetCodex}) {
		t.Fatalf("targets = %#v, want authored order", targets)
	}
}

func TestExplicitMCPServerRejectsAnUnadmittedTransport(t *testing.T) {
	for _, transport := range []string{"http", "HTTP", "sse", "", " stdio", "stdio "} {
		t.Run(transport, func(t *testing.T) {
			_, _, err := ExplicitMCPServer(declarationcodec.MCPServer{
				Name: "context7", Targets: []string{"codex"}, Scope: "project", Transport: transport, Command: declaration.NewMCPAmbientCommand("npx"),
			})
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unsupported MCP transport %q", transport)) {
				t.Fatalf("ExplicitMCPServer transport %q error = %v", transport, err)
			}
		})
	}
}

func TestManifestEdgeRoundOneRejectsIdentityAndRelationCollisions(t *testing.T) {
	tests := []struct {
		name string
		raw  declaration.Manifest
		want string
	}{
		{
			name: "direct and named group identity",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Skills = []declaration.Skill{{ID: "same", Name: "first", Source: localSource("skills/first")}}
				raw.SkillGroups = []declaration.SkillGroup{{Names: []string{"same"}, Source: localSource("skills")}}
				return raw
			}(),
			want: `duplicate skill id "same"`,
		},
		{
			name: "distinct identities same destination",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Skills = []declaration.Skill{
					{ID: "first", Name: "installed", Source: localSource("skills/first")},
					{ID: "second", Name: "installed", Source: localSource("skills/second")},
				}
				return raw
			}(),
			want: "duplicate skill destination",
		},
		{
			name: "same mcp binding",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				server := declaration.MCPServer{Name: "same", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx")}
				raw.MCPServers = []declaration.MCPServer{server, server}
				return raw
			}(),
			want: "duplicate binding",
		},
		{
			name: "same extension relation different ids",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.Extensions = []declaration.Extension{
					{ID: "first", Carrier: "claude-code-plugin", Source: declaration.ExtensionSource{Marketplace: "same@market"}},
					{ID: "second", Carrier: "claude-code-plugin", Source: declaration.ExtensionSource{Marketplace: "same@market"}},
				}
				return raw
			}(),
			want: "duplicate extension relation",
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

func TestManifestEdgeRoundOneRejectsInvalidUTF8AtOpaqueBoundaries(t *testing.T) {
	invalid := string([]byte{'b', 'a', 'd', 0xff})
	tests := []struct {
		name string
		raw  declaration.Manifest
	}{
		{
			name: "extension host source",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetOpenCode)
				raw.Extensions = []declaration.Extension{{ID: "bad", Carrier: "opencode-plugin", Source: declaration.ExtensionSource{HostSource: invalid}}}
				return raw
			}(),
		},
		{
			name: "mcp argument",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.MCPServers = []declaration.MCPServer{{Name: "bad", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx"), Args: []string{invalid}}}
				return raw
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Manifest(test.raw); err == nil {
				t.Fatal("Manifest accepted invalid UTF-8")
			}
		})
	}
}

func TestManifestEdgeRoundTwoPreservesOrthogonalAxes(t *testing.T) {
	tests := []struct {
		name  string
		raw   declaration.Manifest
		check func(*testing.T, int, int, int)
	}{
		{
			name: "same name across families",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Skills = []declaration.Skill{{Name: "shared", Source: localSource("skills/shared")}}
				raw.Hooks = []declaration.Hook{{Name: "shared", Event: "event", Command: "true"}}
				return raw
			}(),
			check: func(t *testing.T, skills int, hooks int, bindings int) {
				if skills != 1 || hooks != 1 {
					t.Fatalf("skills/hooks = %d/%d, want 1/1", skills, hooks)
				}
			},
		},
		{
			name: "family target outside top-level defaults",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Skills = []declaration.Skill{{Name: "claude-only", Targets: []string{"claude-code"}, Source: localSource("skills/claude")}}
				return raw
			}(),
			check: func(t *testing.T, skills int, hooks int, bindings int) {
				if skills != 1 {
					t.Fatalf("skills = %d, want explicit family target to remain independent", skills)
				}
			},
		},
		{
			name: "same mcp server target distinct scopes",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.MCPServers = []declaration.MCPServer{
					{Name: "same", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx")},
					{Name: "same", Scope: "global", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx")},
				}
				return raw
			}(),
			check: func(t *testing.T, skills int, hooks int, bindings int) {
				if bindings != 2 {
					t.Fatalf("bindings = %d, want project and global bindings", bindings)
				}
			},
		},
		{
			name: "unicode safe skill segment",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Skills = []declaration.Skill{{Name: "café", Source: localSource("skills/café")}}
				return raw
			}(),
			check: func(t *testing.T, skills int, hooks int, bindings int) {
				if skills != 1 {
					t.Fatalf("skills = %d, want valid Unicode segment", skills)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment, err := Manifest(test.raw)
			if err != nil {
				t.Fatalf("Manifest returned error: %v", err)
			}
			bindings := 0
			for _, server := range environment.MCPServers() {
				bindings += len(server.Bindings())
			}
			test.check(t, len(environment.Skills()), len(environment.Hooks()), bindings)
		})
	}
}

func TestManifestEdgeRoundTwoRejectsInvalidUTF8InRenderedHookText(t *testing.T) {
	invalid := string([]byte{'b', 'a', 'd', 0xff})
	tests := []struct {
		name string
		raw  declaration.Manifest
	}{
		{
			name: "hook event",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.Hooks = []declaration.Hook{{Name: "bad", Event: invalid, Command: "true"}}
				return raw
			}(),
		},
		{
			name: "instruction render path",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.Instructions = map[string]declaration.Instructions{
					"bad": {
						Source: declaration.InstructionSource{Set: true, Source: localSource("AGENTS.md")},
						Target: map[string]declaration.InstructionTarget{"claude-code": {RenderTo: invalid}},
					},
				}
				return raw
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Manifest(test.raw); err == nil {
				t.Fatal("Manifest accepted invalid UTF-8")
			}
		})
	}
}

func TestManifestEdgeRoundThreeProducesDeterministicFailures(t *testing.T) {
	tests := []struct {
		name string
		raw  declaration.Manifest
	}{
		{
			name: "hook override map",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Hooks = []declaration.Hook{{
					Name: "bad", Event: "event", Command: "true",
					TargetOverrides: []declaration.HookTargetOverride{{Target: "pi"}, {Target: "claude-code"}},
				}}
				return raw
			}(),
		},
		{
			name: "instruction rendering map",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Instructions = map[string]declaration.Instructions{
					"bad": {
						Source: declaration.InstructionSource{Set: true, Source: localSource("AGENTS.md")},
						Target: map[string]declaration.InstructionTarget{"pi": {}, "claude-code": {}},
					},
				}
				return raw
			}(),
		},
		{
			name: "mcp environment map",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.MCPServers = []declaration.MCPServer{{
					Name: "bad", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx"),
					Env: map[string]declaration.MCPEnvReference{
						"1BAD": {FromEnv: "HOST_ONE"},
						"-BAD": {FromEnv: "HOST_TWO"},
					},
				}}
				return raw
			}(),
		},
		{
			name: "instruction name map",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Instructions = map[string]declaration.Instructions{
					"a\x00bad": {Source: declaration.InstructionSource{Set: true, Source: localSource("A.md")}},
					"b\x00bad": {Source: declaration.InstructionSource{Set: true, Source: localSource("B.md")}},
				}
				return raw
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			errors := make(map[string]struct{})
			for range 200 {
				_, err := Manifest(test.raw)
				if err == nil {
					t.Fatal("Manifest returned nil error")
				}
				errors[err.Error()] = struct{}{}
			}
			if len(errors) != 1 {
				t.Fatalf("Manifest produced %d diagnostics for identical input: %#v", len(errors), errors)
			}
		})
	}
}

func TestManifestEdgeRoundThreeRejectsBidiControls(t *testing.T) {
	tests := []struct {
		name string
		raw  declaration.Manifest
	}{
		{
			name: "skill name",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Skills = []declaration.Skill{{Name: "safe\u202etxt", Source: localSource("skills/safe")}}
				return raw
			}(),
		},
		{
			name: "extension host source",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetOpenCode)
				raw.Extensions = []declaration.Extension{{ID: "bad", Carrier: "opencode-plugin", Source: declaration.ExtensionSource{HostSource: "safe\u202etxt"}}}
				return raw
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Manifest(test.raw); err == nil {
				t.Fatal("Manifest accepted a bidirectional control character")
			}
		})
	}
}

func TestManifestEdgeRoundFourRejectsCrossAxisAndPortabilityViolations(t *testing.T) {
	tests := []struct {
		name string
		raw  declaration.Manifest
		want string
	}{
		{
			name: "hook asset scope mismatch",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.HookAssets = map[string]declaration.HookAsset{
					"runner": {Source: declaration.HookAssetSource{Set: true, Source: localSource("/runner.sh")}, Kind: "file", Scope: "global"},
				}
				raw.Hooks = []declaration.Hook{{Name: "run", Event: "event", Command: "{hook_file:runner}"}}
				return raw
			}(),
			want: "does not match hook scope",
		},
		{
			name: "instruction rendering outside targets",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Instructions = map[string]declaration.Instructions{
					"bad": {
						Source: declaration.InstructionSource{Set: true, Source: localSource("AGENTS.md")},
						Target: map[string]declaration.InstructionTarget{"claude-code": {}},
					},
				}
				return raw
			}(),
			want: "not declared for instructions",
		},
		{
			name: "portable project link skill",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.Skills = []declaration.Skill{{Name: "linked", Source: declaration.Source{Path: "skills/linked", Mode: "link"}}}
				return raw
			}(),
			want: "portable = false",
		},
		{
			name: "global relative selector set",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetCodex)
				raw.SkillGroups = []declaration.SkillGroup{{Include: []string{"glob:*"}, Scope: "global", Source: localSource("skills")}}
				return raw
			}(),
			want: "absolute path",
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

func TestManifestEdgeRoundFourRejectsBidiControlsInOtherRenderedFields(t *testing.T) {
	tests := []struct {
		name string
		raw  declaration.Manifest
	}{
		{
			name: "hook name",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.Hooks = []declaration.Hook{{Name: "safe\u202etxt", Event: "event", Command: "true"}}
				return raw
			}(),
		},
		{
			name: "mcp argument",
			raw: func() declaration.Manifest {
				raw := baseManifest(target.TargetClaudeCode)
				raw.MCPServers = []declaration.MCPServer{{Name: "bad", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx"), Args: []string{"safe\u202etxt"}}}
				return raw
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Manifest(test.raw); err == nil {
				t.Fatal("Manifest accepted a bidirectional control character")
			}
		})
	}
}

func TestManifestEdgeRoundFiveAuthorityAndScopeAxes(t *testing.T) {
	t.Run("explicit global extension", func(t *testing.T) {
		raw := baseManifest(target.TargetClaudeCode)
		raw.Extensions = []declaration.Extension{{
			ID: "global", Carrier: "claude-code-plugin", Scope: "global",
			Source: declaration.ExtensionSource{Marketplace: "plugin@market"},
		}}
		environment, err := Manifest(raw)
		if err != nil || len(environment.Extensions()) != 1 {
			t.Fatalf("Manifest result = %#v, %v", environment.Extensions(), err)
		}
	})

	t.Run("inherited global mcp authority", func(t *testing.T) {
		raw := baseManifest(target.TargetCodex)
		raw.Defaults.Scope = "global"
		raw.MCPServers = []declaration.MCPServer{{Name: "bad", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx")}}
		if _, err := Manifest(raw); err == nil || !strings.Contains(err.Error(), "requires explicit scope") {
			t.Fatalf("Manifest error = %v, want explicit global rejection", err)
		}
	})

	t.Run("same skill destination distinct scopes", func(t *testing.T) {
		raw := baseManifest(target.TargetCodex)
		raw.Skills = []declaration.Skill{
			{ID: "project", Name: "same", Source: localSource("skills/project")},
			{ID: "global", Name: "same", Scope: "global", Source: localSource("/skills/global")},
		}
		environment, err := Manifest(raw)
		if err != nil || len(environment.Skills()) != 2 {
			t.Fatalf("Manifest skills = %#v, %v", environment.Skills(), err)
		}
	})

	t.Run("same extension source distinct scopes", func(t *testing.T) {
		raw := baseManifest(target.TargetClaudeCode)
		raw.Extensions = []declaration.Extension{
			{ID: "project", Carrier: "claude-code-plugin", Source: declaration.ExtensionSource{Marketplace: "same@market"}},
			{ID: "global", Carrier: "claude-code-plugin", Scope: "global", Source: declaration.ExtensionSource{Marketplace: "same@market"}},
		}
		environment, err := Manifest(raw)
		if err != nil || len(environment.Extensions()) != 2 {
			t.Fatalf("Manifest extensions = %#v, %v", environment.Extensions(), err)
		}
	})

	t.Run("mcp carriage return argument", func(t *testing.T) {
		raw := baseManifest(target.TargetClaudeCode)
		raw.MCPServers = []declaration.MCPServer{{Name: "bad", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx"), Args: []string{"one\rtwo"}}}
		if _, err := Manifest(raw); err == nil || !strings.Contains(err.Error(), "control") {
			t.Fatalf("Manifest error = %v, want control rejection", err)
		}
	})

	t.Run("emoji joiner skill name", func(t *testing.T) {
		raw := baseManifest(target.TargetCodex)
		raw.Skills = []declaration.Skill{{Name: "dev👩‍💻", Source: localSource("skills/dev")}}
		environment, err := Manifest(raw)
		if err != nil || environment.Skills()[0].InstallName() != "dev👩‍💻" {
			t.Fatalf("Manifest skill = %#v, %v", environment.Skills(), err)
		}
	})
}

func TestManifestEdgeRoundSixMalformedBoundaryShapes(t *testing.T) {
	tests := []struct {
		name string
		raw  declaration.Manifest
		want string
	}{
		{name: "duplicate top-level target", raw: declaration.Manifest{Version: 1, Targets: []string{"codex", "codex"}}, want: "duplicate target"},
		{name: "unknown default install mode", raw: func() declaration.Manifest {
			raw := baseManifest(target.TargetCodex)
			raw.Defaults.InstallMode = "reflink"
			return raw
		}(), want: "unknown install mode"},
		{name: "exclude without include", raw: func() declaration.Manifest {
			raw := baseManifest(target.TargetCodex)
			raw.SkillGroups = []declaration.SkillGroup{{Exclude: []string{"glob:old-*"}, Source: localSource("skills")}}
			return raw
		}(), want: "names or include selectors are required"},
		{name: "instruction trim collision", raw: func() declaration.Manifest {
			raw := baseManifest(target.TargetCodex)
			raw.Instructions = map[string]declaration.Instructions{
				"project":   {Source: declaration.InstructionSource{Set: true, Source: localSource("A.md")}},
				" project ": {Source: declaration.InstructionSource{Set: true, Source: localSource("B.md")}},
			}
			return raw
		}(), want: "duplicate instructions id"},
		{name: "unterminated hook placeholder", raw: func() declaration.Manifest {
			raw := baseManifest(target.TargetClaudeCode)
			raw.Hooks = []declaration.Hook{{Name: "bad", Event: "event", Command: "run {hook_file:missing"}}
			return raw
		}(), want: "missing closing brace"},
		{name: "unsupported hook directory placeholder", raw: func() declaration.Manifest {
			raw := baseManifest(target.TargetClaudeCode)
			raw.Hooks = []declaration.Hook{{Name: "bad", Event: "event", Command: "run {hook_dir:assets}"}}
			return raw
		}(), want: "hook_dir placeholders are unsupported"},
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

func TestManifestEdgeRoundSevenPreservesDeterministicAuthoredOrder(t *testing.T) {
	t.Run("named git child", func(t *testing.T) {
		raw := baseManifest(target.TargetCodex)
		raw.SkillGroups = []declaration.SkillGroup{{Names: []string{"child"}, Source: gitSource("skills")}}
		environment, err := Manifest(raw)
		if err != nil {
			t.Fatalf("Manifest returned error: %v", err)
		}
		git, ok := environment.Skills()[0].Source().Git()
		if !ok || git.RepositoryPath().String() != "skills/child" || git.Ref().String() != "main" {
			t.Fatalf("child source = %#v", environment.Skills()[0].Source())
		}
	})

	t.Run("interleaved mcp aggregation", func(t *testing.T) {
		raw := baseManifest(target.TargetCodex, target.TargetClaudeCode)
		raw.MCPServers = []declaration.MCPServer{
			{Name: "a", Targets: []string{"codex"}, Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx")},
			{Name: "b", Targets: []string{"claude-code"}, Transport: "stdio", Command: declaration.NewMCPAmbientCommand("uvx")},
			{Name: "a", Targets: []string{"claude-code"}, Transport: "stdio", Command: declaration.NewMCPAmbientCommand("uvx")},
		}
		environment, err := Manifest(raw)
		if err != nil {
			t.Fatalf("Manifest returned error: %v", err)
		}
		servers := environment.MCPServers()
		if len(servers) != 2 || servers[0].ID().Name() != "a" || servers[1].ID().Name() != "b" || servers[0].Bindings()[1].Target() != target.TargetClaudeCode {
			t.Fatalf("servers = %#v, want first-seen server and binding order", servers)
		}
	})

	t.Run("extension authored order", func(t *testing.T) {
		raw := baseManifest(target.TargetOpenCode, target.TargetPi)
		raw.Extensions = []declaration.Extension{
			{ID: "zeta", Carrier: "pi-package", Targets: []string{"pi"}, Source: declaration.ExtensionSource{HostSource: "zeta"}},
			{ID: "alpha", Carrier: "opencode-plugin", Targets: []string{"opencode"}, Source: declaration.ExtensionSource{HostSource: "alpha"}},
		}
		environment, err := Manifest(raw)
		if err != nil || environment.Extensions()[0].ID().Name() != "zeta" || environment.Extensions()[1].ID().Name() != "alpha" {
			t.Fatalf("extensions = %#v, %v", environment.Extensions(), err)
		}
	})

	t.Run("instruction name order", func(t *testing.T) {
		raw := baseManifest(target.TargetCodex)
		raw.Instructions = map[string]declaration.Instructions{
			"zeta":  {Source: declaration.InstructionSource{Set: true, Source: localSource("Z.md")}},
			"alpha": {Source: declaration.InstructionSource{Set: true, Source: localSource("A.md")}},
		}
		environment, err := Manifest(raw)
		if err != nil || environment.Instructions()[0].ID().Name() != "alpha" || environment.Instructions()[1].ID().Name() != "zeta" {
			t.Fatalf("instructions = %#v, %v", environment.Instructions(), err)
		}
	})

	t.Run("valid unicode mcp argument", func(t *testing.T) {
		raw := baseManifest(target.TargetClaudeCode)
		raw.MCPServers = []declaration.MCPServer{{Name: "unicode", Transport: "stdio", Command: declaration.NewMCPAmbientCommand("npx"), Args: []string{"안녕👩‍💻"}}}
		if _, err := Manifest(raw); err != nil {
			t.Fatalf("Manifest rejected valid Unicode argument: %v", err)
		}
	})

	t.Run("valid join control extension source", func(t *testing.T) {
		raw := baseManifest(target.TargetOpenCode)
		raw.Extensions = []declaration.Extension{{ID: "joiner", Carrier: "opencode-plugin", Source: declaration.ExtensionSource{HostSource: "emoji‍tool"}}}
		if _, err := Manifest(raw); err != nil {
			t.Fatalf("Manifest rejected non-bidi format character: %v", err)
		}
	})
}
