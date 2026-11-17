package reconcile

import "fmt"

// CarrierHandoffExpectation keeps actual before-actions separate from a
// prospective remainder. Neither observing it nor cloning it qualifies execution.
type CarrierHandoffExpectation struct {
	handoffs CarrierHandoffs
	after    Result
}

func NewCarrierHandoffExpectation(handoffs CarrierHandoffs, after Result) (CarrierHandoffExpectation, error) {
	if handoffs.Empty() {
		return CarrierHandoffExpectation{}, fmt.Errorf("carrier handoff expectation requires retirements")
	}
	if err := validateOperationContext(after.context); err != nil {
		return CarrierHandoffExpectation{}, err
	}
	if after.prospective || after.handoff != nil {
		return CarrierHandoffExpectation{}, fmt.Errorf("carrier handoff expectations cannot be nested or requalified without observation")
	}
	for _, acquisition := range handoffs.Acquisitions() {
		actual := acquisition.Action()
		matched := false
		for _, candidate := range after.relations {
			if candidate.Subject() == actual.Subject() &&
				candidate.CarrierIdentity().ExactEqual(actual.CarrierIdentity()) &&
				candidate.RouteRequest().Equal(actual.RouteRequest()) {
				matched = true
				break
			}
		}
		if !matched {
			return CarrierHandoffExpectation{}, fmt.Errorf("carrier handoff remainder changed or omitted an acquisition")
		}
	}
	for _, retirement := range handoffs.Retirements() {
		for _, absence := range after.absences {
			if absence.Claim().FactKey() == retirement.Claim().FactKey() {
				return CarrierHandoffExpectation{}, fmt.Errorf("carrier handoff remainder repeats a prefix retirement")
			}
		}
	}
	after = after.Clone()
	after.prospective = true
	return CarrierHandoffExpectation{handoffs: handoffs, after: after}, nil
}

func (expectation CarrierHandoffExpectation) Handoffs() CarrierHandoffs { return expectation.handoffs }

func (expectation CarrierHandoffExpectation) After() Result { return expectation.after.Clone() }

func (result Result) CarrierHandoff() (CarrierHandoffExpectation, bool) {
	if result.handoff == nil {
		return CarrierHandoffExpectation{}, false
	}
	return *result.handoff, true
}

// IsProspective distinguishes a reservation/preview remainder from current
// assessment. Execution requires re-observation, not clearing this role.
func (result Result) IsProspective() bool { return result.prospective }

func (result Result) WithCarrierHandoff(expectation CarrierHandoffExpectation) (Result, error) {
	if result.prospective {
		return Result{}, fmt.Errorf("prospective decisions cannot become a current handoff assessment")
	}
	return NewResult(ResultInput{
		Context: result.context, ManagedPaths: result.ManagedPaths(), Aggregates: result.Aggregates(),
		NativeNarrowings: result.NativeNarrowings(), Relations: result.Relations(), RelationOrders: result.RelationOrders(),
		CarrierAdoptions: result.CarrierAdoptions(), CarrierAbsences: result.CarrierAbsences(), Delegates: result.Delegates(),
		CarrierHandoff: &expectation,
	})
}

func validateCarrierHandoffBinding(result Result, expectation CarrierHandoffExpectation) error {
	if expectation.handoffs.Empty() || result.context != expectation.after.context {
		return fmt.Errorf("carrier handoff expectation has no matching operation context")
	}
	for _, retirement := range expectation.handoffs.Retirements() {
		matched := false
		for _, absence := range result.absences {
			if absence.Claim().ExactEqual(retirement.Claim()) && absence.Decision() == retirement.Decision() {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("carrier handoff result omitted a current retirement")
		}
	}
	for _, acquisition := range expectation.handoffs.Acquisitions() {
		actual := acquisition.Action()
		matched := false
		for _, action := range result.relations {
			if action.Subject() == actual.Subject() && action.CarrierIdentity().ExactEqual(actual.CarrierIdentity()) &&
				action.RouteRequest().Equal(actual.RouteRequest()) && action.kind == actual.kind &&
				action.execution == actual.execution && action.correlation.State() == actual.correlation.State() {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("carrier handoff result changed a current acquisition observation")
		}
	}
	return nil
}
