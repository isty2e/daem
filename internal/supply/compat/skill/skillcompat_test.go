package skillcompat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
)

func TestProfilesCoverCompatibilityAxes(t *testing.T) {
	for _, target := range []target.Target{
		target.TargetCodex,
		target.TargetClaudeCode,
		target.TargetOpenCode,
		target.TargetPi,
		target.TargetAntigravityCLI,
	} {
		t.Run(string(target), func(t *testing.T) {
			profile, ok := profileForTarget(target)
			if !ok {
				t.Fatalf("profileForTarget(%q) ok = false, want true", target)
			}
			if !profile.Artifact.RequiresDirectory || !profile.Artifact.RequiresUppercaseSkillFile {
				t.Fatalf("Artifact = %#v, want directory with uppercase SKILL.md", profile.Artifact)
			}
			if len(profile.Discovery.ProjectRoots)+len(profile.Discovery.GlobalRoots) == 0 {
				t.Fatalf("Discovery = %#v, want at least one root", profile.Discovery)
			}
			if !profile.Frontmatter.RequireFrontmatter {
				t.Fatalf("Frontmatter = %#v, want required YAML frontmatter", profile.Frontmatter)
			}
			if profile.Identity.AddressedBy == "" {
				t.Fatalf("Identity = %#v, want addressability rule", profile.Identity)
			}
			if !profile.Selection.DescriptionAffectsSelection {
				t.Fatalf("Selection = %#v, want description selection semantics", profile.Selection)
			}
			if len(profile.ControlFields.RecognizedFrontmatterFields) == 0 {
				t.Fatalf("ControlFields = %#v, want recognized frontmatter fields", profile.ControlFields)
			}
			if profile.Collision.Behavior == "" {
				t.Fatalf("Collision = %#v, want collision behavior", profile.Collision)
			}
		})
	}
}

