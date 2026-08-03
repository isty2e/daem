package skill

import (
	"context"
	"errors"
	"fmt"
	"math"
	pathpkg "path"
	"regexp"
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maximumSkillDiagnosticValueBytes = 128

// SelectorKind identifies one admitted SkillSet selector algebra.
type SelectorKind string

const (
	SelectorGlob  SelectorKind = "glob"
	SelectorRegex SelectorKind = "regex"
)

// Selector is one immutable direct-child selector.
type Selector struct {
	kind    SelectorKind
	pattern string
}

type selectorMatcher struct {
	selector Selector
	regex    *regexp.Regexp
}

// ParseSelector validates a selector expression.
func ParseSelector(value string) (Selector, error) {
	trimmed := strings.TrimSpace(value)
	kindValue, pattern, ok := strings.Cut(trimmed, ":")
	if !ok {
		return Selector{}, fmt.Errorf("selector must use glob:<pattern> or regex:<pattern>")
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return Selector{}, fmt.Errorf("selector pattern is required")
	}
	if int64(len(pattern)) > defaultExpansionPatternBytes {
		return Selector{}, newExpansionLimitError(
			ExpansionLimitPatternBytes,
			defaultExpansionPatternBytes,
			int64(len(pattern)),
		)
	}

	switch SelectorKind(strings.TrimSpace(kindValue)) {
	case SelectorGlob:
		if strings.ContainsAny(pattern, "/\\") {
			return Selector{}, fmt.Errorf("glob selector matches direct child names and must not contain path separators")
		}
		if _, err := pathpkg.Match(pattern, ""); err != nil {
			return Selector{}, fmt.Errorf("invalid glob selector: %w", err)
		}
		return Selector{kind: SelectorGlob, pattern: pattern}, nil
	case SelectorRegex:
		_, err := regexp.Compile(pattern)
		if err != nil {
			var syntaxErr *syntax.Error
			if errors.As(err, &syntaxErr) {
				return Selector{}, fmt.Errorf("invalid regex selector: %s", syntaxErr.Code)
			}
			return Selector{}, fmt.Errorf("invalid regex selector")
		}
		return Selector{kind: SelectorRegex, pattern: pattern}, nil
	default:
		return Selector{}, fmt.Errorf("selector must use glob:<pattern> or regex:<pattern>")
	}
}

// Kind returns the selector algebra kind.
func (selector Selector) Kind() SelectorKind { return selector.kind }

// Pattern returns the selector pattern.
func (selector Selector) Pattern() string { return selector.pattern }

// Expression returns the canonical selector expression.
func (selector Selector) Expression() string {
	if selector.kind == "" {
		return selector.pattern
	}
	return string(selector.kind) + ":" + selector.pattern
}

func newSelectorMatcher(selector Selector) (selectorMatcher, error) {
	matcher := selectorMatcher{selector: selector}
	switch selector.kind {
	case SelectorGlob:
		return matcher, nil
	case SelectorRegex:
		expression, err := regexp.Compile(selector.pattern)
		if err != nil {
			return selectorMatcher{}, fmt.Errorf("regex selector is not normalized")
		}
		matcher.regex = expression
		return matcher, nil
	default:
		return selectorMatcher{}, fmt.Errorf("unknown selector kind %q", selector.kind)
	}
}

func newSelectorMatchers(selectors []Selector) ([]selectorMatcher, error) {
	matchers := make([]selectorMatcher, 0, len(selectors))
	for index, selector := range selectors {
		matcher, err := newSelectorMatcher(selector)
		if err != nil {
			return nil, fmt.Errorf("selector[%d]: %w", index, err)
		}
		matchers = append(matchers, matcher)
	}
	return matchers, nil
}

func (matcher selectorMatcher) matches(name string) (bool, error) {
	switch matcher.selector.kind {
	case SelectorGlob:
		return pathpkg.Match(matcher.selector.pattern, name)
	case SelectorRegex:
		if matcher.regex == nil {
			return false, fmt.Errorf("regex selector matcher is not compiled")
		}
		return matcher.regex.MatchString(name), nil
	default:
		return false, fmt.Errorf("unknown selector kind %q", matcher.selector.kind)
	}
}

// workUnits bounds the pattern scan and post-star retries performed by glob
// matching; the same conservative charge also covers linear regexp matching.
func (matcher selectorMatcher) workUnits(name string) int64 {
	patternBytes := int64(len(matcher.selector.pattern))
	nameBytes := int64(len(name))
	if nameBytes == math.MaxInt64 {
		return math.MaxInt64
	}
	nameScanBytes := nameBytes + 1
	if patternBytes > math.MaxInt64/nameScanBytes {
		return math.MaxInt64
	}
	return patternBytes * nameScanBytes
}

func (matcher selectorMatcher) evaluate(
	ctx context.Context,
	name string,
	budget *ExpansionBudget,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := budget.admitMatch(matcher.workUnits(name)); err != nil {
		return false, err
	}
	matched, err := matcher.matches(name)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return matched, nil
}

func selectNames(
	ctx context.Context,
	childNames []string,
	include []Selector,
	exclude []Selector,
	budget *ExpansionBudget,
) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("skill group expansion context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := budget.validateDeclaration(include, exclude); err != nil {
		return nil, err
	}
	includeMatchers, err := newSelectorMatchers(include)
	if err != nil {
		return nil, fmt.Errorf("include: %w", err)
	}
	excludeMatchers, err := newSelectorMatchers(exclude)
	if err != nil {
		return nil, fmt.Errorf("exclude: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	selected := make(map[string]struct{})
	for index, matcher := range includeMatchers {
		matches := 0
		for _, name := range childNames {
			matched, err := matcher.evaluate(ctx, name, budget)
			if err != nil {
				return nil, fmt.Errorf("include[%d]: %w", index, err)
			}
			if matched {
				if _, exists := selected[name]; !exists {
					if err := budget.admitSelection(); err != nil {
						return nil, err
					}
					selected[name] = struct{}{}
				}
				matches++
			}
		}
		if matches == 0 {
			return nil, fmt.Errorf(
				"include[%d]: selector %s matched no skill directories",
				index,
				skillDiagnosticValue(matcher.selector.Expression()),
			)
		}
	}

	for index, matcher := range excludeMatchers {
		for _, name := range childNames {
			matched, err := matcher.evaluate(ctx, name, budget)
			if err != nil {
				return nil, fmt.Errorf("exclude[%d]: %w", index, err)
			}
			if matched {
				delete(selected, name)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	selectedNames := make([]string, 0, len(selected))
	for name := range selected {
		selectedNames = append(selectedNames, name)
	}
	sort.Strings(selectedNames)
	if len(selectedNames) == 0 {
		return nil, fmt.Errorf("include: selectors matched no skills after exclusions")
	}

	candidateCounts := make(map[string]int, len(selectedNames))
	for _, childName := range childNames {
		if _, selected := selected[childName]; selected {
			candidateCounts[childName]++
		}
	}

	names := make([]string, 0, len(selectedNames))
	for _, selectedName := range selectedNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name, err := cleanName(selectedName)
		if err != nil || name != selectedName {
			return nil, fmt.Errorf(
				"selected child %s must be a canonical safe single path segment",
				skillDiagnosticValue(selectedName),
			)
		}
		if candidateCounts[selectedName] > 1 {
			return nil, fmt.Errorf(
				"selected child name %s appears more than once",
				skillDiagnosticValue(selectedName),
			)
		}
		names = append(names, name)
	}
	return names, nil
}

func selectorSetMatches(
	name string,
	include []Selector,
	exclude []Selector,
	budget *ExpansionBudget,
) (bool, error) {
	if err := budget.validateDeclaration(include, exclude); err != nil {
		return false, err
	}
	includeMatchers, err := newSelectorMatchers(include)
	if err != nil {
		return false, fmt.Errorf("include: %w", err)
	}
	excludeMatchers, err := newSelectorMatchers(exclude)
	if err != nil {
		return false, fmt.Errorf("exclude: %w", err)
	}
	included := false
	for _, matcher := range includeMatchers {
		matched, err := matcher.evaluate(context.Background(), name, budget)
		if err != nil {
			return false, err
		}
		if matched {
			included = true
			break
		}
	}
	if !included {
		return false, nil
	}
	for _, matcher := range excludeMatchers {
		matched, err := matcher.evaluate(context.Background(), name, budget)
		if err != nil {
			return false, err
		}
		if matched {
			return false, nil
		}
	}
	return true, nil
}

// ParseName validates and canonicalizes one skill identity or install name.
func ParseName(value string) (string, error) {
	return cleanName(value)
}

func cleanName(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("must be a safe single path segment")
	}
	name := strings.TrimSpace(value)
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "~") {
		return "", fmt.Errorf("must be a safe single path segment")
	}
	if strings.IndexFunc(name, isUnsafeControl) >= 0 {
		return "", fmt.Errorf("must be a safe single path segment")
	}
	if strings.ContainsAny(name, "/\\") || pathpkg.Clean(name) != name {
		return "", fmt.Errorf("must be a safe single path segment")
	}
	return name, nil
}

func skillDiagnosticValue(value string) string {
	if len(value) <= maximumSkillDiagnosticValueBytes {
		return strconv.Quote(value)
	}
	return fmt.Sprintf(
		"%s (bytes=%d)",
		strconv.Quote(value[:maximumSkillDiagnosticValueBytes]),
		len(value),
	)
}

func isUnsafeControl(value rune) bool {
	return unicode.IsControl(value) || unicode.Is(unicode.Bidi_Control, value)
}
