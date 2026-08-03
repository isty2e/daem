package skill

import (
	"errors"
	"fmt"
)

const (
	defaultExpansionSelectors         int64 = 128
	defaultExpansionPatternBytes      int64 = 64 << 10
	defaultExpansionMatchEvaluations  int64 = 1_000_000
	defaultExpansionMatcherInputBytes int64 = 128 << 20
	defaultExpansionSelectedSkills    int64 = 4_096
)

// ExpansionLimitKind identifies one independent skill-group expansion dimension.
type ExpansionLimitKind string

const (
	ExpansionLimitSelectors         ExpansionLimitKind = "selectors"
	ExpansionLimitPatternBytes      ExpansionLimitKind = "pattern_bytes"
	ExpansionLimitMatchEvaluations  ExpansionLimitKind = "match_evaluations"
	ExpansionLimitMatcherInputBytes ExpansionLimitKind = "matcher_input_bytes"
	ExpansionLimitSelectedSkills    ExpansionLimitKind = "selected_skills"
)

// ErrExpansionLimitExceeded classifies bounded skill-group expansion failures.
var ErrExpansionLimitExceeded = errors.New("skill group expansion limit exceeded")

type expansionLimits struct {
	maximumSelectors         int64
	maximumPatternBytes      int64
	maximumMatchEvaluations  int64
	maximumMatcherInputBytes int64
	maximumSelectedSkills    int64
}

func newExpansionLimits(
	maximumSelectors int64,
	maximumPatternBytes int64,
	maximumMatchEvaluations int64,
	maximumMatcherInputBytes int64,
	maximumSelectedSkills int64,
) (expansionLimits, error) {
	limits := expansionLimits{
		maximumSelectors:         maximumSelectors,
		maximumPatternBytes:      maximumPatternBytes,
		maximumMatchEvaluations:  maximumMatchEvaluations,
		maximumMatcherInputBytes: maximumMatcherInputBytes,
		maximumSelectedSkills:    maximumSelectedSkills,
	}
	if err := limits.validate(); err != nil {
		return expansionLimits{}, err
	}
	return limits, nil
}

func defaultExpansionLimits() expansionLimits {
	return expansionLimits{
		maximumSelectors:         defaultExpansionSelectors,
		maximumPatternBytes:      defaultExpansionPatternBytes,
		maximumMatchEvaluations:  defaultExpansionMatchEvaluations,
		maximumMatcherInputBytes: defaultExpansionMatcherInputBytes,
		maximumSelectedSkills:    defaultExpansionSelectedSkills,
	}
}

func (limits expansionLimits) validate() error {
	if limits.maximumSelectors <= 0 || limits.maximumPatternBytes <= 0 ||
		limits.maximumMatchEvaluations <= 0 || limits.maximumMatcherInputBytes <= 0 ||
		limits.maximumSelectedSkills <= 0 {
		return fmt.Errorf("skill group expansion limits must be positive")
	}
	return nil
}

// ExpansionLimitError reports the first deterministic exhausted dimension.
type ExpansionLimitError struct {
	kind     ExpansionLimitKind
	limit    int64
	observed int64
}

func (err *ExpansionLimitError) Error() string {
	if err == nil {
		return ErrExpansionLimitExceeded.Error()
	}
	return fmt.Sprintf(
		"%s: %s observed=%d limit=%d",
		ErrExpansionLimitExceeded,
		err.kind,
		err.observed,
		err.limit,
	)
}

func (err *ExpansionLimitError) Unwrap() error { return ErrExpansionLimitExceeded }

// Kind returns the exhausted expansion dimension.
func (err *ExpansionLimitError) Kind() ExpansionLimitKind {
	if err == nil {
		return ""
	}
	return err.kind
}

// Limit returns the configured maximum.
func (err *ExpansionLimitError) Limit() int64 {
	if err == nil {
		return 0
	}
	return err.limit
}

