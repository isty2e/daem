package adopt

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	targetpkg "github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
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
	candidates, err := NewCandidateSet(CandidateSetInput{
		Sources: []Source{{
			ResourceName: "instructions",
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeProject,
			LivePath:     "AGENTS.md",
			SourcePath:   filepath.Join(root, "daem.d", "instructions.md"),
			Content:      sourceContent,
		}},
	})
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

func TestPlanIdentityIncludesEverySkillSourceRoute(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(
		[]targetpkg.Target{targetpkg.TargetCodex, targetpkg.TargetClaudeCode},
		[]targetpkg.Scope{targetpkg.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	newPlan := func(claudePath string) Plan {
		t.Helper()
		skillPath := filepath.Join(sourceDirectory.Root(), "skills", "review")
		candidates, candidateErr := NewCandidateSet(CandidateSetInput{Skills: []Skill{{
			ResourceName: "review",
			InstallName:  "review",
			Target:       targetpkg.TargetCodex,
			Targets:      []targetpkg.Target{targetpkg.TargetCodex, targetpkg.TargetClaudeCode},
			Scope:        targetpkg.ScopeProject,
			SourceRoutes: []SkillSourceRoute{
				{Target: targetpkg.TargetClaudeCode, LivePath: claudePath, ReadPath: claudePath},
				{Target: targetpkg.TargetCodex, LivePath: "/codex/skills/review", ReadPath: "/codex/skills/review"},
			},
			SourcePath:  skillPath,
			ContentHash: artifact.HashFileContent([]byte("review")),
		}}})
		if candidateErr != nil {
			t.Fatal(candidateErr)
		}
		plan, planErr := NewPlan(request, nil, []byte("version = 1\n"), candidates, nil)
		if planErr != nil {
			t.Fatal(planErr)
		}
		return plan
	}

	before, err := newPlan("/claude/skills/review").IdentityBytes()
	if err != nil {
		t.Fatal(err)
	}
	after, err := newPlan("/claude/skills/other").IdentityBytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) {
		t.Fatal("plan identity ignored non-primary skill source route")
	}
}

func TestPlanIdentityIncludesNonwritableSkillSourceAuthority(t *testing.T) {
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
	newPlan := func(readPath string) Plan {
		t.Helper()
		candidates, candidateErr := NewCandidateSet(CandidateSetInput{
			SkillSourceAuthorities: []SkillSourceAuthority{{
				ResourceName: "review",
				Scope:        targetpkg.ScopeProject,
				ContentHash:  artifact.HashFileContent([]byte("review")),
				Routes: []SkillSourceRoute{{
					Target: targetpkg.TargetCodex, LivePath: "/host/skills/review", ReadPath: readPath,
				}},
			}},
		})
		if candidateErr != nil {
			t.Fatal(candidateErr)
		}
		plan, planErr := NewPlan(
			request,
			[]byte("version = 1\n"),
			[]byte("version = 1\n"),
			candidates,
			[]MergeResult{{Resource: "skill/review", Status: MergeStatusNoop, Detail: "already declared"}},
		)
		if planErr != nil {
			t.Fatal(planErr)
		}
		return plan
	}

	before, err := newPlan("/resolved/skills/review").IdentityBytes()
	if err != nil {
		t.Fatal(err)
	}
	after, err := newPlan("/resolved/skills/other").IdentityBytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) {
		t.Fatal("plan identity ignored nonwritable skill source authority")
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
	candidates, err := NewCandidateSet(CandidateSetInput{})
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("version = 1\n")
	subject, err := topology.ParseSubjectID("projection/codex.project.mcp-server/context7")
	if err != nil {
		t.Fatal(err)
	}
	results := []MergeResult{{
		Resource: "mcp_server/context7",
		Subject:  subject,
		Status:   MergeStatusNoop,
		Detail:   "already present",
	}}
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
	identity, err := plan.IdentityBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(identity), subject.String()) {
		t.Fatalf("plan identity = %s, want canonical merge subject %q", identity, subject.String())
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
	candidates, err := NewCandidateSet(CandidateSetInput{
		Sources: []Source{{
			ResourceName: "other-target",
			Target:       targetpkg.TargetClaudeCode,
			Scope:        targetpkg.ScopeProject,
			LivePath:     "CLAUDE.md",
			SourcePath:   filepath.Join(root, "daem.d", "instructions.md"),
			Content:      []byte("instructions\n"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPlan(request, nil, []byte("version = 1\n"), candidates, nil); err == nil {
		t.Fatal("plan accepted a candidate outside the request target selection")
	}
}
