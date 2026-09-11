package authoring

import (
	"testing"

	"github.com/isty2e/daem/internal/declaration"
)

func TestGitAuthoringPreservesLocatorsBeforeShorthand(t *testing.T) {
	for _, test := range []struct{ name, input, sourcePath, wantGit, wantPath string }{
		{"scp user", "deploy@example.com:team/guidance", "AGENTS.md", "deploy@example.com:team/guidance", "AGENTS.md"},
		{"scp no user", "example.com:team/guidance", "AGENTS.md", "example.com:team/guidance", "AGENTS.md"},
		{"scp nested", "deploy@example.com:team/tools/guidance", "AGENTS.md", "deploy@example.com:team/tools/guidance", "AGENTS.md"},
		{"scp git user", "git@example.com:team/guidance", "AGENTS.md", "git@example.com:team/guidance", "AGENTS.md"},
		{"scp suffix", "deploy@example.com:team/guidance.git", "AGENTS.md", "deploy@example.com:team/guidance.git", "AGENTS.md"},
		{"shorthand separate", "owner/repo", "AGENTS.md", "https://github.com/owner/repo.git", "AGENTS.md"},
		{"shorthand embedded", "owner/repo/docs/AGENTS.md", "", "https://github.com/owner/repo.git", "docs/AGENTS.md"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			check := func(t *testing.T, kind string, git string, path string, ref string, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("%s: %v", kind, err)
				}
				if git != test.wantGit || path != test.wantPath || ref != "main" {
					t.Fatalf("%s source = (%q, %q, %q), want (%q, %q, main)", kind, git, path, ref, test.wantGit, test.wantPath)
				}
			}
			t.Run("instruction", func(t *testing.T) {
				got, err := InstructionFromAddRequest(AddInstructionRequest{Name: "project", SourceArg: test.input, SourcePath: test.sourcePath, Ref: "main"}, root, declaration.ManifestHeader{})
				check(t, "instruction", got.Source.Git, got.Source.Path, got.Source.Ref, err)
			})
			t.Run("skill", func(t *testing.T) {
				got, _, err := skillSource(AddSkillRequest{SourceArg: test.input, SourcePath: test.sourcePath, Ref: "main"}, root)
				check(t, "skill", got.Git, got.Path, got.Ref, err)
			})
			t.Run("skill-group", func(t *testing.T) {
				got, err := skillGroupSource(AddSkillGroupRequest{SourceArg: test.input, SourcePath: test.sourcePath, Ref: "main"}, root)
				check(t, "skill-group", got.Git, got.Path, got.Ref, err)
			})
		})
	}
}
