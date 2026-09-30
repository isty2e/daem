package apply

import "github.com/isty2e/daem/internal/operationplan"

func compileApplyPinRouteSchedule(
	builder *operationplan.EffectStructureBuilder,
	statefile *applyStatefileSchedule,
	route applyRouteScheduleFact,
) operationplan.EffectNode {
	node := operationplan.PinChangeEffectNode(builder, route.ref, route.work.Global, statefile.bound)
	statefile.bound = true
	return node
}
