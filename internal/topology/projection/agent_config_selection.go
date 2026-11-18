package projection

import (
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
)

// AgentConfigKeySelection describes one declaration's selected key paths.
// It grants neither replacement approval nor filesystem mutation authority.
type AgentConfigKeySelection struct {
	declaration entity.ID
	selectors   []output.KeyPath
}

func NewAgentConfigKeySelection(declaration entity.ID, selectors []output.KeyPath) (AgentConfigKeySelection, error) {
	if err := declaration.Validate(); err != nil {
		return AgentConfigKeySelection{}, err
	}
	if declaration.Kind() != entity.KindAgentConfig {
		return AgentConfigKeySelection{}, fmt.Errorf("key selection requires an agent configuration declaration")
	}
	keys := slices.Clone(selectors)
	for _, key := range keys {
		if _, err := output.ParseKeyPath(key.String()); err != nil {
			return AgentConfigKeySelection{}, err
		}
	}
	slices.SortFunc(keys, func(left, right output.KeyPath) int { return strings.Compare(left.String(), right.String()) })
	keys = slices.Compact(keys)
	return AgentConfigKeySelection{declaration: declaration, selectors: keys}, nil
}

func (selection AgentConfigKeySelection) Validate() error {
	_, err := NewAgentConfigKeySelection(selection.declaration, selection.selectors)
	return err
}

func (selection AgentConfigKeySelection) Declaration() entity.ID { return selection.declaration }

func (selection AgentConfigKeySelection) Selectors() []output.KeyPath {
	return slices.Clone(selection.selectors)
}

func (selection AgentConfigKeySelection) Includes(key output.KeyPath) bool {
	return len(selection.selectors) == 0 || slices.ContainsFunc(selection.selectors, func(selector output.KeyPath) bool { return selector.Contains(key) })
}
