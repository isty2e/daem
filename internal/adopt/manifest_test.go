package adopt

import (
	"reflect"
	"strings"
	"testing"

	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestImportManifestSkillGroupsUseSetIdentityAndProductTargetOrder(t *testing.T) {
	skills := []Skill{
		{
			InstallName: "alpha",
			Targets:     []targetpkg.Target{targetpkg.TargetPi, targetpkg.TargetCodex},
			Scope:       targetpkg.ScopeGlobal,
			GroupRoot:   "skills/group",
		},
		{
			InstallName: "beta",
			Targets:     []targetpkg.Target{targetpkg.TargetCodex, targetpkg.TargetPi},
			Scope:       targetpkg.ScopeGlobal,
			GroupRoot:   "skills/group",
		},
	}

	body, _, err := importManifestTables(nil, skills, nil, nil, nil, make(map[targetpkg.Target]struct{}))
	if err != nil {
		t.Fatalf("importManifestTables returned error: %v", err)
	}
	if len(body.SkillGroups) != 1 {
		t.Fatalf("skill groups = %#v, want one order-independent group", body.SkillGroups)
	}
	group := body.SkillGroups[0]
	if !reflect.DeepEqual(group.Names, []string{"alpha", "beta"}) {
		t.Fatalf("group names = %#v", group.Names)
	}
	if !reflect.DeepEqual(group.Targets, []string{"codex", "pi"}) {
		t.Fatalf("group targets = %#v, want product order", group.Targets)
	}
}

func TestImportManifestTablesRejectsDuplicateResourceIdentities(t *testing.T) {
	tests := []struct {
		name       string
		sources    []Source
		skills     []Skill
		mcpServers []MCPServer
		want       string
	}{
		{
			name: "instruction",
			sources: []Source{
				{ResourceName: "daily", Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeProject},
				{ResourceName: "daily", Target: targetpkg.TargetClaudeCode, Scope: targetpkg.ScopeProject},
			},
			want: `duplicate imported resource "daily"`,
		},
		{
			name: "incompatible skill",
			skills: []Skill{
				{
					ResourceName: "review",
					InstallName:  "review",
					Targets:      []targetpkg.Target{targetpkg.TargetCodex},
					Scope:        targetpkg.ScopeProject,
					SourcePath:   "skills/review",
				},
				{
					ResourceName: "review",
					InstallName:  "other",
					Targets:      []targetpkg.Target{targetpkg.TargetClaudeCode},
					Scope:        targetpkg.ScopeProject,
					SourcePath:   "skills/review",
				},
			},
			want: `duplicate imported skill resource "review" has incompatible source or skill name`,
		},
		{
			name: "mcp server",
			mcpServers: []MCPServer{
				{ResourceName: "context7", Target: targetpkg.TargetCodex, Scope: targetpkg.ScopeGlobal},
				{ResourceName: "context7", Target: targetpkg.TargetClaudeCode, Scope: targetpkg.ScopeGlobal},
			},
			want: `duplicate imported mcp_server resource "context7"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := importManifestTables(
				test.sources,
				test.skills,
				nil,
				test.mcpServers,
				nil,
				make(map[targetpkg.Target]struct{}),
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("importManifestTables error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestImportManifestDirectSkillAndMergePreserveAuthoredOrder(t *testing.T) {
	body, _, err := importManifestTables(nil, []Skill{{
		ResourceName: "alpha",
		InstallName:  "alpha",
		Targets:      []targetpkg.Target{targetpkg.TargetPi, targetpkg.TargetCodex},
		Scope:        targetpkg.ScopeGlobal,
		SourcePath:   "skills/alpha",
	}}, nil, nil, nil, make(map[targetpkg.Target]struct{}))
	if err != nil {
		t.Fatalf("importManifestTables returned error: %v", err)
	}
	if len(body.Skills) != 1 || !reflect.DeepEqual(body.Skills[0].Targets, []string{"pi", "codex"}) {
		t.Fatalf("direct skill targets = %#v, want authored order", body.Skills)
	}

	merged := mergeImportTargetStrings(
		[]string{"pi", "pi", "codex"},
		[]targetpkg.Target{targetpkg.TargetClaudeCode, targetpkg.TargetPi},
	)
	if !reflect.DeepEqual(merged, []string{"pi", "codex", "claude-code"}) {
		t.Fatalf("merged targets = %#v, want existing-first unique order", merged)
	}
}
