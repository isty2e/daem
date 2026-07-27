package codec

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestRenderImportManifestGolden(t *testing.T) {
	body := ImportManifestBody{
		Instructions: map[string]ImportManifestInstruction{
			"project.daily": {
				Source:  "daem.d/instructions/project-daily.md",
				Targets: []string{"codex"},
				Scope:   "project",
				Target: map[string]ImportManifestInstructionRendering{
					"codex": {RenderTo: "AGENTS.md"},
				},
			},
		},
		SkillGroups: []ImportManifestSkillGroup{{
			Names:       []string{"alpha", "beta"},
			Source:      ImportManifestSource{Path: "daem.d/skills/group", Mode: "vendor"},
			Targets:     []string{"codex", "pi"},
			Scope:       "global",
			InstallMode: "copy",
		}},
		Skills: []ImportManifestSkill{{
			ID:          "review-codex",
			Name:        "review",
			Source:      ImportManifestSource{Path: "daem.d/skills/review", Mode: "vendor"},
			Targets:     []string{"codex"},
			Scope:       "project",
			InstallMode: "copy",
		}},
		Hooks: []ImportManifestHook{{
			Name:          "lint",
			Event:         "PreToolUse",
			Matcher:       "Write",
			Type:          "command",
			Command:       "make lint",
			Timeout:       30,
			StatusMessage: "checking",
			Targets:       []string{"codex"},
			Scope:         "project",
			TargetOverrides: []ImportManifestHookTargetOverride{{
				Target:    "codex",
				Condition: "always",
			}},
		}},
		MCPServers: []MCPServer{{
			Name:      "context7",
			Targets:   []string{"codex"},
			Scope:     "global",
			Transport: "stdio",
			Command:   declaration.NewMCPAmbientCommand("npx"),
			Args:      []string{"-y", "@upstash/context7-mcp"},
			Env: map[string]MCPEnvReference{
				"TOKEN": {FromEnv: "CONTEXT7_TOKEN"},
			},
		}},
	}

	got, err := RenderImportManifest([]string{"codex", "pi"}, body)
	if err != nil {
		t.Fatalf("RenderImportManifest returned error: %v", err)
	}
	want := []byte(`version = 1
targets = ["codex", "pi"]

[instructions]
  [instructions."project.daily"]
    source = "daem.d/instructions/project-daily.md"
    targets = ["codex"]
    scope = "project"
    [instructions."project.daily".target]
      [instructions."project.daily".target.codex]
        render_to = "AGENTS.md"

[[skill_group]]
  names = ["alpha", "beta"]
  targets = ["codex", "pi"]
  scope = "global"
  install_mode = "copy"
  [skill_group.source]
    path = "daem.d/skills/group"
    mode = "vendor"

[[skill]]
  id = "review-codex"
  name = "review"
  targets = ["codex"]
  scope = "project"
  install_mode = "copy"
  [skill.source]
    path = "daem.d/skills/review"
    mode = "vendor"

[[hook]]
  name = "lint"
  event = "PreToolUse"
  matcher = "Write"
  type = "command"
  command = "make lint"
  timeout = 30
  status_message = "checking"
  targets = ["codex"]
  scope = "project"

  [[hook.target_override]]
    target = "codex"
    if = "always"

[[mcp_server]]
  name = "context7"
  targets = ["codex"]
  scope = "global"
  transport = "stdio"
  command = "npx"
  args = ["-y", "@upstash/context7-mcp"]
  [mcp_server.env]
    [mcp_server.env.TOKEN]
      from_env = "CONTEXT7_TOKEN"
`)
	if !bytes.Equal(got, want) {
		t.Fatalf("RenderImportManifest =\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderImportManifestBodyCompactsEmptyFamilies(t *testing.T) {
	body := ImportManifestBody{
		Skills: []ImportManifestSkill{{
			Name:        "review",
			Source:      ImportManifestSource{Path: "skills/review", Mode: "vendor"},
			Targets:     []string{"codex"},
			Scope:       "project",
			InstallMode: "copy",
		}},
	}

	got, err := RenderImportManifestBody(body)
	if err != nil {
		t.Fatalf("RenderImportManifestBody returned error: %v", err)
	}
	for _, placeholder := range [][]byte{
		[]byte("instructions = {}"),
		[]byte("skill_group = []"),
		[]byte("hook = []"),
		[]byte("mcp_server = []"),
	} {
		if bytes.Contains(got, placeholder) {
			t.Fatalf("body = %q, want placeholder %q removed", got, placeholder)
		}
	}
	if !bytes.Contains(got, []byte("[[skill]]")) {
		t.Fatalf("body = %q, want skill row", got)
	}
}

func TestRenderImportManifestOrdersMapKeysDeterministically(t *testing.T) {
	body := ImportManifestBody{
		Instructions: map[string]ImportManifestInstruction{
			"z.last": {
				Source:  "z.md",
				Targets: []string{"codex"},
				Scope:   "project",
			},
			"a.first": {
				Source:  "a.md",
				Targets: []string{"codex"},
				Scope:   "project",
			},
		},
		MCPServers: []MCPServer{{
			Name:      "env-order",
			Targets:   []string{"codex"},
			Scope:     "global",
			Transport: "stdio",
			Command:   declaration.NewMCPAmbientCommand("node"),
			Env: map[string]MCPEnvReference{
				"Z_TOKEN": {FromEnv: "Z_TOKEN"},
				"A_TOKEN": {FromEnv: "A_TOKEN"},
			},
		}},
	}

	first, err := RenderImportManifest([]string{"codex"}, body)
	if err != nil {
		t.Fatalf("RenderImportManifest returned error: %v", err)
	}
	for range 50 {
		got, err := RenderImportManifest([]string{"codex"}, body)
		if err != nil {
			t.Fatalf("RenderImportManifest returned error: %v", err)
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("RenderImportManifest changed bytes between identical calls")
		}
	}

	content := string(first)
	for _, orderedPair := range [][2]string{
		{`[instructions."a.first"]`, `[instructions."z.last"]`},
		{"[mcp_server.env.A_TOKEN]", "[mcp_server.env.Z_TOKEN]"},
	} {
		if strings.Index(content, orderedPair[0]) >= strings.Index(content, orderedPair[1]) {
			t.Fatalf("rendered order = %q before %q in:\n%s", orderedPair[0], orderedPair[1], content)
		}
	}
}

func TestAppendImportManifestBodyPreservesExistingBytes(t *testing.T) {
	tests := []struct {
		name     string
		existing []byte
		body     []byte
		want     []byte
	}{
		{
			name:     "empty body returns copy",
			existing: []byte("version = 1\r\ntargets = [\"codex\"]"),
			want:     []byte("version = 1\r\ntargets = [\"codex\"]"),
		},
		{
			name: "retained CRLF prefix",
			existing: []byte(
				"version = 1\r\n" +
					"targets = [\"codex\"]\r\n" +
					"# retained\r\n",
			),
			body: []byte("\n[[skill]]\nname = \"review\"\n\n"),
			want: []byte(
				"version = 1\r\n" +
					"targets = [\"codex\"]\r\n" +
					"# retained\r\n" +
					"\n" +
					"[[skill]]\n" +
					"name = \"review\"\n",
			),
		},
		{
			name: "missing terminal newline",
			existing: []byte(
				"version = 1\n" +
					"targets = [\"codex\"]\n" +
					"# retained",
			),
			body: []byte("[[skill]]\nname = \"review\"\n"),
			want: []byte(
				"version = 1\n" +
					"targets = [\"codex\"]\n" +
					"# retained\n" +
					"\n" +
					"[[skill]]\n" +
					"name = \"review\"\n",
			),
		},
		{
			name: "empty existing document",
			body: []byte("  [[hook]]\nname = \"lint\"  "),
			want: []byte("[[hook]]\nname = \"lint\"\n"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := bytes.Clone(test.existing)
			got := AppendImportManifestBody(test.existing, test.body)
			if !bytes.Equal(got, test.want) {
				t.Fatalf("AppendImportManifestBody = %q, want %q", got, test.want)
			}
			if !bytes.Equal(test.existing, original) {
				t.Fatalf("AppendImportManifestBody mutated existing input")
			}
			if len(got) != 0 && len(test.existing) != 0 && &got[0] == &test.existing[0] {
				t.Fatalf("AppendImportManifestBody aliased existing input")
			}
		})
	}
}
