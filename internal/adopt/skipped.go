package adopt

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/target"
)

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
)

// Category returns the stable user-response class for the skipped reason.
func (skipped Skipped) Category() SkipCategory {
	switch skipped.Reason {
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
		"stale_adapter_contract":
		return SkipCategoryUnsupported
	default:
		return SkipCategoryActionRequired
	}
}

// ActionHint returns a stable next-action family for actionable skipped rows.
func (skipped Skipped) ActionHint() SkipActionHint {
	if skipped.Category() != SkipCategoryActionRequired {
		return ""
	}

	switch skipped.Reason {
	case "secret_literal_forbidden":
		return SkipActionUseSymbolicEnvironment
	case "source_not_importable", "source_provenance_unrecoverable":
		return SkipActionAuthorExplicitSource
	}
	if strings.Contains(skipped.Reason, "changed_during_read") {
		return SkipActionRetryWhenStable
	}
	if strings.Contains(skipped.Reason, "too_large") ||
		strings.Contains(skipped.Reason, "depth_exceeded") ||
		strings.Contains(skipped.Reason, "budget_exceeded") ||
		strings.Contains(skipped.Reason, "structure_limit") {
		return SkipActionReduceSource
	}
	if strings.Contains(skipped.Reason, "not_regular") ||
		strings.Contains(skipped.Reason, "not_directory") ||
		strings.Contains(skipped.Reason, "final_symlink") ||
		skipped.Reason == "nested_symlink" {
		return SkipActionReplaceUnsupportedEntry
	}
	if strings.Contains(skipped.Reason, "malformed") ||
		strings.Contains(skipped.Reason, "invalid_") ||
		strings.Contains(skipped.Reason, "duplicate_") ||
		strings.Contains(skipped.Reason, "equivalence_undefined") ||
		skipped.Reason == "missing_skill_md" {
		return SkipActionRepairSource
	}
	return SkipActionReviewSource
}

func (skipped Skipped) validate() error {
	if _, err := target.ParseTarget(string(skipped.Target)); err != nil {
		return fmt.Errorf("target: %w", err)
	}
	if _, err := target.ParseScope(string(skipped.Scope)); err != nil {
		return fmt.Errorf("scope: %w", err)
	}
	if strings.TrimSpace(skipped.LivePath) == "" || strings.TrimSpace(skipped.Reason) == "" {
		return fmt.Errorf("live path and reason are required")
	}
	if skipped.Category() == SkipCategoryActionRequired && skipped.ActionHint() == "" {
		return fmt.Errorf("actionable skip requires an action hint")
	}
	return nil
}
