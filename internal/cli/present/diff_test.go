package clipresent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestDryRunDiffReportFromPreservesTypedFactsAndCopiesMutableContent(t *testing.T) {
	entityID, err := entity.New(entity.KindInstructions, "project")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	targets := []target.Target{target.TargetCodex, target.TargetOpenCode}
	current := []byte("old\n")
	desired := []byte("new\n")

	got := DryRunDiffReportFrom(applyworkflow.DryRunDiffCollection{
		Diffs: []applyworkflow.DryRunDiff{{
			EntityID:       entityID,
			Targets:        targets,
			Scope:          target.ScopeProject,
			Destination:    "AGENTS.md",
			CurrentLabel:   "current/AGENTS.md",
			CurrentContent: current,
			DesiredLabel:   "desired/AGENTS.md",
			DesiredContent: desired,
		}},
		UninspectedManagedPathCount: 7,
	})
	if len(got.Diffs) != 1 ||
		got.UninspectedManagedPathCount != 7 ||
		got.Diffs[0].ResourceID != "instructions/project" ||
		!slices.Equal(got.Diffs[0].Targets, []string{"codex", "opencode"}) ||
		got.Diffs[0].Scope != "project" ||
		got.Diffs[0].Destination != "AGENTS.md" ||
		!slices.Equal(got.Diffs[0].CurrentContent, current) ||
		!slices.Equal(got.Diffs[0].DesiredContent, desired) {
		t.Fatalf("projected diff = %#v, want exact typed facts", got)
	}

	targets[0] = target.TargetClaudeCode
	current[0] = 'X'
	desired[0] = 'Y'
	if got.Diffs[0].Targets[0] != "codex" ||
		string(got.Diffs[0].CurrentContent) != "old\n" ||
		string(got.Diffs[0].DesiredContent) != "new\n" {
		t.Fatalf("projected diff retained workflow storage: %#v", got.Diffs[0])
	}
}

func TestPrintDryRunDiffsDoesNotInventPrimaryTargetForSharedPath(t *testing.T) {
	got := printDryRunDiffsForTest(t, DryRunDiffReport{Diffs: []DryRunDiff{{
		ResourceID:  "instructions/project",
		Targets:     []string{"codex", "opencode"},
		Scope:       "project",
		Destination: "AGENTS.md",
	}}})
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

func TestFormatTextDiffBoundsOneSidedDiff(t *testing.T) {
	desiredContent := []byte(strings.Repeat("desired\n", maxInlineDiffCells))

	lines := strings.Join(FormatTextDiff("current", nil, "desired", desiredContent), "\n")
	if !strings.Contains(lines, "inline diff omitted because the files are too large") {
		t.Fatalf("diff = %q, want explicit omission", lines)
	}
	if strings.Contains(lines, "+desired") {
		t.Fatalf("diff emitted oversized one-sided content")
	}
}

func TestFormatTextDiffBoundsInputBytesBeforeLineSplitting(t *testing.T) {
	desiredContent := []byte(strings.Repeat(
		"x",
		int(applyworkflow.MaximumDryRunDiffInputBytes)+1,
	))

	lines := strings.Join(FormatTextDiff("current", nil, "desired", desiredContent), "\n")
	if !strings.Contains(lines, "inline diff omitted because the files are too large") {
		t.Fatalf("diff = %q, want explicit omission", lines)
	}
}

func TestCanonicalDiffLineIDsCompareLongLinesByCanonicalIdentity(t *testing.T) {
	common := strings.Repeat("x", 4<<10)
	current := []string{common + "a\n", common + "b\n"}
	desired := []string{common + "a\n", common + "c\n"}

	currentIDs, desiredIDs := canonicalDiffLineIDs(current, desired)
	if currentIDs[0] != desiredIDs[0] {
		t.Fatalf("equal lines received different ids: %v and %v", currentIDs, desiredIDs)
	}
	if currentIDs[1] == desiredIDs[1] {
		t.Fatalf("distinct lines received equal ids: %v and %v", currentIDs, desiredIDs)
	}
}

func TestPrintDryRunDiffsReportsTypedCollectionOmission(t *testing.T) {
	got := printDryRunDiffsForTest(t, DryRunDiffReport{Diffs: []DryRunDiff{{
		ResourceID:     "instructions/project",
		Targets:        []string{"codex"},
		Scope:          "project",
		Destination:    "AGENTS.md",
		OmissionReason: applyworkflow.DryRunDiffOmittedOperationLimit,
	}}})
	if !strings.Contains(got, "operation input budget was exhausted") {
		t.Fatalf("output = %q, want operation-budget omission", got)
	}
}

func TestPrintDryRunDiffsReportsUninspectedDecisionCountOnce(t *testing.T) {
	got := printDryRunDiffsForTest(t, DryRunDiffReport{
		UninspectedManagedPathCount: 23,
	})
	if strings.Count(got, "operation item budget was exhausted") != 1 ||
		!strings.Contains(got, "omitted 23 managed-path decisions") {
		t.Fatalf("output = %q, want one aggregate item-budget omission", got)
	}
}

func TestPrintDryRunDiffsEscapesDynamicMetadataLabelsAndContent(t *testing.T) {
	got := printDryRunDiffsForTest(t, DryRunDiffReport{Diffs: []DryRunDiff{{
		ResourceID:     "instructions/project\n\x1b[2J",
		Targets:        []string{"codex\u202e"},
		Scope:          "project\n",
		Destination:    "AGENTS\x1b[2J.md",
		CurrentLabel:   "current\nforged",
		CurrentContent: []byte("old\x1b[2J\u202e\n"),
		DesiredLabel:   "desired\rforged",
		DesiredContent: []byte("new\tvalue\n"),
	}}})
	for _, want := range []string{
		`instructions/project\n\x1b[2J`,
		`codex\u202e`,
		`scope=project\n`,
		`AGENTS\x1b[2J.md`,
		`--- current\nforged`,
		`+++ desired\rforged`,
		`-old\x1b[2J\u202e`,
		`+new\tvalue`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	for _, reject := range []string{"\x1b", "\u202e"} {
		if strings.Contains(got, reject) {
			t.Fatalf("output contains raw display control %q: %q", reject, got)
		}
	}
}

func TestPrintDryRunDiffsBoundsAggregateCellWork(t *testing.T) {
	diff := DryRunDiff{
		ResourceID:     "instructions/project",
		Targets:        []string{"codex"},
		Scope:          "project",
		Destination:    "AGENTS.md",
		CurrentContent: []byte("old\n"),
		DesiredContent: []byte("new\n"),
	}
	var output bytes.Buffer
	if err := printDryRunDiffs(
		context.Background(),
		&output,
		DryRunDiffReport{Diffs: []DryRunDiff{diff, diff}},
		4,
	); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Count(got, "report work budget was exhausted") != 1 ||
		!strings.Contains(got, "diff rendering omitted 1 textual diffs") {
		t.Fatalf("output = %q, want one per-file and one aggregate work omission", got)
	}
}

func TestPrintDryRunDiffsObservesCancellationBetweenFiles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := PrintDryRunDiffs(ctx, io.Discard, DryRunDiffReport{Diffs: []DryRunDiff{{}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PrintDryRunDiffs error = %v, want cancellation", err)
	}
}

func printDryRunDiffsForTest(t testing.TB, report DryRunDiffReport) string {
	t.Helper()
	var output bytes.Buffer
	if err := PrintDryRunDiffs(context.Background(), &output, report); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
