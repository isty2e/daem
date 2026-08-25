package adopt

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/target"
)

func TestSkippedSummaryContainsOnlyCompactAggregateCounts(t *testing.T) {
	t.Parallel()

	skipped := make([]adoptmodel.Skipped, 0, 1001)
	for index := range 1000 {
		skipped = append(skipped, adoptmodel.Skipped{
			Target:   target.TargetCodex,
			Scope:    target.ScopeProject,
			LivePath: fmt.Sprintf("missing-%04d", index),
			Reason:   "missing",
		})
	}
	skipped = append(skipped, adoptmodel.Skipped{
		Target:   target.TargetClaudeCode,
		Scope:    target.ScopeProject,
		LivePath: ".mcp.json#/mcpServers/secret",
		Reason:   "secret_literal_forbidden",
	})

	summary := skippedSummary(skipped)
	if !strings.Contains(summary, "action_required=1 unsupported=0 informational=1000") {
		t.Fatalf("summary = %q, want aggregate counts", summary)
	}
	for _, forbidden := range []string{"missing-0000", ".mcp.json", "secret_literal_forbidden"} {
		if strings.Contains(summary, forbidden) {
			t.Fatalf("summary = %q, want no detailed row %q", summary, forbidden)
		}
	}
	if len(summary) > 128 {
		t.Fatalf("summary = %q, want bounded aggregate output", summary)
	}
}

func TestNothingToImportErrorPreservesTypedSkippedEvidenceOutsideErrorText(t *testing.T) {
	t.Parallel()

	want := []adoptmodel.Skipped{{
		Target:   target.TargetClaudeCode,
		Scope:    target.ScopeProject,
		LivePath: ".mcp.json#/mcpServers/secret",
		Reason:   "secret_literal_forbidden",
		Detail:   "provider=claude-code",
	}}
	err := newNothingToImportError(nil, want)
	if !errors.Is(err, adoptmodel.ErrNothingToImport) {
		t.Fatalf("error = %v, want ErrNothingToImport", err)
	}
	if strings.Contains(err.Error(), want[0].LivePath) || strings.Contains(err.Error(), want[0].Detail) {
		t.Fatalf("error text = %q, want detailed rows kept out of Error()", err)
	}
	got, overflow := ImportFailureSkipped(err)
	if overflow || len(got) != 1 || got[0] != want[0] {
		t.Fatalf("typed skipped = %#v overflow=%t, want %#v/false", got, overflow, want)
	}
	got[0].Detail = "mutated"
	if again, againOverflow := ImportFailureSkipped(err); againOverflow || len(again) != 1 || again[0] != want[0] {
		t.Fatalf("typed skipped alias = %#v overflow=%t, want immutable copy %#v", again, againOverflow, want)
	}
}

func TestSkippedObservationErrorCarriesOnlyBoundedRetainedRows(t *testing.T) {
	t.Parallel()

	retained := []adoptmodel.Skipped{{
		Target:   target.TargetCodex,
		Scope:    target.ScopeProject,
		LivePath: "retained",
		Reason:   "missing",
	}}
	cause := fmt.Errorf("%w: rows observed=2 limit=1", adoptmodel.ErrSkipObservationLimitExceeded)
	err := newSkippedObservationError(cause, retained)
	if !errors.Is(err, adoptmodel.ErrSkipObservationLimitExceeded) {
		t.Fatalf("error = %v, want skip observation limit", err)
	}
	got, overflow := ImportFailureSkipped(err)
	if !overflow || len(got) != 1 || got[0] != retained[0] {
		t.Fatalf("typed skipped = %#v overflow=%t, want retained evidence", got, overflow)
	}
}
