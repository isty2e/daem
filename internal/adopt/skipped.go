package adopt

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/target"
)

// SkipReason identifies one stable semantic cause for a skipped observation.
// Diagnostic context belongs in Skipped.Detail and must not affect classification.
type SkipReason string

// SkipCategory identifies the user-response class of one skipped observation.
type SkipCategory string

const (
	SkipCategoryActionRequired SkipCategory = "action_required"
	SkipCategoryUnsupported    SkipCategory = "unsupported"
	SkipCategoryInformational  SkipCategory = "informational"
)

// SkipActionHint identifies one stable next-action family for an actionable skip.
type SkipActionHint string

const (
	SkipActionReviewSource            SkipActionHint = "review_source"
	SkipActionRepairSource            SkipActionHint = "repair_source"
	SkipActionRetryWhenStable         SkipActionHint = "retry_when_stable"
	SkipActionReduceSource            SkipActionHint = "reduce_source"
	SkipActionReplaceUnsupportedEntry SkipActionHint = "replace_unsupported_entry"
	SkipActionAuthorExplicitSource    SkipActionHint = "author_explicit_source"
	SkipActionUseSymbolicEnvironment  SkipActionHint = "use_symbolic_environment_reference"
	SkipActionResolveConflict         SkipActionHint = "resolve_conflict"
)

// Category returns the stable user-response class for the skipped reason.
func (skipped Skipped) Category() SkipCategory {
	return skipped.Reason.category()
}

func (reason SkipReason) category() SkipCategory {
	switch reason {
	case "missing",
		"empty_instruction_file",
		"hooks_empty",
		"duplicate_skill_name",
		"supplied_skill_root",
		"supplied_skill_entry",
		"supplied_plugin_cache_skill",
		"instruction_classify_only":
		return SkipCategoryInformational
	case "unsupported_hooks_surface",
		"unsupported_mcp_server_surface",
		"instruction_import_not_implemented",
		"unsupported_inline_hooks",
		"unsupported_mcp_alternate_config",
		"unsupported_mcp_transport",
		"unsupported_mcp_managed_field",
		"unsupported_mcp_projection",
		"unsupported_group_field",
		"unsupported_handler_field",
		"unsupported_handler_type",
		"unsupported_async",
		"unsupported_condition",
		"unsupported_target_shape",
		"stale_adapter_contract":
		return SkipCategoryUnsupported
	default:
		return SkipCategoryActionRequired
	}
}

// ActionHint returns a stable next-action family for actionable skipped rows.
func (skipped Skipped) ActionHint() SkipActionHint {
	return skipped.Reason.actionHint()
}

func (reason SkipReason) actionHint() SkipActionHint {
	if reason.category() != SkipCategoryActionRequired {
		return ""
	}

	switch reason {
	case "secret_literal_forbidden":
		return SkipActionUseSymbolicEnvironment
	case "source_not_importable", "source_provenance_unrecoverable":
		return SkipActionAuthorExplicitSource
	case "conflicting_skill_name":
		return SkipActionResolveConflict
	case "instruction_file_changed_during_read",
		"hook_file_changed_during_read",
		"mcp_config_changed_during_read":
		return SkipActionRetryWhenStable
	case "instruction_file_too_large",
		"hook_file_too_large",
		"mcp_config_too_large",
		"json_depth_exceeded",
		"hook_import_budget_exceeded",
		"inline_config_structure_limit":
		return SkipActionReduceSource
	case "instruction_not_regular_file",
		"not_regular_file",
		"skill_not_directory",
		"instruction_final_symlink",
		"hook_final_symlink",
		"mcp_config_final_symlink",
		"nested_symlink":
		return SkipActionReplaceUnsupportedEntry
	case "mcp_config_malformed",
		"inline_config_malformed",
		"duplicate_json_key",
		"invalid_canonical_hook",
		"invalid_mcp_argument",
		"invalid_skill_name",
		"missing_skill_md",
		"projection_equivalence_undefined",
		"malformed_json",
		"malformed_group",
		"malformed_handler":
		return SkipActionRepairSource
	default:
		return SkipActionReviewSource
	}
}

func (skipped Skipped) validate() error {
	if _, err := target.ParseTarget(string(skipped.Target)); err != nil {
		return fmt.Errorf("target: %w", err)
	}
	if _, err := target.ParseScope(string(skipped.Scope)); err != nil {
		return fmt.Errorf("scope: %w", err)
	}
	if strings.TrimSpace(skipped.LivePath) == "" || strings.TrimSpace(string(skipped.Reason)) == "" {
		return fmt.Errorf("live path and reason are required")
	}
	if skipped.Category() == SkipCategoryActionRequired && skipped.ActionHint() == "" {
		return fmt.Errorf("actionable skip requires an action hint")
	}
	return nil
}
