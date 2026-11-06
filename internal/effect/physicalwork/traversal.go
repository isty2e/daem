package physicalwork

import "fmt"

// TraversalPhase routes retained capabilities to their admitted control and host
// allocations. Advancing the phase grants no capacity of its own.
type TraversalPhase struct {
	control PathWorkBudget
	host    PathWorkBudget
	sealed  bool
}

func NewTraversalPhase(initial PathWorkBudget) (*TraversalPhase, error) {
	if initial == nil {
		return nil, fmt.Errorf("initial physical traversal budget is required")
	}
	return &TraversalPhase{control: initial, host: initial}, nil
}

func (phase *TraversalPhase) Validate() error {
	if phase == nil || phase.control == nil || phase.host == nil {
		return fmt.Errorf("physical traversal phase is unavailable")
	}
	return nil
}

func (phase *TraversalPhase) Executing() bool {
	return phase != nil && phase.sealed
}

func (phase *TraversalPhase) AdmitPathComponents(count int) error {
	if err := phase.Validate(); err != nil {
		return err
	}
	return phase.control.AdmitPathComponents(count)
}

// AdmitPhysicalWork preserves storage-owned work admission through the control cursor.
func (phase *TraversalPhase) AdmitPhysicalWork(paths int, entries int, bytes int64) error {
	if err := phase.Validate(); err != nil {
		return err
	}
	return admitTraversalWork(phase.control, paths, entries, bytes)
}

func (phase *TraversalPhase) Advance(control, host PathWorkBudget) error {
	if err := phase.Validate(); err != nil {
		return err
	}
	if phase.sealed {
		return fmt.Errorf("physical traversal phase was already advanced")
	}
	if control == nil || host == nil {
		return fmt.Errorf("next physical traversal budget is required")
	}
	phase.control, phase.host, phase.sealed = control, host, true
	return nil
}

// Host returns a work view that follows the same phase transition as the control cursor.
func (phase *TraversalPhase) Host() PathWorkBudget {
	return hostTraversal{phase: phase}
}

type hostTraversal struct {
	phase *TraversalPhase
}

func (view hostTraversal) AdmitPathComponents(count int) error {
	if err := view.phase.Validate(); err != nil {
		return err
	}
	return view.phase.host.AdmitPathComponents(count)
}

func (view hostTraversal) AdmitPhysicalWork(paths int, entries int, bytes int64) error {
	if err := view.phase.Validate(); err != nil {
		return err
	}
	return admitTraversalWork(view.phase.host, paths, entries, bytes)
}

func admitTraversalWork(budget PathWorkBudget, paths int, entries int, bytes int64) error {
	if worker, ok := budget.(interface {
		AdmitPhysicalWork(int, int, int64) error
	}); ok {
		return worker.AdmitPhysicalWork(paths, entries, bytes)
	}
	if entries == 0 && bytes == 0 {
		return budget.AdmitPathComponents(paths)
	}
	return fmt.Errorf("physical traversal phase cannot admit entry or byte work")
}
