package projection

import (
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
)

// HostConfigKeySelection describes one declaration's selected key paths.
// It grants neither replacement approval nor filesystem mutation authority.
type HostConfigKeySelection struct {
	declaration entity.ID
	selectors   []output.KeyPath
}

func NewHostConfigKeySelection(declaration entity.ID, selectors []output.KeyPath) (HostConfigKeySelection, error) {
	if err := declaration.Validate(); err != nil {
		return HostConfigKeySelection{}, err
	}
	if declaration.Kind() != entity.KindHostConfig {
		return HostConfigKeySelection{}, fmt.Errorf("key selection requires a host-config declaration")
	}
	keys := slices.Clone(selectors)
	for _, key := range keys {
		if _, err := output.ParseKeyPath(key.String()); err != nil {
			return HostConfigKeySelection{}, err
		}
	}
	slices.SortFunc(keys, func(left, right output.KeyPath) int { return strings.Compare(left.String(), right.String()) })
	keys = slices.Compact(keys)
	return HostConfigKeySelection{declaration: declaration, selectors: keys}, nil
}

func (selection HostConfigKeySelection) Validate() error {
	_, err := NewHostConfigKeySelection(selection.declaration, selection.selectors)
	return err
}

func (selection HostConfigKeySelection) Declaration() entity.ID { return selection.declaration }

func (selection HostConfigKeySelection) Selectors() []output.KeyPath {
	return slices.Clone(selection.selectors)
}

func (selection HostConfigKeySelection) Includes(key output.KeyPath) bool {
	return len(selection.selectors) == 0 || slices.ContainsFunc(selection.selectors, func(selector output.KeyPath) bool { return selector.Contains(key) })
}
