package skillcompat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func TestValidateSkillArtifactAcceptsSkillDirectory(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "SKILL.md"), []byte("---\nname: demo\n---\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	err := validateSkillArtifactPath(t, tempDir, "local:skills/demo?mode=vendor")
	if err != nil {
		t.Fatalf("ValidateSkillArtifact returned error: %v", err)
	}
}

func TestValidateSkillArtifactRejectsMissingSkillFile(t *testing.T) {
	err := validateSkillArtifactPath(t, t.TempDir(), "local:skills/demo?mode=vendor")
	if err == nil {
		t.Fatal("ValidateSkillArtifact returned nil error")
	}

	if !strings.Contains(err.Error(), "missing SKILL.md") {
		t.Fatalf("error = %q, want missing SKILL.md diagnostic", err)
	}
}

func TestValidateSkillArtifactRejectsLowercaseSkillFile(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "skill.md"), []byte("---\nname: demo\n---\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	err := validateSkillArtifactPath(t, tempDir, "local:skills/demo?mode=vendor")
	if err == nil {
		t.Fatal("ValidateSkillArtifact returned nil error")
	}

	if !strings.Contains(err.Error(), "missing SKILL.md") {
		t.Fatalf("error = %q, want case-sensitive missing SKILL.md diagnostic", err)
	}
}

func TestValidateSkillArtifactRejectsFileArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "protect_env.py")
	if err := os.WriteFile(path, []byte("print('protected')\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	err := ValidateSkillArtifact(
		context.Background(),
		openSkillView(t, path),
		"local:hooks/protect_env.py?mode=link",
	)
	if err == nil {
		t.Fatal("ValidateSkillArtifact returned nil error")
	}

	if !strings.Contains(err.Error(), "must resolve to a directory") {
		t.Fatalf("error = %q, want directory diagnostic", err)
	}
}

func TestValidateSkillArtifactRejectsSkillFileDirectory(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tempDir, "SKILL.md"), 0o700); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}

	err := validateSkillArtifactPath(t, tempDir, "local:skills/demo?mode=vendor")
	if err == nil {
		t.Fatal("ValidateSkillArtifact returned nil error")
	}

	if !strings.Contains(err.Error(), "non-regular file") {
		t.Fatalf("error = %q, want non-regular file diagnostic", err)
	}
}

func TestValidateSkillArtifactRejectsSkillFileSymlink(t *testing.T) {
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "target.md")
	skillPath := filepath.Join(tempDir, "SKILL.md")

	if err := os.WriteFile(targetPath, []byte("---\nname: demo\n---\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := os.Symlink(targetPath, skillPath); err != nil {
		t.Skipf("symlinks are unavailable on this platform: %v", err)
	}

	err := validateSkillArtifactPath(t, tempDir, "local:skills/demo?mode=vendor")
	if err == nil {
		t.Fatal("ValidateSkillArtifact returned nil error")
	}

	if !strings.Contains(err.Error(), "SKILL.md as a symlink") {
		t.Fatalf("error = %q, want symlink diagnostic", err)
	}
}

func TestLoadSkillFrontmatterParsesYamlFeatures(t *testing.T) {
	tempDir := writeSkillFrontmatter(t, "oracle", "---\nname: \"oracle:review\"\ndescription: >\n  Use for oracle review.\n  Handles quoted colons.\nmetadata:\n  owner: platform\nallowed-tools:\n  - Bash(git status *)\n---\n# Oracle\n")

	frontmatter, err := loadSkillFrontmatterPath(t, tempDir)
	if err != nil {
		t.Fatalf("LoadSkillFrontmatter returned error: %v", err)
	}
	if frontmatter.Name != "oracle:review" {
		t.Fatalf("Name = %q, want quoted scalar with colon", frontmatter.Name)
	}
	if !strings.Contains(frontmatter.Description, "Handles quoted colons.") {
		t.Fatalf("Description = %q, want folded multiline text", frontmatter.Description)
	}
	for _, field := range []string{"name", "description", "metadata", "allowed-tools"} {
		if _, ok := frontmatter.Fields[field]; !ok {
			t.Fatalf("Fields = %#v, want field %q", frontmatter.Fields, field)
		}
	}
}

func TestLoadSkillFrontmatterParsesBOMAndCRLF(t *testing.T) {
	tempDir := writeSkillFrontmatter(t, "oracle", "\xef\xbb\xbf---\r\nname: oracle\r\ndescription: Review with CRLF.\r\n---\r\n# Oracle\r\n")

	frontmatter, err := loadSkillFrontmatterPath(t, tempDir)
	if err != nil {
		t.Fatalf("LoadSkillFrontmatter returned error: %v", err)
	}
	if frontmatter.Name != "oracle" || frontmatter.Description != "Review with CRLF." {
		t.Fatalf("frontmatter = %#v, want BOM/CRLF normalized scalars", frontmatter)
	}
}

func TestSkillFrontmatterReportsCanonicalStringFieldState(t *testing.T) {
	frontmatter, err := ParseSkillFrontmatter([]byte("---\nname: null\nlicense: MIT\nmetadata:\n  owner: platform\n---\n"))
	if err != nil {
		t.Fatalf("ParseSkillFrontmatter returned error: %v", err)
	}
	if value, present := frontmatter.StringField("name"); !present || value != "" {
		t.Fatalf("name string field = %q, %t, want present normalized null", value, present)
	}
	if value, present := frontmatter.StringField("license"); !present || value != "MIT" {
		t.Fatalf("license string field = %q, %t, want MIT", value, present)
	}
	if _, present := frontmatter.StringField("metadata"); present {
		t.Fatal("mapping-valued metadata reported as a string field")
	}
	if _, present := frontmatter.Fields["metadata"]; !present {
		t.Fatal("mapping-valued metadata disappeared from field presence")
	}
}

