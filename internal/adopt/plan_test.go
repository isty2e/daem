package adopt

import (
	"bytes"
	"path/filepath"
	"testing"

	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestPlanOwnsBytesCollectionsAndIdentityDisclosure(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(
		[]targetpkg.Target{targetpkg.TargetCodex},
		[]targetpkg.Scope{targetpkg.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	sourceContent := []byte("instructions\n")
	candidates, err := NewCandidateSet([]Source{{
		ResourceName: "instructions",
		Target:       targetpkg.TargetCodex,
		Scope:        targetpkg.ScopeProject,
		LivePath:     "AGENTS.md",
		SourcePath:   filepath.Join(root, "daem.d", "instructions.md"),
		Content:      sourceContent,
	}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	manifestContent := []byte("version = 1\n")
	plan, err := NewPlan(request, nil, manifestContent, candidates, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := plan.IdentityBytes()
	if err != nil {
		t.Fatal(err)
	}

	manifestContent[0] = 'X'
	sourceContent[0] = 'X'
	disclosedManifest := plan.ManifestContent()
	disclosedSources := plan.Sources()
	disclosedManifest[0] = 'Y'
	disclosedSources[0].Content[0] = 'Y'
	afterMutation, err := plan.IdentityBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(identity, afterMutation) {
		t.Fatalf("plan identity changed through caller aliases")
	}
	if string(plan.ManifestContent()) != "version = 1\n" || string(plan.Sources()[0].Content) != "instructions\n" {
		t.Fatalf("plan facts changed through caller aliases")
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("plan became invalid: %v", err)
	}
}

func TestMergePlanOwnsOriginalBytesAndResults(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(
		[]targetpkg.Target{targetpkg.TargetCodex},
		[]targetpkg.Scope{targetpkg.ScopeProject},
		output,
		sourceDirectory,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := NewCandidateSet(nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("version = 1\n")
	results := []MergeResult{{Resource: "instructions/existing", Status: MergeStatusNoop, Detail: "already present"}}
	plan, err := NewPlan(request, original, original, candidates, results)
	if err != nil {
		t.Fatal(err)
	}
	original[0] = 'X'
	results[0].Detail = "changed"
	disclosedOriginal := plan.OriginalContent()
	disclosedResults := plan.MergeResults()
	disclosedOriginal[0] = 'Y'
	disclosedResults[0].Detail = "changed again"
	if string(plan.OriginalContent()) != "version = 1\n" || plan.MergeResults()[0].Detail != "already present" {
		t.Fatalf("merge plan changed through caller aliases")
	}
}

func TestPlanRejectsPartialAndOutOfSelectionFacts(t *testing.T) {
	if err := (Plan{}).Validate(); err == nil {
		t.Fatal("zero Plan validated")
	}

	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(
		[]targetpkg.Target{targetpkg.TargetCodex},
		[]targetpkg.Scope{targetpkg.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := NewCandidateSet([]Source{{
		ResourceName: "other-target",
		Target:       targetpkg.TargetClaudeCode,
		Scope:        targetpkg.ScopeProject,
		LivePath:     "CLAUDE.md",
		SourcePath:   filepath.Join(root, "daem.d", "instructions.md"),
		Content:      []byte("instructions\n"),
	}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPlan(request, nil, []byte("version = 1\n"), candidates, nil); err == nil {
		t.Fatal("plan accepted a candidate outside the request target selection")
	}
}
