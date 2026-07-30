package configrelation

import (
	"context"
	"fmt"

	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
)

// OrderSequenceEventKind identifies one physical-sequence execution transition.
type OrderSequenceEventKind string

const (
	OrderSequenceStarted OrderSequenceEventKind = "started"
	OrderSequenceDone    OrderSequenceEventKind = "done"
	OrderSequenceFailed  OrderSequenceEventKind = "failed"
)

// OrderSequenceEvent is one exact physical-sequence mutation outcome.
type OrderSequenceEvent struct {
	Kind       OrderSequenceEventKind
	SequenceID hostrelation.PhysicalSequenceID
	Changed    bool
	Err        error
}

// OrderSequenceEventSink observes physical-sequence execution. Nil is a no-op.
type OrderSequenceEventSink func(OrderSequenceEvent)

func (sink OrderSequenceEventSink) emit(event OrderSequenceEvent) {
	if sink != nil {
		sink(event)
	}
}

// OrderPlan is the host-neutral effect boundary for one freshly observed
// extension-order class. Host-specific plan selection remains private to this
// admitted specialization package.
type OrderPlan struct {
	pi       *PiOrderPlan
	openCode *OpenCodeOrderPlan
}

// NewOrderPlan converts exactly one fresh host observation into its direct
// mutation plan.
func NewOrderPlan(observation relationhost.OrderObservation) (OrderPlan, error) {
	piObservation, hasPi := observation.Pi()
	openCodeObservation, hasOpenCode := observation.OpenCode()
	if hasPi == hasOpenCode {
		return OrderPlan{}, fmt.Errorf(
			"extension order observation requires exactly one host-native value",
		)
	}
	if hasPi {
		plan, err := NewPiOrderPlan(piObservation)
		if err != nil {
			return OrderPlan{}, err
		}
		return OrderPlan{pi: &plan}, nil
	}
	plan, err := NewOpenCodeOrderPlan(openCodeObservation)
	if err != nil {
		return OrderPlan{}, err
	}
	return OrderPlan{openCode: &plan}, nil
}

// PhysicalAuthority returns every exact path the selected host plan may mutate.
func (plan OrderPlan) PhysicalAuthority() (mutation.PhysicalAuthoritySet, error) {
	switch {
	case plan.pi != nil && plan.openCode == nil:
		return plan.pi.PhysicalAuthority()
	case plan.openCode != nil && plan.pi == nil:
		return plan.openCode.PhysicalAuthority()
	default:
		return mutation.PhysicalAuthoritySet{}, fmt.Errorf(
			"extension order plan is incomplete",
		)
	}
}

// Bind captures exact rooted entry authority for the selected host plan.
func (plan OrderPlan) Bind(
	selectedRoot *rootedpath.CapturedRoot,
	selectedRootPath string,
) (*BoundOrder, error) {
	switch {
	case plan.pi != nil && plan.openCode == nil:
		bound, err := plan.pi.Bind(selectedRoot, selectedRootPath)
		if err != nil {
			return nil, err
		}
		return &BoundOrder{pi: bound}, nil
	case plan.openCode != nil && plan.pi == nil:
		bound, err := plan.openCode.Bind(selectedRoot, selectedRootPath)
		if err != nil {
			return nil, err
		}
		return &BoundOrder{openCode: bound}, nil
	default:
		return nil, fmt.Errorf("extension order plan is incomplete")
	}
}

// BoundOrder owns one closed host-specific binding behind a host-neutral
// execution surface.
type BoundOrder struct {
	pi       *BoundPiOrder
	openCode *BoundOpenCodeOrder
}

// Execute converges physical sequences in their host-defined stable order.
func (order *BoundOrder) Execute(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	events OrderSequenceEventSink,
) (int, error) {
	if order == nil {
		return 0, fmt.Errorf("bound extension order is unavailable")
	}
	switch {
	case order.pi != nil && order.openCode == nil:
		changed, err := order.pi.Execute(ctx, filesystem, events)
		if changed {
			return 1, err
		}
		return 0, err
	case order.openCode != nil && order.pi == nil:
		return order.openCode.Execute(ctx, filesystem, events)
	default:
		return 0, fmt.Errorf("bound extension order is incomplete")
	}
}

// Close releases every retained host entry authority.
func (order *BoundOrder) Close() error {
	if order == nil {
		return nil
	}
	switch {
	case order.pi != nil && order.openCode == nil:
		err := order.pi.Close()
		order.pi = nil
		return err
	case order.openCode != nil && order.pi == nil:
		err := order.openCode.Close()
		order.openCode = nil
		return err
	default:
		return fmt.Errorf("bound extension order is incomplete")
	}
}
