package clipresent

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestDryRunDiffsFromPreservesTypedFactsAndCopiesMutableContent(t *testing.T) {
	entityID, err := entity.New(entity.KindInstructions, "project")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	targets := []target.Target{target.TargetCodex, target.TargetOpenCode}
	current := []byte("old\n")
	desired := []byte("new\n")

	got := DryRunDiffsFrom([]applyworkflow.DryRunDiff{{
		EntityID:       entityID,
		Targets:        targets,
		Scope:          target.ScopeProject,
		Destination:    "AGENTS.md",
		CurrentLabel:   "current/AGENTS.md",
		CurrentContent: current,
		DesiredLabel:   "desired/AGENTS.md",
		DesiredContent: desired,
	}})
	if len(got) != 1 ||
		got[0].ResourceID != "instructions/project" ||
		!slices.Equal(got[0].Targets, []string{"codex", "opencode"}) ||
		got[0].Scope != "project" ||
		got[0].Destination != "AGENTS.md" ||
		!slices.Equal(got[0].CurrentContent, current) ||
		!slices.Equal(got[0].DesiredContent, desired) {
		t.Fatalf("projected diff = %#v, want exact typed facts", got)
	}

	targets[0] = target.TargetClaudeCode
	current[0] = 'X'
	desired[0] = 'Y'
	if got[0].Targets[0] != "codex" ||
		string(got[0].CurrentContent) != "old\n" ||
		string(got[0].DesiredContent) != "new\n" {
		t.Fatalf("projected diff retained workflow storage: %#v", got[0])
	}
}

func TestPrintDryRunDiffsDoesNotInventPrimaryTargetForSharedPath(t *testing.T) {
	var output bytes.Buffer
	PrintDryRunDiffs(&output, []DryRunDiff{{
		ResourceID:  "instructions/project",
		Targets:     []string{"codex", "opencode"},
		Scope:       "project",
		Destination: "AGENTS.md",
	}})

	got := output.String()
	if !strings.Contains(got, `diff resource="instructions/project" targets=codex,opencode scope=project destination="AGENTS.md"`) {
		t.Fatalf("output = %q, want shared target attribution", got)
	}
	if strings.Contains(got, " target=codex ") {
		t.Fatalf("output = %q, invented primary target", got)
	}
}

func TestFormatTextDiffOmitsHugeInlineDiff(t *testing.T) {
	currentContent := []byte(strings.Repeat("current\n", 600))
	desiredContent := []byte(strings.Repeat("desired\n", 600))

	lines := strings.Join(FormatTextDiff("current/AGENTS.md", currentContent, "desired/AGENTS.md", desiredContent), "\n")
	for _, want := range []string{
		"--- current/AGENTS.md",
		"+++ desired/AGENTS.md",
		"text content differs; inline diff omitted because the files are too large",
	} {
		if !strings.Contains(lines, want) {
			t.Fatalf("diff = %q, want %q", lines, want)
		}
	}
	for _, reject := range []string{
		"-current",
		"+desired",
	} {
		if strings.Contains(lines, reject) {
			t.Fatalf("diff = %q, did not want %q", lines, reject)
		}
	}
}
