package profile

import (
	"fmt"
	"path"
	"strings"

	"github.com/isty2e/daem/internal/target"
)

type piMCPBuiltinState uint8

const (
	piMCPBuiltinUnobserved piMCPBuiltinState = iota
	piMCPBuiltinEnabled
	piMCPBuiltinDisabled
)

type piMCPBuiltinMatch uint8

const (
	piMCPBuiltinNoMatch piMCPBuiltinMatch = iota
	piMCPBuiltinMatched
	piMCPBuiltinMatchUnobserved
)

// PiMCPBuiltinSelection retains separate user and project settings evidence.
// Its zero value cannot qualify a Native projection.
type PiMCPBuiltinSelection struct {
	global  piMCPBuiltinState
	project piMCPBuiltinState
}

// ResolvePiMCPBuiltinSelection models user forced-exclusion precedence and
// project last-matching override precedence. Plain entries do not select builtins.
func ResolvePiMCPBuiltinSelection(globalDirectives, projectDirectives []string) PiMCPBuiltinSelection {
	var groups [3]piMCPBuiltinMatch
	for _, directive := range globalDirectives {
		group, _, match := piMCPBuiltinDirective(directive)
		if group < 0 || groups[group] == piMCPBuiltinMatched {
			continue
		}
		if match != piMCPBuiltinNoMatch {
			groups[group] = match
		}
	}

	global := piMCPBuiltinEnabled
	for index, desired := range []piMCPBuiltinState{piMCPBuiltinDisabled, piMCPBuiltinEnabled, piMCPBuiltinDisabled} {
		global = applyPiMCPBuiltinMatch(global, groups[index], desired)
	}
	project := global
	for _, directive := range projectDirectives {
		_, desired, match := piMCPBuiltinDirective(directive)
		project = applyPiMCPBuiltinMatch(project, match, desired)
	}
	return PiMCPBuiltinSelection{global: global, project: project}
}

func (selection PiMCPBuiltinSelection) requireEnabled(scope target.Scope) error {
	states := []piMCPBuiltinState{selection.project}
	if scope == target.ScopeGlobal {
		states = append(states, selection.global)
	}
	for _, state := range states {
		if state == piMCPBuiltinDisabled {
			return fmt.Errorf("native Pi MCP builtin is disabled in the applicable settings envelope; no settings were changed")
		}
	}
	for _, state := range states {
		if state != piMCPBuiltinEnabled {
			return fmt.Errorf("native Pi MCP builtin selection cannot be established within the static selector envelope; no settings were changed")
		}
	}
	return nil
}

func applyPiMCPBuiltinMatch(current piMCPBuiltinState, match piMCPBuiltinMatch, desired piMCPBuiltinState) piMCPBuiltinState {
	switch match {
	case piMCPBuiltinNoMatch:
		return current
	case piMCPBuiltinMatched:
		return desired
	default:
		if current == desired {
			return current
		}
		return piMCPBuiltinUnobserved
	}
}

func piMCPBuiltinDirective(directive string) (int, piMCPBuiltinState, piMCPBuiltinMatch) {
	if directive == "" {
		return -1, piMCPBuiltinUnobserved, piMCPBuiltinNoMatch
	}
	pattern := strings.ReplaceAll(directive[1:], `\`, "/")
	switch directive[0] {
	case '!':
		return 0, piMCPBuiltinDisabled, matchPiMCPBuiltinPattern(pattern, false)
	case '+':
		return 1, piMCPBuiltinEnabled, matchPiMCPBuiltinPattern(pattern, true)
	case '-':
		return 2, piMCPBuiltinDisabled, matchPiMCPBuiltinPattern(pattern, true)
	default:
		return -1, piMCPBuiltinUnobserved, piMCPBuiltinNoMatch
	}
}

func matchPiMCPBuiltinPattern(pattern string, exact bool) piMCPBuiltinMatch {
	const identity = "builtin:mcp"
	if exact {
		pattern = strings.TrimPrefix(pattern, "./")
		if pattern == identity {
			return piMCPBuiltinMatched
		}
		if strings.Contains(pattern, "/") && strings.HasSuffix(pattern, "/"+identity) {
			return piMCPBuiltinMatchUnobserved
		}
		return piMCPBuiltinNoMatch
	}

	// Full Minimatch and root-relative aliases are outside passive matching.
	// Preserve uncertainty instead of treating unmodelled syntax as a non-match.
	if strings.ContainsAny(pattern, "/[]{}()!") {
		return piMCPBuiltinMatchUnobserved
	}
	matched, err := path.Match(pattern, identity)
	if err != nil {
		return piMCPBuiltinMatchUnobserved
	}
	if matched {
		return piMCPBuiltinMatched
	}
	return piMCPBuiltinNoMatch
}
