package qualification

import (
	"fmt"
	"slices"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type namespaceRuleMode uint8

const (
	forwardNamespaceRules namespaceRuleMode = iota + 1
	recordedNamespaceRules
)

// RuleExpectations distinguishes new forward intent from recorded restoration.
// Its zero value cannot silently omit forward admission evidence.
type RuleExpectations struct {
	mode     namespaceRuleMode
	expected []journalnamespace.DocumentNamespace
}

func ForwardRuleExpectations(expected []journalnamespace.DocumentNamespace) RuleExpectations {
	return RuleExpectations{mode: forwardNamespaceRules, expected: slices.Clone(expected)}
}

func RecordedRuleExpectations() RuleExpectations {
	return RuleExpectations{mode: recordedNamespaceRules}
}

func (rules RuleExpectations) index() (map[aggregate.DocumentAddress]journalnamespace.DocumentNamespace, error) {
	switch rules.mode {
	case recordedNamespaceRules:
		return nil, nil
	case forwardNamespaceRules:
	default:
		return nil, fmt.Errorf("native namespace qualification requires explicit forward or recorded rule expectations")
	}
	expected := make(map[aggregate.DocumentAddress]journalnamespace.DocumentNamespace)
	for _, namespace := range rules.expected {
		for _, address := range namespace.Addresses() {
			if _, duplicate := expected[address]; duplicate {
				return nil, fmt.Errorf("native forward rule observations contain duplicate aliases")
			}
			expected[address] = namespace
		}
	}
	return expected, nil
}
