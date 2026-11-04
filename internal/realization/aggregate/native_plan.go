package aggregate

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/output"
)

// RetainsStructuralContainer is a derived native plan relation, not permission
// to overwrite an ancestor or retain arbitrary removed values.
func (plan Plan) RetainsStructuralContainer(address ProjectionAddress) bool {
	return plan.retainedContainers[address]
}

func (plan *Plan) indexNativeTreeRelations() error {
	if _, native := HostConfigSyntaxForCodec(plan.intents[0].before.contract.CodecContractID()); !native {
		return nil
	}
	desired := make([]output.KeyPath, 0, len(plan.intents))
	for _, intent := range plan.intents {
		if intent.desired != nil {
			desired = append(desired, intent.before.contract.address.KeyPath())
		}
	}
	sort.Slice(desired, func(left, right int) bool {
		return desired[left].String() < desired[right].String()
	})
	for _, key := range desired {
		if hasDesiredDescendant(desired, key) {
			return fmt.Errorf("native aggregate plan has overlapping desired key scopes")
		}
	}
	for _, intent := range plan.intents {
		if intent.desired == nil && hasDesiredDescendant(desired, intent.before.contract.address.KeyPath()) {
			if plan.retainedContainers == nil {
				plan.retainedContainers = make(map[ProjectionAddress]bool)
			}
			plan.retainedContainers[intent.before.contract.address] = true
		}
	}
	return nil
}

func hasDesiredDescendant(desired []output.KeyPath, ancestor output.KeyPath) bool {
	prefix := ancestor.String() + "/"
	index := sort.Search(len(desired), func(index int) bool {
		return desired[index].String() >= prefix
	})
	return index < len(desired) && ancestor.Contains(desired[index])
}
