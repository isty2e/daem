package adopt

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/target"
)

func TestSkippedSummaryCompactsBenignRowsAndRetainsActionablePaths(t *testing.T) {
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
	for _, expected := range []string{
		"action_required=1 unsupported=0 informational=1000",
		".mcp.json#/mcpServers/secret: secret_literal_forbidden",
		"informational target=codex reason=missing count=1000",
	} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("summary = %q, want %q", summary, expected)
		}
	}
	if strings.Contains(summary, "missing-0000") || len(summary) > 512 {
		t.Fatalf("summary = %q, want bounded benign output", summary)
	}
}

func TestNothingToImportErrorPreservesTypedSkippedEvidence(t *testing.T) {
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
	got := NothingToImportSkipped(err)
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("typed skipped = %#v, want %#v", got, want)
	}
	got[0].Detail = "mutated"
	if again := NothingToImportSkipped(err); len(again) != 1 || again[0] != want[0] {
		t.Fatalf("typed skipped alias = %#v, want immutable copy %#v", again, want)
	}
}
