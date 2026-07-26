package manifest

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRejectsNonBooleanCompatRepair(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{
			name: "skill",
			manifest: `
version = 1
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
compat_repair = "true"
`,
		},
		{
			name: "skill_group",
			manifest: `
version = 1
targets = ["codex"]

[[skill_group]]
names = ["review"]
source = { path = "skills", mode = "vendor" }
compat_repair = "true"
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.manifest))
			if err == nil {
				t.Fatal("Bytes returned nil error")
			}
			if !strings.Contains(err.Error(), "compat_repair") {
				t.Fatalf("error = %q, want compat_repair diagnostic", err.Error())
			}
		})
	}
}

func TestParseRejectsDuplicateSkillDestinationNameForSameTargetScope(t *testing.T) {
	tempDir := t.TempDir()
	firstPath := filepath.ToSlash(filepath.Join(tempDir, "skills", "first"))
	secondPath := filepath.ToSlash(filepath.Join(tempDir, "skills", "second"))
	_, err := Decode([]byte(`
version = 1
	targets = ["codex"]

	[[skill]]
	id = "first_review"
	name = "review"
	source = { path = "` + firstPath + `", mode = "vendor" }
	targets = ["codex"]
	scope = "global"

	[[skill]]
	id = "second_review"
	name = "review"
	source = { path = "` + secondPath + `", mode = "vendor" }
	targets = ["codex"]
	scope = "global"
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}
	if !strings.Contains(err.Error(), `duplicate skill destination name "review"`) {
		t.Fatalf("error = %q, want duplicate destination name diagnostic", err.Error())
	}
}

func TestParseRejectsDuplicateSkillID(t *testing.T) {
	tempDir := t.TempDir()
	firstPath := filepath.ToSlash(filepath.Join(tempDir, "skills", "first"))
	secondPath := filepath.ToSlash(filepath.Join(tempDir, "skills", "second"))
	_, err := Decode([]byte(`
	version = 1
	targets = ["codex", "claude-code"]

	[[skill]]
	id = "shared_review"
	name = "review"
	source = { path = "` + firstPath + `", mode = "vendor" }
	targets = ["codex"]

	[[skill]]
	id = "shared_review"
	name = "debug"
	source = { path = "` + secondPath + `", mode = "vendor" }
	targets = ["claude-code"]
	`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}
	if !strings.Contains(err.Error(), `duplicate skill id "shared_review"`) {
		t.Fatalf("error = %q, want duplicate skill id diagnostic", err.Error())
	}
}

func TestParseRejectsRemovedSkillInstallNameField(t *testing.T) {
	_, err := Decode([]byte(`
	version = 1
	targets = ["codex"]

	[[skill]]
	name = "review"
	install_name = "legacy-review"
	source = { path = "skills/review", mode = "vendor" }
	`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}
	if !strings.Contains(err.Error(), "unknown manifest key") || !strings.Contains(err.Error(), "install_name") {
		t.Fatalf("error = %q, want unknown install_name diagnostic", err.Error())
	}
}

func TestParseAllowsDuplicateSkillNameForDifferentTargetsWithDistinctIDs(t *testing.T) {
	tempDir := t.TempDir()
	codexPath := filepath.ToSlash(filepath.Join(tempDir, "skills", "codex-review"))
	claudePath := filepath.ToSlash(filepath.Join(tempDir, "skills", "claude-review"))
	environment, err := Decode([]byte(`
version = 1
	targets = ["codex", "claude-code"]

	[[skill]]
	id = "codex_global_review"
	name = "review"
	source = { path = "` + codexPath + `", mode = "vendor" }
	targets = ["codex"]
	scope = "global"

	[[skill]]
	id = "claude_code_global_review"
	name = "review"
	source = { path = "` + claudePath + `", mode = "vendor" }
	targets = ["claude-code"]
	scope = "global"
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}
	if len(environment.Skills()) != 2 {
		t.Fatalf("skills = %#v, want two target-specific skills", environment.Skills())
	}
}
