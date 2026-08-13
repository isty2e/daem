package manifest

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/source"
)

func TestParseRejectsNoncanonicalSkillFamilySourceKeys(t *testing.T) {
	families := []struct {
		name        string
		declaration string
	}{
		{
			name:        "skill",
			declaration: "[[skill]]\nname = \"review\"",
		},
		{
			name:        "skill_group",
			declaration: "[[skill_group]]\ninclude = [\"glob:*\"]",
		},
	}
	sources := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "case alias",
			raw:  `Git = "https://example.com/review.git", path = ".", ref = "main"`,
			want: `source key "Git" must use canonical spelling "git"`,
		},
		{
			name: "canonical then case alias",
			raw:  `git = "https://example.com/expected.git", Git = "https://example.com/alternate.git", path = ".", ref = "main"`,
			want: `duplicate source key "git" after normalization`,
		},
		{
			name: "case alias then canonical",
			raw:  `Git = "https://example.com/alternate.git", git = "https://example.com/expected.git", path = ".", ref = "main"`,
			want: `duplicate source key "git" after normalization`,
		},
		{
			name: "whitespace alias collision",
			raw:  `path = "skills/review", " path " = "skills/alternate", mode = "vendor"`,
			want: `duplicate source key "path" after normalization`,
		},
	}

	for _, family := range families {
		for _, source := range sources {
			t.Run(family.name+"/"+source.name, func(t *testing.T) {
				manifest := fmt.Sprintf(
					"version = 1\ntargets = [\"codex\"]\n\n%s\nsource = { %s }\n",
					family.declaration,
					source.raw,
				)
				_, err := Decode([]byte(manifest))
				if err == nil || !strings.Contains(err.Error(), source.want) {
					t.Fatalf("error = %v, want %q", err, source.want)
				}
			})
		}
	}
}

func TestParseKeepsSkillFamilySourceInlineTableOnly(t *testing.T) {
	families := []string{
		"[[skill]]\nname = \"review\"",
		"[[skill_group]]\ninclude = [\"glob:*\"]",
	}
	for _, family := range families {
		manifest := fmt.Sprintf(
			"version = 1\ntargets = [\"codex\"]\n\n%s\nsource = \"skills/review\"\n",
			family,
		)
		_, err := Decode([]byte(manifest))
		if err == nil || !strings.Contains(err.Error(), "source must be an inline table") {
			t.Fatalf("error = %v, want inline-table source diagnostic", err)
		}
	}
}

func TestParseAcceptsS3ObjectSources(t *testing.T) {
	environment, err := Decode([]byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = { s3 = "s3://daem/instructions/AGENTS.md", version_id = "v1", region = "us-east-1" }
targets = ["codex"]

[[skill]]
name = "oracle"
source = { s3 = "s3://daem/skills/oracle.tar.gz", format = "tar.gz", version_id = "v2" }
targets = ["codex"]
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}

	instructionSource, ok := environment.Instructions()[0].Source().S3()
	if !ok {
		t.Fatal("instruction source is not s3")
	}
	if instructionSource.Format() != source.S3ObjectFormatFile || instructionSource.VersionID() != "v1" || instructionSource.Region() != "us-east-1" {
		t.Fatalf("instruction S3 source = %#v", instructionSource)
	}

	skillSource, ok := environment.Skills()[0].Source().S3()
	if !ok {
		t.Fatal("skill source is not s3")
	}
	if skillSource.Format() != source.S3ObjectFormatTarGzip || skillSource.VersionID() != "v2" {
		t.Fatalf("skill S3 source = %#v", skillSource)
	}
}

func TestParseRejectsFileFormatS3Skill(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { s3 = "s3://daem/skills/oracle.md" }
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "S3 skill sources must use archive format") {
		t.Fatalf("error = %q, want S3 skill archive diagnostic", err)
	}
}

func TestParseRejectsArchiveFormatS3Instructions(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = { s3 = "s3://daem/instructions/project.tar.gz", format = "tar.gz" }
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "instruction S3 sources must use file format") {
		t.Fatalf("error = %q, want S3 instruction file diagnostic", err)
	}
}

func TestParseRejectsLocalLinkInstructions(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = { path = "AGENTS.md", mode = "link" }
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "instruction local sources must use vendor mode") {
		t.Fatalf("error = %q, want instruction vendor mode diagnostic", err)
	}
}

func TestParseRejectsGlobalRelativeLocalInstructionShorthand(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[instructions.global]
source = "AGENTS.md"
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "global local filesystem sources must use an absolute path") {
		t.Fatalf("error = %q, want global local absolute path diagnostic", err)
	}
}

func TestParseRejectsGlobalRelativeStructuredLocalInstruction(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[instructions.global]
source = { path = "AGENTS.md", mode = "vendor" }
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "global local filesystem sources must use an absolute path") {
		t.Fatalf("error = %q, want global local absolute path diagnostic", err)
	}
}

func TestParseRejectsGlobalRelativeLocalSkill(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
scope = "global"
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "global local filesystem sources must use an absolute path") {
		t.Fatalf("error = %q, want global local absolute path diagnostic", err)
	}
}

func TestParseRejectsGlobalRelativeLocalSkillGroup(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill_group]]
names = ["oracle"]
source = { path = "skills", mode = "vendor" }
scope = "global"
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "global local filesystem sources must use an absolute path") {
		t.Fatalf("error = %q, want global local absolute path diagnostic", err)
	}
}

func TestParseAcceptsGlobalAbsoluteLocalSources(t *testing.T) {
	instructionPath := filepath.Join(t.TempDir(), "AGENTS.md")
	skillPath := filepath.Join(t.TempDir(), "skills", "oracle")

	environment, err := Decode([]byte(`
version = 1
targets = ["codex"]

[instructions.global]
source = "` + filepath.ToSlash(instructionPath) + `"

[[skill]]
name = "oracle"
source = { path = "` + filepath.ToSlash(skillPath) + `", mode = "vendor" }
scope = "global"
`))
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}

	instructionSource, ok := environment.Instructions()[0].Source().Local()
	if !ok || instructionSource.Path() != filepath.ToSlash(instructionPath) {
		t.Fatalf("instruction source = %#v, want %q", instructionSource, filepath.ToSlash(instructionPath))
	}
	skillSource, ok := environment.Skills()[0].Source().Local()
	if !ok || skillSource.Path() != filepath.ToSlash(skillPath) {
		t.Fatalf("skill source = %#v, want %q", skillSource, filepath.ToSlash(skillPath))
	}
}

func TestParseRejectsMixedS3AndLocalSourceFields(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "oracle"
source = { s3 = "s3://daem/skills/oracle.tar.gz", path = "skills/oracle", format = "tar.gz" }
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "s3 sources cannot set path") {
		t.Fatalf("error = %q, want mixed source diagnostic", err)
	}
}

func TestParseRejectsS3SkillGroups(t *testing.T) {
	_, err := Decode([]byte(`
version = 1
targets = ["codex"]

[[skill_group]]
names = ["foo"]
source = { s3 = "s3://daem/skills.tar.gz", format = "tar.gz" }
`))
	if err == nil {
		t.Fatal("Bytes returned nil error")
	}

	if !strings.Contains(err.Error(), "S3 prefix directory sources are unsupported") {
		t.Fatalf("error = %q, want unsupported S3 skill group diagnostic", err)
	}
}
