package skill

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExpansionLimitsRejectInvalidPolicies(t *testing.T) {
	t.Parallel()
	for _, values := range [][6]int64{
		{0, 1, 1, 1, 1, 1},
		{1, 0, 1, 1, 1, 1},
		{1, 1, 0, 1, 1, 1},
		{1, 1, 1, 0, 1, 1},
		{1, 1, 1, 1, 0, 1},
		{1, 1, 1, 1, 1, 0},
	} {
		if _, err := newExpansionLimits(values[0], values[1], values[2], values[3], values[4], values[5]); err == nil {
			t.Fatalf("newExpansionLimits%v returned nil error", values)
		}
	}
}

func TestExpansionBudgetAcceptsExactGroupLimitAndRejectsLimitPlusOne(t *testing.T) {
	t.Parallel()
	budget := mustExpansionBudget(t, mustExpansionLimits(t, 2, 1, 1, 1, 1, 1))
	if err := budget.CheckGroupCount(2); err != nil {
		t.Fatalf("CheckGroupCount at exact limit returned error: %v", err)
	}
	assertExpansionLimit(t, budget.CheckGroupCount(3), ExpansionLimitGroups)
}

func TestExpansionBudgetAcceptsExactDeclarationLimits(t *testing.T) {
	t.Parallel()
	limits := mustExpansionLimits(t, 1, 2, 4, 10, 10, 2)
	budget := mustExpansionBudget(t, limits)
	include := []Selector{{kind: SelectorGlob, pattern: "ab"}}
	exclude := []Selector{{kind: SelectorGlob, pattern: "cd"}}
	if err := budget.validateDeclaration(include, exclude); err != nil {
		t.Fatalf("validateDeclaration at exact limits returned error: %v", err)
	}
}

func TestExpansionBudgetRejectsDeclarationLimitPlusOne(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		include []Selector
		exclude []Selector
		kind    ExpansionLimitKind
	}{
		{
			name: "selector count",
			include: []Selector{
				{kind: SelectorGlob, pattern: "a"},
				{kind: SelectorGlob, pattern: "b"},
			},
			exclude: []Selector{{kind: SelectorGlob, pattern: "c"}},
			kind:    ExpansionLimitSelectors,
		},
		{
			name:    "pattern bytes",
			include: []Selector{{kind: SelectorGlob, pattern: "abcde"}},
			kind:    ExpansionLimitPatternBytes,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			budget := mustExpansionBudget(t, mustExpansionLimits(t, 1, 2, 4, 10, 10, 2))
			err := budget.validateDeclaration(testCase.include, testCase.exclude)
			assertExpansionLimit(t, err, testCase.kind)
		})
	}
}

func TestExpansionBudgetAcceptsExactWorkAndSelectionLimits(t *testing.T) {
	t.Parallel()
	budget := mustExpansionBudget(t, mustExpansionLimits(t, 1, 2, 4, 2, 4, 2))
	for range 2 {
		if err := budget.admitMatch(2); err != nil {
			t.Fatalf("admitMatch at exact limits returned error: %v", err)
		}
		if err := budget.admitSelection(); err != nil {
			t.Fatalf("admitSelection at exact limit returned error: %v", err)
		}
	}
}

func TestExpansionBudgetRejectsWorkAndSelectionLimitPlusOne(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		limits  expansionLimits
		consume func(*ExpansionBudget) error
		kind    ExpansionLimitKind
	}{
		{
			name:   "match evaluations",
			limits: mustExpansionLimits(t, 1, 2, 4, 1, 10, 2),
			consume: func(budget *ExpansionBudget) error {
				if err := budget.admitMatch(1); err != nil {
					return err
				}
				return budget.admitMatch(1)
			},
			kind: ExpansionLimitMatchEvaluations,
		},
		{
			name:   "matcher work units",
			limits: mustExpansionLimits(t, 1, 2, 4, 3, 4, 2),
			consume: func(budget *ExpansionBudget) error {
				if err := budget.admitMatch(2); err != nil {
					return err
				}
				return budget.admitMatch(3)
			},
			kind: ExpansionLimitMatcherWorkUnits,
		},
		{
			name:   "selected skills",
			limits: mustExpansionLimits(t, 1, 2, 4, 2, 10, 1),
			consume: func(budget *ExpansionBudget) error {
				if err := budget.admitSelection(); err != nil {
					return err
				}
				return budget.admitSelection()
			},
			kind: ExpansionLimitSelectedSkills,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			budget := mustExpansionBudget(t, testCase.limits)
			assertExpansionLimit(t, testCase.consume(budget), testCase.kind)
		})
	}
}

func TestSelectNamesDoesNotRefundBroadIncludeBeforeExclusion(t *testing.T) {
	t.Parallel()
	budget := mustExpansionBudget(t, mustExpansionLimits(t, 1, 2, 8, 10, 100, 1))
	include := []Selector{mustSelector(t, "glob:*")}
	exclude := []Selector{mustSelector(t, "glob:beta")}

	_, err := selectNames(context.Background(), []string{"alpha", "beta"}, include, exclude, budget)
	assertExpansionLimit(t, err, ExpansionLimitSelectedSkills)
}

func TestSelectNamesChargesMultiplicativeMatcherWorkBeforeEvaluation(t *testing.T) {
	t.Parallel()
	budget := mustExpansionBudget(t, mustExpansionLimits(t, 1, 1, 10, 1, 100, 1))
	include := []Selector{mustSelector(t, "glob:aaaaaaaaaa")}

	_, err := selectNames(context.Background(), []string{"bbbbbbbbbb"}, include, nil, budget)
	assertExpansionLimit(t, err, ExpansionLimitMatcherWorkUnits)
	if budget.matchEvaluations != 0 {
		t.Fatalf("match evaluations = %d, want no matcher call after work rejection", budget.matchEvaluations)
	}
}