func TestAntigravityCLIRequiresNameAndDescriptionButAllowsDirectoryMismatch(t *testing.T) {
	artifact := writeSkill(t, "antigravity_guide", "---\nname: antigravity-guide\ndescription: Guide for Antigravity.\n---\n")

	diagnostics := artifact.diagnostics("antigravity_guide", target.TargetAntigravityCLI)

	assertNoBlockingDiagnostics(t, diagnostics)
	if err := artifact.validate("antigravity_guide", target.TargetAntigravityCLI); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestAntigravityCLIBlocksMissingNameOrDescription(t *testing.T) {
	artifact := writeSkill(t, "antigravity-guide", "---\nmetadata:\n  owner: platform\n---\n")

	diagnostics := artifact.diagnostics("antigravity-guide", target.TargetAntigravityCLI)

	assertDiagnostic(t, diagnostics, SeverityError, AxisFrontmatter, "name is required")
	assertDiagnostic(t, diagnostics, SeverityError, AxisFrontmatter, "description is required")
	if err := artifact.validate("antigravity-guide", target.TargetAntigravityCLI); err == nil {
		t.Fatal("Validate returned nil error")
	}
}

func TestCodexRequiresNameAndDescription(t *testing.T) {
	artifact := writeSkill(t, "oracle", "---\nmetadata:\n  owner: platform\n---\n")

	diagnostics := artifact.diagnostics("oracle", target.TargetCodex)

	assertDiagnostic(t, diagnostics, SeverityError, AxisFrontmatter, "name is required")
	assertDiagnostic(t, diagnostics, SeverityError, AxisFrontmatter, "description is required")
}

func TestClaudeCodeAllowsMissingNameAndWarnsForMissingDescription(t *testing.T) {
	artifact := writeSkill(t, "oracle", "---\n---\n")

	diagnostics := artifact.diagnostics("oracle", target.TargetClaudeCode)

	assertNoBlockingDiagnostics(t, diagnostics)
	assertDiagnostic(t, diagnostics, SeverityWarning, AxisSelection, "description is recommended")
	if err := artifact.validate("oracle", target.TargetClaudeCode); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestOpenCodeRejectsStrictNameMismatchAndPattern(t *testing.T) {
	artifact := writeSkill(t, "oracle", "---\nname: Other Skill\ndescription: Review code.\n---\n")

	diagnostics := artifact.diagnostics("oracle", target.TargetOpenCode)

	assertDiagnostic(t, diagnostics, SeverityError, AxisIdentity, `name "Other Skill" must match`)
	assertDiagnostic(t, diagnostics, SeverityError, AxisIdentity, `must match skill name "oracle"`)
	if err := artifact.validate("oracle", target.TargetOpenCode); err == nil {
		t.Fatal("Validate returned nil error")
	}
}

func TestPiAllowsNameDirectoryMismatchAndWarnsForNonPortableName(t *testing.T) {
	artifact := writeSkill(t, "folder-name", "---\nname: Portable Skill\ndescription: Review code.\n---\n")

	diagnostics := artifact.diagnostics("folder-name", target.TargetPi)

	assertNoBlockingDiagnostics(t, diagnostics)
	assertDiagnostic(t, diagnostics, SeverityWarning, AxisIdentity, "does not match portable pattern")
	if err := artifact.validate("folder-name", target.TargetPi); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestPiWarnsButDoesNotBlockLengthViolations(t *testing.T) {
	longName := strings.Repeat("a", 65)
	longDescription := strings.Repeat("d", 1025)
	artifact := writeSkill(t, "folder-name", "---\nname: "+longName+"\ndescription: "+longDescription+"\n---\n")

	diagnostics := artifact.diagnostics("folder-name", target.TargetPi)

	assertNoBlockingDiagnostics(t, diagnostics)
	assertDiagnostic(t, diagnostics, SeverityWarning, AxisIdentity, "name exceeds 64 characters")
	assertDiagnostic(t, diagnostics, SeverityWarning, AxisSelection, "description exceeds 1024 characters")
	if err := artifact.validate("folder-name", target.TargetPi); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestOpenCodeBlocksLengthViolations(t *testing.T) {
	longName := strings.Repeat("a", 65)
	longDescription := strings.Repeat("d", 1025)
	artifact := writeSkill(t, "folder-name", "---\nname: "+longName+"\ndescription: "+longDescription+"\n---\n")

	diagnostics := artifact.diagnostics("folder-name", target.TargetOpenCode)

	assertDiagnostic(t, diagnostics, SeverityError, AxisIdentity, "name exceeds 64 characters")
	assertDiagnostic(t, diagnostics, SeverityError, AxisSelection, "description exceeds 1024 characters")
	if err := artifact.validate("folder-name", target.TargetOpenCode); err == nil {
		t.Fatal("Validate returned nil error")
	}
}

func TestUnknownAndTargetSpecificControlFields(t *testing.T) {
	artifact := writeSkill(t, "tool-control", "---\nname: tool-control\ndescription: Review code.\nallowed-tools:\n  - Bash(git status *)\ndisable-model-invocation: true\n---\n")

	opencodeDiagnostics := artifact.diagnostics("tool-control", target.TargetOpenCode)
	assertDiagnostic(t, opencodeDiagnostics, SeverityWarning, AxisControlField, `field "allowed-tools" is not recognized`)
	assertDiagnostic(t, opencodeDiagnostics, SeverityWarning, AxisControlField, `field "disable-model-invocation" is not recognized`)

	codexDiagnostics := artifact.diagnostics("tool-control", target.TargetCodex)
	assertNoDiagnostic(t, codexDiagnostics, `field "allowed-tools" is not recognized`)
	assertDiagnostic(t, codexDiagnostics, SeverityWarning, AxisControlField, `field "disable-model-invocation" is not recognized`)

	claudeDiagnostics := artifact.diagnostics("tool-control", target.TargetClaudeCode)
	assertNoDiagnostic(t, claudeDiagnostics, `field "disable-model-invocation" is not recognized`)

	piDiagnostics := artifact.diagnostics("tool-control", target.TargetPi)
	assertNoDiagnostic(t, piDiagnostics, `field "disable-model-invocation" is not recognized`)
}

func TestClaudeCodeRecognizesTargetSpecificControlFields(t *testing.T) {
	artifact := writeSkill(t, "tool-control", "---\nname: tool-control\ndescription: Review code.\nallowed-tools:\n  - Bash(git status *)\nmodel: opus\neffort: high\nhooks:\n  pre: echo ok\npaths:\n  - references/guide.md\n---\n")

	diagnostics := artifact.diagnostics("tool-control", target.TargetClaudeCode)

	assertNoDiagnostic(t, diagnostics, "is not recognized")
	assertNoBlockingDiagnostics(t, diagnostics)
	if err := artifact.validate("tool-control", target.TargetClaudeCode); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestUnsupportedControlFieldDiagnosticsAreSortedAndNonBlocking(t *testing.T) {
	artifact := writeSkill(t, "tool-control", "---\nname: tool-control\ndescription: Review code.\nzeta-field: true\nalpha-field: true\n---\n")

	diagnostics := artifact.diagnostics("tool-control", target.TargetOpenCode)

	var unknownMessages []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Axis == AxisControlField && diagnostic.Code == "unrecognized-frontmatter-field" {
			unknownMessages = append(unknownMessages, diagnostic.Message)
		}
	}
	if len(unknownMessages) != 2 {
		t.Fatalf("diagnostics = %#v, want two unknown field warnings", diagnostics)
	}
	if !strings.Contains(unknownMessages[0], `"alpha-field"`) || !strings.Contains(unknownMessages[1], `"zeta-field"`) {
		t.Fatalf("unknown messages = %#v, want sorted field diagnostics", unknownMessages)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Axis == AxisControlField && diagnostic.Blocking() {
			t.Fatalf("diagnostic = %#v, want unknown control fields non-blocking", diagnostic)
		}
	}
}

func TestStandardOptionalFieldsAreRecognizedAcrossProfiles(t *testing.T) {
	artifact := writeSkill(t, "portable-skill", "---\nname: portable-skill\ndescription: Review code.\nlicense: MIT\ncompatibility: Requires git.\nmetadata:\n  owner: platform\n---\n")

	for _, target := range []target.Target{
		target.TargetCodex,
		target.TargetClaudeCode,
		target.TargetOpenCode,
		target.TargetPi,
		target.TargetAntigravityCLI,
	} {
		t.Run(string(target), func(t *testing.T) {
			diagnostics := artifact.diagnostics("portable-skill", target)

			assertNoDiagnostic(t, diagnostics, "is not recognized")
			assertNoBlockingDiagnostics(t, diagnostics)
		})
	}
}

func TestInvalidFrontmatterIsReportedSeparatelyFromTargetRules(t *testing.T) {
	artifact := writeSkill(t, "oracle", "---\nname: 123\ndescription: Review code.\n---\n")

	diagnostics := artifact.diagnostics("oracle", target.TargetCodex)

	assertDiagnostic(t, diagnostics, SeverityError, AxisFrontmatter, `field "name" must be a string`)
	assertNoDiagnostic(t, diagnostics, "name is required")
}

func assertDiagnostic(
	t *testing.T,
	diagnostics []Diagnostic,
	severity Severity,
	axis Axis,
	message string,
) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == severity && diagnostic.Axis == axis && strings.Contains(diagnostic.Message, message) {
			return
		}
	}

	t.Fatalf("diagnostics = %#v, want severity=%q axis=%q containing %q", diagnostics, severity, axis, message)
}

func assertNoDiagnostic(t *testing.T, diagnostics []Diagnostic, message string) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, message) {
			t.Fatalf("diagnostics = %#v, want no diagnostic containing %q", diagnostics, message)
		}
	}
}

func assertNoBlockingDiagnostics(t *testing.T, diagnostics []Diagnostic) {
	t.Helper()

	for _, diagnostic := range diagnostics {
		if diagnostic.Blocking() {
			t.Fatalf("diagnostics = %#v, want no blocking diagnostics", diagnostics)
		}
	}
}

type testSkill struct {
	sourceID artifact.SourceID
	view     access.View
}

func (skill testSkill) diagnostics(installName string, selectedTarget target.Target) []Diagnostic {
	return Diagnostics(context.Background(), skill.view, skill.sourceID, installName, selectedTarget)
}

func (skill testSkill) validate(installName string, selectedTarget target.Target) error {
	return Validate(context.Background(), skill.view, skill.sourceID, installName, selectedTarget)
}

func writeSkill(t *testing.T, name string, content string) testSkill {
	t.Helper()

	tempDir := t.TempDir()
	skillPath := filepath.Join(tempDir, name)
	if err := os.MkdirAll(skillPath, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	view, err := access.OpenView(skillPath)
	if err != nil {
		t.Fatalf("OpenView returned error: %v", err)
	}
	return testSkill{
		sourceID: artifact.SourceID("local:skills/" + name + "?mode=vendor"),
		view:     view,
	}
}
