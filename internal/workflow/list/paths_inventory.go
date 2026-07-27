package listworkflow

import (
	"cmp"
	"fmt"
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

// LocationInventory is one canonical, defensively exposed location inventory.
type LocationInventory struct {
	entries []LocationEntry
}

func newLocationInventory(values []LocationEntry) (LocationInventory, error) {
	entries := append([]LocationEntry(nil), values...)
	for index, entry := range entries {
		if err := entry.validate(); err != nil {
			return LocationInventory{}, fmt.Errorf("location entry[%d]: %w", index, err)
		}
	}
	sort.Slice(entries, func(left int, right int) bool {
		return compareLocationEntries(entries[left], entries[right]) < 0
	})
	for index := 1; index < len(entries); index++ {
		if compareLocationEntries(entries[index-1], entries[index]) == 0 {
			return LocationInventory{}, fmt.Errorf(
				"duplicate location entry for target %q scope %q resource %q",
				entries[index].Target(),
				entries[index].Scope(),
				entries[index].ResourceKind(),
			)
		}
	}
	return LocationInventory{entries: entries}, nil
}

func (inventory LocationInventory) Entries() []LocationEntry {
	return append([]LocationEntry(nil), inventory.entries...)
}

func compareLocationEntries(left LocationEntry, right LocationEntry) int {
	comparisons := []int{
		cmp.Compare(
			slices.Index(target.SupportedTargets(), left.Target()),
			slices.Index(target.SupportedTargets(), right.Target()),
		),
		cmp.Compare(scopeRank(left.Scope()), scopeRank(right.Scope())),
		cmp.Compare(resourceRank(left.ResourceKind()), resourceRank(right.ResourceKind())),
		cmp.Compare(left.Variant(), right.Variant()),
		cmp.Compare(roleRank(left.Role()), roleRank(right.Role())),
		cmp.Compare(left.Operation(), right.Operation()),
		cmp.Compare(left.Path(), right.Path()),
		cmp.Compare(left.Route(), right.Route()),
		cmp.Compare(left.Reason(), right.Reason()),
		cmp.Compare(left.Detail(), right.Detail()),
	}
	for _, comparison := range comparisons {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

func scopeRank(scope target.Scope) int {
	if scope == target.ScopeProject {
		return 0
	}
	return 1
}

func resourceRank(kind entity.Kind) int {
	switch kind {
	case entity.KindInstructions:
		return 0
	case entity.KindSkill:
		return 1
	case entity.KindHook:
		return 2
	case entity.KindHookAsset:
		return 3
	case entity.KindMCPServer:
		return 4
	case entity.KindExtension:
		return 5
	default:
		return 9
	}
}

func roleRank(role LocationRole) int {
	switch role {
	case LocationRoleWrite:
		return 0
	case LocationRoleDiscovery:
		return 1
	case LocationRoleRuntime:
		return 2
	case LocationRoleConfig:
		return 3
	case LocationRoleInternal:
		return 4
	case LocationRoleDelegated:
		return 5
	case LocationRoleUnsupported:
		return 6
	default:
		return 9
	}
}
