package authoring

import (
	"path/filepath"
	"strings"
	"testing"

	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
)

func TestSkillGroupWarningsClassifyLocalSourceProblems(t *testing.T) {
	tempDir := t.TempDir()
	writeTestFile(t, filepath.Join(tempDir, "skills", "oracle"), "SKILL.md", "---\nname: oracle\ndescription: Oracle\n---\n")

	if warnings := skillGroupWarnings(declarationcodec.SkillGroup{
		Names:  []string{"oracle"},
		Source: declarationcodec.SkillSource{Path: "skills", Mode: "vendor"},
	}, tempDir); len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}

	warnings := skillGroupWarnings(declarationcodec.SkillGroup{
		Names:  []string{"missing"},
		Source: declarationcodec.SkillSource{Path: "missing", Mode: "vendor"},
	}, tempDir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "does not exist yet") {
		t.Fatalf("warnings = %#v, want missing source warning", warnings)
	}
}

func TestInstructionWarningsClassifyLocalSourceProblems(t *testing.T) {
	tempDir := t.TempDir()
	writeTestFile(t, filepath.Join(tempDir, "instructions"), "project.md", "Project guidance.\n")
	if warnings := instructionWarnings(declarationcodec.Instruction{
		Source: declarationcodec.InstructionSource{Path: "instructions/project.md", Mode: "vendor"},
	}, tempDir); len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}

	directoryWarnings := instructionWarnings(declarationcodec.Instruction{
		Source: declarationcodec.InstructionSource{Path: "instructions", Mode: "vendor"},
	}, tempDir)
	if len(directoryWarnings) != 1 || !strings.Contains(directoryWarnings[0], "is a directory") {
		t.Fatalf("warnings = %#v, want directory warning", directoryWarnings)
	}

	missingWarnings := instructionWarnings(declarationcodec.Instruction{
		Source: declarationcodec.InstructionSource{Path: "missing.md", Mode: "vendor"},
	}, tempDir)
	if len(missingWarnings) != 1 || !strings.Contains(missingWarnings[0], "does not exist yet") {
		t.Fatalf("warnings = %#v, want missing warning", missingWarnings)
	}
}
