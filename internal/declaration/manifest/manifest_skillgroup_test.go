package manifest

import (
	"strings"
	"testing"

	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func TestParseRejectsSkillGroupNamesWithSelectors(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill_group]]
names = ["oracle"]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
`))
	if err == nil || !strings.Contains(err.Error(), "use either names or include selectors") {
		t.Fatalf("Bytes error = %v, want names/include conflict", err)
	}
}

func TestParseExpandsSkillGroups(t *testing.T) {
	environment, err := Decode([]byte(`
version = 1
targets = ["codex", "claude-code"]

[defaults]
scope = "project"
install_mode = "copy"

[[skill_group]]
names = ["foo", "bar"]
source = { git = "https://github.com/example/skills.git", path = "skills", ref = "main" }
targets = ["codex"]
scope = "global"
portable = false

[[skill_group]]
names = ["local-review"]
source = { path = "local/skills", mode = "vendor" }
targets = ["claude-code"]
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}

	skills := environment.Skills()
	if len(skills) != 3 {
		t.Fatalf("len(Skills) = %d, want 3", len(skills))
	}

	foo := skills[0]
	if foo.ID().Name() != "foo" || foo.Scope() != target.ScopeGlobal || foo.InstallMode() != desiredskill.InstallModeCopy || foo.Portable() {
		t.Fatalf("foo skill = %#v, want global copy non-portable", foo)
	}

	fooSource, ok := foo.Source().Git()
	if !ok {
		t.Fatal("foo source is not git")
	}
	if fooSource.RepositoryPath().String() != "skills/foo" || fooSource.Ref().String() != "main" {
		t.Fatalf("foo git source = %#v, want skills/foo at main", fooSource)
	}

	barSource, ok := skills[1].Source().Git()
	if !ok {
		t.Fatal("bar source is not git")
	}
	if skills[1].ID().Name() != "bar" || barSource.RepositoryPath().String() != "skills/bar" {
		t.Fatalf("bar skill/source = %#v / %#v, want skills/bar", skills[1], barSource)
	}

	local := skills[2]
	if local.ID().Name() != "local-review" || local.Scope() != target.ScopeProject || !local.Portable() {
		t.Fatalf("local skill = %#v, want default project portable skill", local)
	}

	localSource, ok := local.Source().Local()
	if !ok {
		t.Fatal("local-review source is not local")
	}
	if localSource.Path() != "local/skills/local-review" || localSource.Mode() != source.LocalSourceModeVendor {
		t.Fatalf("local source = %#v, want local/skills/local-review vendor", localSource)
	}
}

func TestParseRejectsDuplicateSkillGroupNames(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "foo"
source = { path = "skills/foo", mode = "vendor" }

[[skill_group]]
names = ["foo"]
source = { path = "skills", mode = "vendor" }
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), `duplicate skill id "foo"`) {
		t.Fatalf("error = %q, want duplicate skill id diagnostic", err)
	}
}

func TestParseRejectsUnsafeSkillGroupNames(t *testing.T) {
	tests := []struct {
		name          string
		tomlSkillName string
	}{
		{name: "empty", tomlSkillName: `""`},
		{name: "dot", tomlSkillName: `"."`},
		{name: "parent", tomlSkillName: `".."`},
		{name: "slash", tomlSkillName: `"foo/bar"`},
		{name: "backslash", tomlSkillName: `'foo\bar'`},
		{name: "home", tomlSkillName: `"~foo"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill_group]]
names = [` + test.tomlSkillName + `]
source = { path = "skills", mode = "vendor" }
`))
			if err == nil {
				t.Fatal("Bytes returned nil error")
			}

			if !strings.Contains(err.Error(), "safe single path segment") {
				t.Fatalf("error = %q, want safe path segment diagnostic", err)
			}
		})
	}
}
