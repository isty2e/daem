package adopt

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestSkippedClassificationIsTotalAndFailVisible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reason     SkipReason
		category   SkipCategory
		actionHint SkipActionHint
	}{
		{name: "missing", reason: "missing", category: SkipCategoryInformational},
		{name: "empty instruction", reason: "empty_instruction_file", category: SkipCategoryInformational},
		{name: "empty hooks", reason: "hooks_empty", category: SkipCategoryInformational},
		{name: "duplicate skill", reason: "duplicate_skill_name", category: SkipCategoryInformational},
		{name: "conflicting skill", reason: "conflicting_skill_name", category: SkipCategoryActionRequired, actionHint: SkipActionResolveConflict},
		{name: "supplied skill root", reason: "supplied_skill_root", category: SkipCategoryInformational},
		{name: "supplied skill entry", reason: "supplied_skill_entry", category: SkipCategoryInformational},
		{name: "supplied plugin cache skill", reason: "supplied_plugin_cache_skill", category: SkipCategoryInformational},
		{name: "classify only", reason: "instruction_classify_only", category: SkipCategoryInformational},
		{name: "unsupported hooks surface", reason: "unsupported_hooks_surface", category: SkipCategoryUnsupported},
		{name: "unsupported MCP surface", reason: "unsupported_mcp_server_surface", category: SkipCategoryUnsupported},
		{name: "not implemented", reason: "instruction_import_not_implemented", category: SkipCategoryUnsupported},
		{name: "unsupported inline hooks", reason: "unsupported_inline_hooks", category: SkipCategoryUnsupported},
		{name: "unsupported alternate config", reason: "unsupported_mcp_alternate_config", category: SkipCategoryUnsupported},
		{name: "unsupported transport", reason: "unsupported_mcp_transport", category: SkipCategoryUnsupported},
		{name: "unsupported managed field", reason: "unsupported_mcp_managed_field", category: SkipCategoryUnsupported},
		{name: "legacy unsupported projection", reason: "unsupported_mcp_projection", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "invalid canonical MCP", reason: "invalid_canonical_mcp", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "lossy provider MCP document", reason: "mcp_provider_document_lossy", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "unclassified MCP projection", reason: "mcp_projection_unclassified", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "unsupported hook group field", reason: "unsupported_group_field", category: SkipCategoryUnsupported},
		{name: "unsupported hook handler field", reason: "unsupported_handler_field", category: SkipCategoryUnsupported},
		{name: "unsupported hook handler type", reason: "unsupported_handler_type", category: SkipCategoryUnsupported},
		{name: "unsupported async hook", reason: "unsupported_async", category: SkipCategoryUnsupported},
		{name: "unsupported hook condition", reason: "unsupported_condition", category: SkipCategoryUnsupported},
		{name: "unsupported target hook shape", reason: "unsupported_target_shape", category: SkipCategoryUnsupported},
		{name: "stale adapter", reason: "stale_adapter_contract", category: SkipCategoryUnsupported},
		{name: "empty hook event", reason: "empty_event", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "hook groups not array", reason: "groups_not_array", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "malformed hook group", reason: "malformed_group", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "missing hook handlers", reason: "missing_handlers", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "malformed hook handler", reason: "malformed_handler", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "missing hook command", reason: "missing_command", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "empty hook JSON", reason: "empty_json", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "malformed hook JSON", reason: "malformed_json", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "multiple hook JSON values", reason: "multiple_json_values", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "hook top level not object", reason: "top_level_not_object", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "hooks missing", reason: "hooks_missing", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "hooks null", reason: "hooks_null", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "hooks not object", reason: "hooks_not_object", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "literal secret", reason: "secret_literal_forbidden", category: SkipCategoryActionRequired, actionHint: SkipActionUseSymbolicEnvironment},
		{name: "source not importable", reason: "source_not_importable", category: SkipCategoryActionRequired, actionHint: SkipActionAuthorExplicitSource},
		{name: "lost provenance", reason: "source_provenance_unrecoverable", category: SkipCategoryActionRequired, actionHint: SkipActionAuthorExplicitSource},
		{name: "instruction changed", reason: "instruction_file_changed_during_read", category: SkipCategoryActionRequired, actionHint: SkipActionRetryWhenStable},
		{name: "hook changed", reason: "hook_file_changed_during_read", category: SkipCategoryActionRequired, actionHint: SkipActionRetryWhenStable},
		{name: "MCP changed", reason: "mcp_config_changed_during_read", category: SkipCategoryActionRequired, actionHint: SkipActionRetryWhenStable},
		{name: "instruction too large", reason: "instruction_file_too_large", category: SkipCategoryActionRequired, actionHint: SkipActionReduceSource},
		{name: "hook too large", reason: "hook_file_too_large", category: SkipCategoryActionRequired, actionHint: SkipActionReduceSource},
		{name: "MCP too large", reason: "mcp_config_too_large", category: SkipCategoryActionRequired, actionHint: SkipActionReduceSource},
		{name: "JSON depth", reason: "json_depth_exceeded", category: SkipCategoryActionRequired, actionHint: SkipActionReduceSource},
		{name: "hook budget", reason: "hook_import_budget_exceeded", category: SkipCategoryActionRequired, actionHint: SkipActionReduceSource},
		{name: "inline structure", reason: "inline_config_structure_limit", category: SkipCategoryActionRequired, actionHint: SkipActionReduceSource},
		{name: "instruction symlink", reason: "instruction_final_symlink", category: SkipCategoryActionRequired, actionHint: SkipActionReplaceUnsupportedEntry},
		{name: "hook symlink", reason: "hook_final_symlink", category: SkipCategoryActionRequired, actionHint: SkipActionReplaceUnsupportedEntry},
		{name: "MCP symlink", reason: "mcp_config_final_symlink", category: SkipCategoryActionRequired, actionHint: SkipActionReplaceUnsupportedEntry},
		{name: "instruction not regular", reason: "instruction_not_regular_file", category: SkipCategoryActionRequired, actionHint: SkipActionReplaceUnsupportedEntry},
		{name: "generic not regular", reason: "not_regular_file", category: SkipCategoryActionRequired, actionHint: SkipActionReplaceUnsupportedEntry},
		{name: "skill not directory", reason: "skill_not_directory", category: SkipCategoryActionRequired, actionHint: SkipActionReplaceUnsupportedEntry},
		{name: "nested symlink", reason: "nested_symlink", category: SkipCategoryActionRequired, actionHint: SkipActionReplaceUnsupportedEntry},
		{name: "malformed MCP", reason: "mcp_config_malformed", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "malformed inline config", reason: "inline_config_malformed", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "duplicate JSON key", reason: "duplicate_json_key", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "invalid canonical hook", reason: "invalid_canonical_hook", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "invalid MCP argument", reason: "invalid_mcp_argument", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "invalid skill name", reason: "invalid_skill_name", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "missing SKILL.md", reason: "missing_skill_md", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "projection equivalence", reason: "projection_equivalence_undefined", category: SkipCategoryActionRequired, actionHint: SkipActionRepairSource},
		{name: "inline config unreadable", reason: "inline_config_unreadable", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "policy skip", reason: "instruction_import_skipped_by_policy", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "unknown future reason", reason: "future_reason", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
		{name: "unknown unsupported-like reason", reason: "unsupported_future_surface", category: SkipCategoryActionRequired, actionHint: SkipActionReviewSource},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			skipped := Skipped{
				Target:   target.TargetCodex,
				Scope:    target.ScopeProject,
				LivePath: "live",
				Reason:   test.reason,
			}
			if got := skipped.Category(); got != test.category {
				t.Fatalf("Category() = %q, want %q", got, test.category)
			}
			if got := skipped.ActionHint(); got != test.actionHint {
				t.Fatalf("ActionHint() = %q, want %q", got, test.actionHint)
			}
		})
	}
}