func TestSelectNamesRejectsMaximumPathologicalGlobBeforeEvaluation(t *testing.T) {
	t.Parallel()
	pattern := "*" + strings.Repeat("a", int(defaultExpansionPatternBytes)-1)
	selector := mustSelector(t, "glob:"+pattern)
	name := strings.Repeat("b", 4<<10)
	budget := NewExpansionBudget()

	_, err := selectNames(context.Background(), []string{name}, []Selector{selector}, nil, budget)
	assertExpansionLimit(t, err, ExpansionLimitMatcherWorkUnits)
	if budget.matchEvaluations != 0 {
		t.Fatalf("match evaluations = %d, want no matcher call after work rejection", budget.matchEvaluations)
	}
}

func TestSelectNamesHonorsCancellationBeforeMatching(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget := NewExpansionBudget()

	_, err := selectNames(ctx, []string{"alpha"}, []Selector{mustSelector(t, "glob:*")}, nil, budget)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("selectNames error = %v, want context cancellation", err)
	}
	if budget.matchEvaluations != 0 {
		t.Fatalf("match evaluations = %d, want none", budget.matchEvaluations)
	}
}

func TestRegexSelectorMatcherCompilesExpressionOnceBeforeCandidateMatching(t *testing.T) {
	t.Parallel()
	selector := mustSelector(t, "regex:^alpha$")
	matcher, err := newSelectorMatcher(selector)
	if err != nil {
		t.Fatalf("newSelectorMatcher returned error: %v", err)
	}
	if matcher.regex == nil {
		t.Fatal("newSelectorMatcher omitted compiled regex")
	}
	matched, err := matcher.matches("alpha")
	if err != nil || !matched {
		t.Fatalf("matches = %t, %v; want true", matched, err)
	}
	matcher.regex = nil
	if _, err := matcher.matches("alpha"); err == nil || !strings.Contains(err.Error(), "not compiled") {
		t.Fatalf("unnormalized matches error = %v", err)
	}
}

func TestSelectorSemanticEqualityDoesNotDependOnMatcherState(t *testing.T) {
	t.Parallel()
	left := mustSelector(t, "regex:^alpha$")
	right := mustSelector(t, "regex:^alpha$")
	if left != right {
		t.Fatalf("equal selectors compare unequal: %#v / %#v", left, right)
	}
}

func TestParseSelectorRejectsPatternBeyondDefaultBudgetBeforeRegexCompile(t *testing.T) {
	t.Parallel()
	_, err := ParseSelector("regex:" + strings.Repeat("a", int(defaultExpansionPatternBytes)+1))
	assertExpansionLimit(t, err, ExpansionLimitPatternBytes)
}

func TestSkillExpansionDiagnosticsBoundUntrustedValues(t *testing.T) {
	t.Parallel()
	longValue := strings.Repeat("a", maximumSkillDiagnosticValueBytes+1)

	_, err := ParseSelector("regex:[" + strings.Repeat("a", maximumSkillDiagnosticValueBytes))
	if err == nil || len(err.Error()) > 128 || !strings.Contains(err.Error(), "invalid regex selector") {
		t.Fatalf("regex diagnostic = %q, want bounded syntax error", err)
	}

	selector := mustSelector(t, "glob:"+longValue)
	_, err = selectNames(context.Background(), []string{"other"}, []Selector{selector}, nil, NewExpansionBudget())
	if err == nil {
		t.Fatal("selectNames accepted an unmatched selector")
	}
	if strings.Contains(err.Error(), selector.Expression()) || !strings.Contains(err.Error(), "bytes=134") {
		t.Fatalf("selector diagnostic = %q, want bounded identity", err)
	}

	invalidName := longValue + "/escape"
	_, err = selectNames(context.Background(), []string{invalidName}, []Selector{mustSelector(t, "regex:.*")}, nil, NewExpansionBudget())
	if err == nil {
		t.Fatal("selectNames accepted unsafe child")
	}
	if strings.Contains(err.Error(), invalidName) || !strings.Contains(err.Error(), "bytes=136") {
		t.Fatalf("child diagnostic = %q, want bounded identity", err)
	}
}

func mustExpansionLimits(
	t *testing.T,
	maximumGroups int64,
	maximumSelectors int64,
	maximumPatternBytes int64,
	maximumMatchEvaluations int64,
	maximumMatcherWorkUnits int64,
	maximumSelectedSkills int64,
) expansionLimits {
	t.Helper()
	limits, err := newExpansionLimits(
		maximumGroups,
		maximumSelectors,
		maximumPatternBytes,
		maximumMatchEvaluations,
		maximumMatcherWorkUnits,
		maximumSelectedSkills,
	)
	if err != nil {
		t.Fatal(err)
	}
	return limits
}

func mustExpansionBudget(t *testing.T, limits expansionLimits) *ExpansionBudget {
	t.Helper()
	budget, err := newExpansionBudgetWithLimits(limits)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func assertExpansionLimit(t *testing.T, err error, kind ExpansionLimitKind) {
	t.Helper()
	var limitErr *ExpansionLimitError
	if !errors.As(err, &limitErr) || !errors.Is(err, ErrExpansionLimitExceeded) {
		t.Fatalf("error = %v, want ExpansionLimitError", err)
	}
	if limitErr.Kind() != kind {
		t.Fatalf("limit kind = %q, want %q", limitErr.Kind(), kind)
	}
}
