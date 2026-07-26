package clipresent

import (
	"slices"
	"testing"

	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestListRowsOwnsExactProjectionOrderingAndTypedFiltering(t *testing.T) {
	content := []byte(`
version = 1
targets = ["codex", "claude-code"]

[instructions.project]
source = "AGENTS.md"
targets = ["codex"]

[[skill]]
id = "codex-review"
name = "review"
source = { git = "https://github.com/owner/repo.git", path = "skills/review", ref = "main" }
targets = ["codex"]
scope = "global"

[[skill_group]]
names = ["alpha", "beta"]
source = { path = "skills", mode = "vendor" }
targets = ["claude-code"]

[[hook]]
name = "prime-session"
event = "SessionStart"
command = "bd prime"
targets = ["codex"]
`)
	environment, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	groups, err := declarationcodec.SkillGroupMembership(content)
	if err != nil {
		t.Fatalf("Membership returned error: %v", err)
	}
	allTargets, err := targetselection.ForAvailableTargets(environment.Targets(), nil)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}

	rows := ListRows(environment, groups, allTargets)
	want := []ListRow{
		{
			Kind:        "instructions",
			Key:         "project",
			InstallName: "-",
			Source:      "local:AGENTS.md?mode=vendor",
			Targets:     "codex",
			Scope:       "project",
			Group:       "-",
		},
		{
			Kind:        "skill",
			Key:         "alpha",
			InstallName: "alpha",
			Source:      "local:skills/alpha?mode=vendor",
			Targets:     "claude-code",
			Scope:       "project",
			Group:       "skill_group[0]",
		},
		{
			Kind:        "skill",
			Key:         "beta",
			InstallName: "beta",
			Source:      "local:skills/beta?mode=vendor",
			Targets:     "claude-code",
			Scope:       "project",
			Group:       "skill_group[0]",
		},
		{
			Kind:        "skill",
			Key:         "codex-review",
			InstallName: "review",
			Source:      "git:locator=https%3A%2F%2Fgithub.com%2Fowner%2Frepo.git&path=skills%2Freview&ref=name%3Amain",
			Targets:     "codex",
			Scope:       "global",
			Group:       "-",
		},
		{
			Kind:        "hook",
			Key:         "prime-session",
			InstallName: "-",
			Source:      "command-hook",
			Targets:     "codex",
			Scope:       "project",
			Group:       "-",
		},
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}

	claudeOnly, err := targetselection.ForAvailableTargets(environment.Targets(), []string{"claude-code"})
	if err != nil {
		t.Fatalf("ForAvailableTargets claude-code returned error: %v", err)
	}
	filtered := ListRows(environment, groups, claudeOnly)
	if len(filtered) != 2 || filtered[0].Key != "alpha" || filtered[1].Key != "beta" {
		t.Fatalf("filtered rows = %#v, want alpha and beta", filtered)
	}
}

func TestListRowsFiltersByTypedTargetsWithoutNarrowingDisplayedBinding(t *testing.T) {
	content := []byte(`
version = 1
targets = ["codex", "claude-code"]

[[skill]]
id = "shared-review"
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["codex", "claude-code"]
`)
	environment, err := declarationmanifest.Decode(content)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	selection, err := targetselection.ForAvailableTargets(
		environment.Targets(),
		[]string{"claude-code", "claude-code"},
	)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}

	rows := ListRows(environment, nil, selection)
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want shared skill", rows)
	}
	if rows[0].Targets != "codex,claude-code" {
		t.Fatalf("Targets = %q, want complete authored binding", rows[0].Targets)
	}
}