func TestSkippedCollectorBoundsRowsDiagnosticBytesAndDetail(t *testing.T) {
	t.Parallel()

	collector := NewSkippedCollector()
	row := Skipped{
		Target:   target.TargetCodex,
		Scope:    target.ScopeProject,
		LivePath: "live",
		Reason:   "missing",
	}
	for range maximumSkippedObservations {
		if err := collector.Add(row); err != nil {
			t.Fatalf("exact row budget returned error: %v", err)
		}
	}
	if err := collector.Add(row); !errors.Is(err, ErrSkipObservationLimitExceeded) {
		t.Fatalf("limit+1 error = %v, want skip observation exhaustion", err)
	}

	collector = NewSkippedCollector()
	oversizedDetail := "conflicts_with=" + strings.Repeat("x", maximumSkippedDetailBytes)
	if err := collector.Add(Skipped{
		Target:   target.TargetCodex,
		Scope:    target.ScopeGlobal,
		LivePath: "duplicate",
		Reason:   "conflicting_skill_name",
		Detail:   oversizedDetail,
	}); err != nil {
		t.Fatalf("bounded detail returned error: %v", err)
	}
	bounded := collector.Skipped()
	if len(bounded) != 1 || len(bounded[0].Detail) > maximumSkippedDetailBytes ||
		strings.Contains(bounded[0].Detail, strings.Repeat("x", 32)) ||
		!strings.Contains(bounded[0].Detail, "detail_omitted:sha256:") {
		t.Fatalf("bounded detail = %#v", bounded)
	}

	collector = NewSkippedCollector()
	if err := collector.Add(Skipped{
		Target:   target.TargetCodex,
		Scope:    target.ScopeProject,
		LivePath: strings.Repeat("p", maximumSkippedDiagnosticBytes),
		Reason:   "missing",
	}); !errors.Is(err, ErrSkipObservationLimitExceeded) {
		t.Fatalf("diagnostic-byte error = %v, want skip observation exhaustion", err)
	}
	if got := collector.Skipped(); len(got) != 0 {
		t.Fatalf("over-limit row retained: %#v", got)
	}
}

func TestCandidateSetRejectsSkippedObservationBudgetBypass(t *testing.T) {
	t.Parallel()

	base := Skipped{
		Target:   target.TargetCodex,
		Scope:    target.ScopeProject,
		LivePath: "live",
		Reason:   "missing",
	}
	tooMany := make([]Skipped, maximumSkippedObservations+1)
	for index := range tooMany {
		tooMany[index] = base
	}
	if _, err := NewCandidateSet(CandidateSetInput{Skipped: tooMany}); err == nil {
		t.Fatal("NewCandidateSet accepted too many skipped observations")
	}

	oversizedDetail := base
	oversizedDetail.Detail = strings.Repeat("d", maximumSkippedDetailBytes+1)
	if _, err := NewCandidateSet(CandidateSetInput{Skipped: []Skipped{oversizedDetail}}); err == nil {
		t.Fatal("NewCandidateSet accepted an oversized skipped detail")
	}
}

func TestCandidateSetRejectsSkippedObservationWithoutRoute(t *testing.T) {
	t.Parallel()

	_, err := NewCandidateSet(CandidateSetInput{
		Skipped: []Skipped{{LivePath: "live", Reason: "missing"}},
	})
	if err == nil {
		t.Fatal("NewCandidateSet accepted a skipped observation without target and scope")
	}
}
