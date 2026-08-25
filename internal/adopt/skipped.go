package adopt

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/target"
)

const (
	maximumSkippedObservations    = 4096
	maximumSkippedDiagnosticBytes = 256 << 10
	maximumSkippedDetailBytes     = 4096
)

// ErrSkipObservationLimitExceeded classifies operation-wide skipped-result
// count or diagnostic-byte exhaustion.
var ErrSkipObservationLimitExceeded = errors.New("import skip observation limit exceeded")

// SkippedCollector admits one bounded operation-wide set of exact skipped
// observations. It is sequential and must not be shared by concurrent callers.
type SkippedCollector struct {
	skipped         []Skipped
	diagnosticBytes int
	exhausted       bool
}

// NewSkippedCollector constructs an empty operation-wide skip collector.
func NewSkippedCollector() *SkippedCollector {
	return &SkippedCollector{skipped: make([]Skipped, 0)}
}

// Add normalizes and admits one exact skipped observation.
func (collector *SkippedCollector) Add(skipped Skipped) error {
	if collector == nil {
		return fmt.Errorf("skipped observation collector is required")
	}
	if collector.exhausted {
		return ErrSkipObservationLimitExceeded
	}
	skipped.Detail = boundedSkippedDetail(skipped.Detail)
	if err := skipped.validate(); err != nil {
		return err
	}
	remainingBytes := maximumSkippedDiagnosticBytes - collector.diagnosticBytes
	rowBytes, withinBudget := skippedDiagnosticBytesWithin(skipped, remainingBytes)
	if len(collector.skipped) >= maximumSkippedObservations {
		collector.exhausted = true
		return fmt.Errorf(
			"%w: rows observed=%d limit=%d",
			ErrSkipObservationLimitExceeded,
			len(collector.skipped)+1,
			maximumSkippedObservations,
		)
	}
	if !withinBudget {
		collector.exhausted = true
		return fmt.Errorf(
			"%w: diagnostic_bytes observed>%d limit=%d",
			ErrSkipObservationLimitExceeded,
			maximumSkippedDiagnosticBytes,
			maximumSkippedDiagnosticBytes,
		)
	}
	collector.skipped = append(collector.skipped, skipped)
	collector.diagnosticBytes += rowBytes
	return nil
}

// Skipped returns an owned copy of every admitted exact skipped observation.
func (collector *SkippedCollector) Skipped() []Skipped {
	if collector == nil {
		return nil
	}
	return cloneSkipped(collector.skipped)
}

func boundedSkippedDetail(detail string) string {
	if len(detail) <= maximumSkippedDetailBytes {
		return detail
	}
	digest := sha256.Sum256([]byte(detail))
	return fmt.Sprintf("detail_omitted:sha256:%x:bytes=%d", digest, len(detail))
}

func skippedDiagnosticBytesWithin(skipped Skipped, maximum int) (int, bool) {
	if maximum < 0 {
		return 0, false
	}
	total := 0
	for _, length := range []int{
		len(skipped.Target),
		len(skipped.Scope),
		len(skipped.LivePath),
		len(skipped.Reason),
		len(skipped.Detail),
	} {
		if length > maximum-total {
			return 0, false
		}
		total += length
	}
	return total, true
}

func validateSkippedObservations(skipped []Skipped) error {
	if len(skipped) > maximumSkippedObservations {
		return fmt.Errorf(
			"skipped observations exceed %d rows",
			maximumSkippedObservations,
		)
	}
	diagnosticBytes := 0
	for index, observation := range skipped {
		if err := observation.validate(); err != nil {
			return fmt.Errorf("skipped observation %d: %w", index, err)
		}
		rowBytes, withinBudget := skippedDiagnosticBytesWithin(
			observation,
			maximumSkippedDiagnosticBytes-diagnosticBytes,
		)
		if !withinBudget {
			return fmt.Errorf(
				"skipped observations exceed %d diagnostic bytes",
				maximumSkippedDiagnosticBytes,
			)
		}
		diagnosticBytes += rowBytes
	}
	return nil
}

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
		"invalid_canonical_mcp",
		"mcp_provider_document_lossy",
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
	if len(skipped.Detail) > maximumSkippedDetailBytes {
		return fmt.Errorf("detail exceeds %d bytes", maximumSkippedDetailBytes)
	}
	if skipped.Category() == SkipCategoryActionRequired && skipped.ActionHint() == "" {
		return fmt.Errorf("actionable skip requires an action hint")
	}
	return nil
}
