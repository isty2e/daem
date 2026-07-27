package manifest

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestDecodePreservesDirectAndGroupedSkillTargetPlacements(t *testing.T) {
	environment, err := Decode([]byte(`
version = 1
targets = ["codex", "opencode"]

[[skill]]
name = "direct"
source = { git = "https://example.test/skills.git", path = "skills/direct", ref = "main" }
targets = ["codex"]
scope = "global"

[skill.target.codex]
install_to = "~/.codex/skills"

[[skill_group]]
names = ["alpha", "beta"]
source = { path = "skills", mode = "vendor" }
targets = ["opencode"]
scope = "project"

[skill_group.target.opencode]
install_to = ".agents/skills"
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	skills := environment.Skills()
	if len(skills) != 3 {
		t.Fatalf("Skills = %d, want 3", len(skills))
	}
	if got := skills[0].TargetPlacements()[target.TargetCodex].InstallTo(); got != "~/.codex/skills" {
		t.Fatalf("direct install_to = %q", got)
	}
	for _, grouped := range skills[1:] {
		if got := grouped.TargetPlacements()[target.TargetOpenCode].InstallTo(); got != ".agents/skills" {
			t.Fatalf("grouped install_to = %q", got)
		}
	}
}

func TestDecodeRejectsInvalidSkillTargetPlacementShapes(t *testing.T) {
	tests := []struct {
		name      string
		targets   string
		scope     string
		target    string
		installTo string
		want      string
	}{
		{name: "unknown target", targets: `"codex"`, scope: "project", target: "future", installTo: ".agents/skills", want: "unknown target"},
		{name: "undeclared target", targets: `"codex"`, scope: "project", target: "pi", installTo: ".pi/skills", want: "not declared"},
		{name: "wrong project scope", targets: `"codex"`, scope: "project", target: "codex", installTo: "~/.codex/skills", want: "project-relative"},
		{name: "wrong global scope", targets: `"codex"`, scope: "global", target: "codex", installTo: ".agents/skills", want: "must start with ~/"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(`
version = 1
targets = [` + test.targets + `]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = [` + test.targets + `]
scope = "` + test.scope + `"

[skill.target.` + test.target + `]
install_to = "` + test.installTo + `"
`))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDecodeRejectsMalformedSkillTargetPlacementTables(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "duplicate target table",
			body: `
[skill.target.codex]
install_to = ".agents/skills"

[skill.target.codex]
install_to = ".codex/skills"
`,
			want: "already been defined",
		},
		{
			name: "unknown target-local key",
			body: `
[skill.target.codex]
install_to = ".agents/skills"
replicate = true
`,
			want: `unknown manifest key "skill.target.codex.replicate"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["codex"]
scope = "project"
` + test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode error = %v, want %q", err, test.want)
			}
		})
	}
}
