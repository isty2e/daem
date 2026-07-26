package skill

import (
	"fmt"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

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
		if _, err := regexp.Compile(pattern); err != nil {
			return Selector{}, fmt.Errorf("invalid regex selector: %w", err)
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

func (selector Selector) matches(name string) (bool, error) {
	switch selector.kind {
	case SelectorGlob:
		return pathpkg.Match(selector.pattern, name)
	case SelectorRegex:
		expression, err := regexp.Compile(selector.pattern)
		if err != nil {
			return false, err
		}
		return expression.MatchString(name), nil
	default:
		return false, fmt.Errorf("unknown selector kind %q", selector.kind)
	}
}

func selectNames(childNames []string, include []Selector, exclude []Selector) ([]string, error) {
	candidateCounts := make(map[string]int, len(childNames))
	for _, childName := range childNames {
		candidateCounts[childName]++
	}

	selected := make(map[string]struct{})
	for index, selector := range include {
		matches := 0
		for _, name := range childNames {
			matched, err := selector.matches(name)
			if err != nil {
				return nil, fmt.Errorf("include[%d]: %w", index, err)
			}
			if matched {
				selected[name] = struct{}{}
				matches++
			}
		}
		if matches == 0 {
			return nil, fmt.Errorf("include[%d]: selector %q matched no skill directories", index, selector.Expression())
		}
	}

	for index, selector := range exclude {
		for _, name := range childNames {
			matched, err := selector.matches(name)
			if err != nil {
				return nil, fmt.Errorf("exclude[%d]: %w", index, err)
			}
			if matched {
				delete(selected, name)
			}
		}
	}

	selectedNames := make([]string, 0, len(selected))
	for name := range selected {
		selectedNames = append(selectedNames, name)
	}
	sort.Strings(selectedNames)
	if len(selectedNames) == 0 {
		return nil, fmt.Errorf("include: selectors matched no skills after exclusions")
	}

	names := make([]string, 0, len(selectedNames))
	for _, selectedName := range selectedNames {
		name, err := cleanName(selectedName)
		if err != nil || name != selectedName {
			return nil, fmt.Errorf("selected child %q must be a canonical safe single path segment", selectedName)
		}
		if candidateCounts[selectedName] > 1 {
			return nil, fmt.Errorf("selected child name %q appears more than once", selectedName)
		}
		names = append(names, name)
	}
	return names, nil
}

func selectorSetMatches(name string, include []Selector, exclude []Selector) (bool, error) {
	included := false
	for _, selector := range include {
		matched, err := selector.matches(name)
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
	for _, selector := range exclude {
		matched, err := selector.matches(name)
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

func isUnsafeControl(value rune) bool {
	return unicode.IsControl(value) || unicode.Is(unicode.Bidi_Control, value)
}