// Observed returns the first value known to exceed the limit.
func (err *ExpansionLimitError) Observed() int64 {
	if err == nil {
		return 0
	}
	return err.observed
}

// ExpansionBudget accounts for one sequential skill-group expansion phase.
// Declaration limits are checked per group; work and selected results
// accumulate across groups.
type ExpansionBudget struct {
	limits            expansionLimits
	matchEvaluations  int64
	matcherInputBytes int64
	selectedSkills    int64
	exhausted         *ExpansionLimitError
}

// NewExpansionBudget constructs one budget using package defaults.
func NewExpansionBudget() *ExpansionBudget {
	return &ExpansionBudget{limits: defaultExpansionLimits()}
}

func newExpansionBudgetWithLimits(limits expansionLimits) (*ExpansionBudget, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	return &ExpansionBudget{limits: limits}, nil
}

func (budget *ExpansionBudget) validateDeclaration(include []Selector, exclude []Selector) error {
	if budget == nil {
		return fmt.Errorf("skill group expansion budget is required")
	}
	return budget.limits.validateDeclaration(include, exclude)
}

func (limits expansionLimits) validateDeclaration(include []Selector, exclude []Selector) error {
	selectorCount := int64(len(include)) + int64(len(exclude))
	if selectorCount > limits.maximumSelectors {
		return newExpansionLimitError(
			ExpansionLimitSelectors,
			limits.maximumSelectors,
			selectorCount,
		)
	}
	var patternBytes int64
	for _, selector := range include {
		patternBytes += int64(len(selector.Pattern()))
	}
	for _, selector := range exclude {
		patternBytes += int64(len(selector.Pattern()))
	}
	if patternBytes > limits.maximumPatternBytes {
		return newExpansionLimitError(
			ExpansionLimitPatternBytes,
			limits.maximumPatternBytes,
			patternBytes,
		)
	}
	return nil
}

func (budget *ExpansionBudget) admitMatch(inputBytes int64) error {
	if budget == nil {
		return fmt.Errorf("skill group expansion budget is required")
	}
	if inputBytes < 0 {
		return fmt.Errorf("skill group matcher input bytes must not be negative")
	}
	if budget.exhausted != nil {
		return budget.exhausted
	}
	if budget.matchEvaluations == budget.limits.maximumMatchEvaluations {
		return budget.exhaustLocked(
			ExpansionLimitMatchEvaluations,
			budget.limits.maximumMatchEvaluations,
			budget.matchEvaluations+1,
		)
	}
	if inputBytes > budget.limits.maximumMatcherInputBytes-budget.matcherInputBytes {
		return budget.exhaustLocked(
			ExpansionLimitMatcherInputBytes,
			budget.limits.maximumMatcherInputBytes,
			budget.limits.maximumMatcherInputBytes+1,
		)
	}
	budget.matchEvaluations++
	budget.matcherInputBytes += inputBytes
	return nil
}

func (budget *ExpansionBudget) admitSelection() error {
	if budget == nil {
		return fmt.Errorf("skill group expansion budget is required")
	}
	if budget.exhausted != nil {
		return budget.exhausted
	}
	if budget.selectedSkills == budget.limits.maximumSelectedSkills {
		return budget.exhaustLocked(
			ExpansionLimitSelectedSkills,
			budget.limits.maximumSelectedSkills,
			budget.selectedSkills+1,
		)
	}
	budget.selectedSkills++
	return nil
}

func (budget *ExpansionBudget) exhaustLocked(
	kind ExpansionLimitKind,
	limit int64,
	observed int64,
) error {
	if budget.exhausted == nil {
		budget.exhausted = newExpansionLimitError(kind, limit, observed)
	}
	return budget.exhausted
}

func newExpansionLimitError(kind ExpansionLimitKind, limit int64, observed int64) *ExpansionLimitError {
	return &ExpansionLimitError{kind: kind, limit: limit, observed: observed}
}
