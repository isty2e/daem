package repair

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestClassifyReportsMechanicalWithoutMutatingSource(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "skill.md", "---\ndescription: Demo skill\n---\n")

	identity, view := testArtifact(t, originalRoot)
	classification, err := Classify(
		context.Background(),
		identity,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if classification.Repairability() != RepairabilityMechanical {
		t.Fatalf("Repairability = %q, want mechanical", classification.Repairability())
	}
	if len(classification.Actions()) != 2 {
		t.Fatalf("actions = %#v, want rename and name insert", classification.Actions())
	}
	if directoryHasEntry(t, originalRoot, "SKILL.md") {
		t.Fatal("Classify mutated source by creating SKILL.md")
	}
	if !directoryHasEntry(t, originalRoot, "skill.md") {
		t.Fatal("Classify removed source skill.md")
	}
}

func TestClassifyReportsManualWhenMechanicalRepairIsInsufficient(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "skill.md", "---\nname: review\n---\n")

	identity, view := testArtifact(t, originalRoot)
	classification, err := Classify(
		context.Background(),
		identity,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if classification.Repairability() != RepairabilityManual {
		t.Fatalf("Repairability = %q, want manual", classification.Repairability())
	}
	if len(classification.Actions()) == 0 {
		t.Fatalf("actions = %#v, want partial mechanical actions before manual blocker", classification.Actions())
	}
	if reasons := classification.ManualReasons(); len(reasons) == 0 || !strings.Contains(strings.Join(reasons, "; "), "description is required") {
		t.Fatalf("manual reasons = %#v, want description guidance", reasons)
	}
	if directoryHasEntry(t, originalRoot, "SKILL.md") {
		t.Fatal("Classify mutated source by creating SKILL.md")
	}
}

func TestClassifyReportsManualWhenUppercaseSkillBlocksLowercaseRepair(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\nname: review\n---\n")
	writeTestFile(t, originalRoot, "skill.md", "---\nname: review\ndescription: Demo skill\n---\n")
	if !directoryHasEntry(t, originalRoot, "SKILL.md") || !directoryHasEntry(t, originalRoot, "skill.md") {
		t.Skip("filesystem does not support distinct SKILL.md and skill.md entries")
	}

	identity, view := testArtifact(t, originalRoot)
	classification, err := Classify(
		context.Background(),
		identity,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if classification.Repairability() != RepairabilityManual {
		t.Fatalf("Repairability = %q, want manual", classification.Repairability())
	}
	if strings.Contains(strings.Join(classification.Actions(), "; "), "rename file") {
		t.Fatalf("actions = %#v, want no unsafe casing rename across existing SKILL.md", classification.Actions())
	}
	if reasons := classification.ManualReasons(); len(reasons) == 0 || !strings.Contains(strings.Join(reasons, "; "), "description is required") {
		t.Fatalf("manual reasons = %#v, want uppercase SKILL.md description guidance", reasons)
	}
	if !directoryHasEntry(t, originalRoot, "SKILL.md") || !directoryHasEntry(t, originalRoot, "skill.md") {
		t.Fatalf("Classify mutated source entries in %q", originalRoot)
	}
}

func TestClassifyReportsMechanicalForCRLFDelimiterRepair(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "skill.md", " ---   \r\ndescription: Demo skill\r\n---\r\nBody\r\n")

	identity, view := testArtifact(t, originalRoot)
	classification, err := Classify(
		context.Background(),
		identity,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if classification.Repairability() != RepairabilityMechanical {
		t.Fatalf("Repairability = %q, want mechanical", classification.Repairability())
	}
	actions := strings.Join(classification.Actions(), "; ")
	if !strings.Contains(actions, "rename file: skill.md -> SKILL.md") {
		t.Fatalf("actions = %q, want casing repair", actions)
	}
	if !strings.Contains(actions, "normalize frontmatter delimiter") {
		t.Fatalf("actions = %q, want delimiter repair", actions)
	}
	if directoryHasEntry(t, originalRoot, "SKILL.md") {
		t.Fatal("Classify mutated source by creating SKILL.md")
	}
}

func TestClassifyReportsManualForComplexFrontmatterDescription(t *testing.T) {
	originalRoot := filepath.Join(t.TempDir(), "original")
	writeTestFile(t, originalRoot, "SKILL.md", "---\nname: review\ndescription:\n  - Demo skill\n---\n")

	identity, view := testArtifact(t, originalRoot)
	classification, err := Classify(
		context.Background(),
		identity,
		view,
		"review",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatalf("Classify returned error: %v", err)
	}
	if classification.Repairability() != RepairabilityManual {
		t.Fatalf("Repairability = %q, want manual", classification.Repairability())
	}
	if reasons := classification.ManualReasons(); len(reasons) == 0 || !strings.Contains(strings.Join(reasons, "; "), "description") {
		t.Fatalf("manual reasons = %#v, want description guidance", reasons)
	}
}