func TestLoadSkillFrontmatterAllowsIndentedDelimiterInsideBlockScalar(t *testing.T) {
	tempDir := writeSkillFrontmatter(t, "oracle", "---\nname: oracle\ndescription: |\n  Before delimiter-looking text.\n  ---\n  After delimiter-looking text.\n---\n# Oracle\n")

	frontmatter, err := loadSkillFrontmatterPath(t, tempDir)
	if err != nil {
		t.Fatalf("LoadSkillFrontmatter returned error: %v", err)
	}
	if !strings.Contains(frontmatter.Description, "After delimiter-looking text.") {
		t.Fatalf("Description = %q, want indented delimiter text preserved", frontmatter.Description)
	}
}

func TestLoadSkillFrontmatterRejectsIndentedOpeningDelimiter(t *testing.T) {
	tempDir := writeSkillFrontmatter(t, "oracle", " ---\nname: oracle\ndescription: Review.\n---\n")

	_, err := loadSkillFrontmatterPath(t, tempDir)
	if err == nil {
		t.Fatal("LoadSkillFrontmatter returned nil error")
	}
	if !strings.Contains(err.Error(), "frontmatter is required") {
		t.Fatalf("error = %q, want frontmatter diagnostic", err)
	}
}

func TestLoadSkillFrontmatterResolvesScalarAliases(t *testing.T) {
	tempDir := writeSkillFrontmatter(t, "oracle", "---\nname: oracle\nshared-description: &description Use for oracle review.\ndescription: *description\n---\n")

	frontmatter, err := loadSkillFrontmatterPath(t, tempDir)
	if err != nil {
		t.Fatalf("LoadSkillFrontmatter returned error: %v", err)
	}
	if frontmatter.Description != "Use for oracle review." {
		t.Fatalf("Description = %q, want resolved scalar alias", frontmatter.Description)
	}
}

func TestLoadSkillFrontmatterRejectsMissingFrontmatter(t *testing.T) {
	tempDir := writeSkillFrontmatter(t, "oracle", "# Oracle\n")

	_, err := loadSkillFrontmatterPath(t, tempDir)
	if err == nil {
		t.Fatal("LoadSkillFrontmatter returned nil error")
	}
	if !strings.Contains(err.Error(), "frontmatter is required") {
		t.Fatalf("error = %q, want frontmatter diagnostic", err)
	}
}

func TestLoadSkillFrontmatterRejectsNonMappingFrontmatter(t *testing.T) {
	tempDir := writeSkillFrontmatter(t, "oracle", "---\n- name\n- description\n---\n")

	_, err := loadSkillFrontmatterPath(t, tempDir)
	if err == nil {
		t.Fatal("LoadSkillFrontmatter returned nil error")
	}
	if !strings.Contains(err.Error(), "must be a YAML mapping") {
		t.Fatalf("error = %q, want mapping diagnostic", err)
	}
}

func TestLoadSkillFrontmatterRejectsDuplicateFields(t *testing.T) {
	tempDir := writeSkillFrontmatter(t, "oracle", "---\nname: oracle\nname: other\ndescription: Review.\n---\n")

	_, err := loadSkillFrontmatterPath(t, tempDir)
	if err == nil {
		t.Fatal("LoadSkillFrontmatter returned nil error")
	}
	if !strings.Contains(err.Error(), `field "name" is duplicated`) {
		t.Fatalf("error = %q, want duplicate field diagnostic", err)
	}
}

func TestLoadSkillFrontmatterRejectsNonStringNameAndDescription(t *testing.T) {
	tempDir := writeSkillFrontmatter(t, "oracle", "---\nname: 123\ndescription: Review.\n---\n")

	_, err := loadSkillFrontmatterPath(t, tempDir)
	if err == nil {
		t.Fatal("LoadSkillFrontmatter returned nil error")
	}
	if !strings.Contains(err.Error(), `field "name" must be a string`) {
		t.Fatalf("error = %q, want string field diagnostic", err)
	}
}

func TestLoadSkillFrontmatterRejectsNonStringDescription(t *testing.T) {
	tempDir := writeSkillFrontmatter(t, "oracle", "---\nname: oracle\ndescription:\n  - Review\n---\n")

	_, err := loadSkillFrontmatterPath(t, tempDir)
	if err == nil {
		t.Fatal("LoadSkillFrontmatter returned nil error")
	}
	if !strings.Contains(err.Error(), `field "description" must be a string`) {
		t.Fatalf("error = %q, want string field diagnostic", err)
	}
}

func writeSkillFrontmatter(t *testing.T, name string, content string) string {
	t.Helper()

	tempDir := t.TempDir()
	skillPath := filepath.Join(tempDir, name)
	if err := os.MkdirAll(skillPath, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return skillPath
}

func validateSkillArtifactPath(t *testing.T, contentPath string, sourceID artifact.SourceID) error {
	t.Helper()
	return ValidateSkillArtifact(context.Background(), openSkillView(t, contentPath), sourceID)
}

func loadSkillFrontmatterPath(t *testing.T, contentPath string) (SkillFrontmatter, error) {
	t.Helper()
	return LoadSkillFrontmatter(
		context.Background(),
		openSkillView(t, contentPath),
		"local:skills/oracle?mode=vendor",
	)
}

func openSkillView(t *testing.T, contentPath string) access.View {
	t.Helper()
	view, err := access.OpenView(contentPath)
	if err != nil {
		t.Fatalf("OpenView(%q) returned error: %v", contentPath, err)
	}
	return view
}
