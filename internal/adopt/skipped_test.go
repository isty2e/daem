package adopt

import (
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestSkippedClassificationIsTotalAndFailVisible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reason     string
		category   SkipCategory
		actionHint SkipActionHint
	}{
		{name: "missing", reason: "missing", category: SkipCategoryInformational},
		{name: "empty instruction", reason: "empty_instruction_file", category: SkipCategoryInformational},
		{name: "empty hooks", reason: "hooks_empty", category: SkipCategoryInformational},
		{name: "duplicate skill", reason: "duplicate_skill_name", category: SkipCategoryInformational},
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
		{name: "unsupported projection", reason: "unsupported_mcp_projection", category: SkipCategoryUnsupported},
		{name: "stale adapter", reason: "stale_adapter_contract", category: SkipCategoryUnsupported},
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

func TestCandidateSetRejectsSkippedObservationWithoutRoute(t *testing.T) {
	t.Parallel()

	_, err := NewCandidateSet(CandidateSetInput{
		Skipped: []Skipped{{LivePath: "live", Reason: "missing"}},
	})
	if err == nil {
		t.Fatal("NewCandidateSet accepted a skipped observation without target and scope")
	}
}
