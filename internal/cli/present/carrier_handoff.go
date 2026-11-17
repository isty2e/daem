package clipresent

import (
	"fmt"
	"io"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/topology"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

type carrierHandoffsJSON struct {
	ConditionalAdoptions []carrierAdoptionActionJSON     `json:"conditional_carrier_adoption_actions,omitempty"`
	Basis                string                          `json:"basis"`
	Retirements          []carrierHandoffRetirementJSON  `json:"retirements"`
	Acquisitions         []carrierHandoffAcquisitionJSON `json:"acquisitions"`
	ConditionalOrders    []relationOrderJSON             `json:"conditional_order_actions,omitempty"`
	Requalified          *bool                           `json:"requalified,omitempty"`
	ApplyComplete        *bool                           `json:"apply_complete,omitempty"`
}

type carrierHandoffRetirementJSON struct {
	Before  carrierAbsenceActionJSON `json:"before"`
	Settled *bool                    `json:"settled,omitempty"`
}

type carrierHandoffAcquisitionJSON struct {
	After             relationActionJSON `json:"conditional_after"`
	RetirementIndexes []int              `json:"requires_retirements"`
}

func carrierHandoffsJSONFor(
	result reconcile.Result,
	progress *applyworkflow.CarrierHandoffProgress,
	adoptionPhase carrierAdoptionPhase,
	adoptionResults []durablecarrier.ManagedCarrierClaim,
) *carrierHandoffsJSON {
	expectation, present := result.CarrierHandoff()
	if !present {
		return nil
	}
	handoffs, after := expectation.Handoffs(), expectation.After()
	actions := handoffs.Retirements()
	retirements := carrierAbsenceJSONActions(actions)
	output := &carrierHandoffsJSON{Basis: "plan_time_conditional_remainder"}
	for index, action := range actions {
		retirement := carrierHandoffRetirementJSON{Before: retirements[index]}
		if progress != nil {
			settled := false
			for _, claim := range progress.RetiredClaims {
				settled = settled || claim.ExactEqual(action.Claim())
			}
			retirement.Settled = &settled
		}
		output.Retirements = append(output.Retirements, retirement)
	}
	for _, acquisition := range handoffs.Acquisitions() {
		for _, action := range after.Relations() {
			if action.Subject() != acquisition.Action().Subject() {
				continue
			}
			row := carrierHandoffAcquisitionJSON{After: relationJSONActions([]reconcile.RelationAction{action})[0]}
			for _, key := range acquisition.RetirementKeys() {
				for index, retirement := range actions {
					if retirement.Claim().FactKey() == key {
						row.RetirementIndexes = append(row.RetirementIndexes, index)
					}
				}
			}
			output.Acquisitions = append(output.Acquisitions, row)
		}
	}
	_, orders, adoptions := carrierHandoffAfterViews(expectation)
	output.ConditionalOrders = relationOrderJSONActions(orders, after.Relations())
	output.ConditionalAdoptions = carrierAdoptionJSONActionsWithResults(adoptions, adoptionPhase, adoptionResults)
	if progress != nil {
		requalified, complete := progress.Requalified, progress.Complete
		output.Requalified, output.ApplyComplete = &requalified, &complete
	}
	return output
}

// PrintCarrierHandoffsWithOptions discloses conditional work before confirmation;
// it does not turn the projected after observations into current host evidence.
func PrintCarrierHandoffsWithOptions(output io.Writer, result reconcile.Result, options HumanOptions) {
	expectation, present := result.CarrierHandoff()
	if !present {
		return
	}
	fmt.Fprintln(output, "carrier handoffs: retire the selected old carriers, then replan before acquisition")
	for _, retirement := range expectation.Handoffs().Retirements() {
		identity := retirement.Claim().Identity()
		source := carrierIdentityDisclosureFor(identity).sourceNamespace
		fmt.Fprintf(output, "  retire extension=%q target=%s scope=%s source=%q\n", identity.RelationSubject().Key(), identity.Target(), identity.Scope(), source.Value())
	}
	PrintCarrierAbsenceActionsWithOptions(output, expectation.Handoffs().Retirements(), options)
	fmt.Fprintln(output, "conditional after retirement (not current host observations or order consent):")
	relations, orders, adoptions := carrierHandoffAfterViews(expectation)
	for _, relation := range relations {
		source := carrierIdentityDisclosureFor(relation.CarrierIdentity()).sourceNamespace
		fmt.Fprintf(output, "  acquire extension=%q target=%s scope=%s source=%q\n", relation.Subject().Key(), relation.Target(), relation.Scope(), source.Value())
	}
	PrintRelationActionsWithOptions(output, relations, options)
	PrintRelationOrderActionsWithOptions(output, orders, options)
	PrintCarrierAdoptionActionsWithOptions(output, adoptions, options)
}

func carrierHandoffAfterViews(expectation reconcile.CarrierHandoffExpectation) ([]reconcile.RelationAction, []reconcile.RelationOrderDecision, []carrieradoption.Action) {
	subjects := make(map[topology.SubjectID]bool)
	for _, acquisition := range expectation.Handoffs().Acquisitions() {
		subjects[acquisition.Action().Subject()] = true
	}
	after := expectation.After()
	var relations []reconcile.RelationAction
	for _, action := range after.Relations() {
		if subjects[action.Subject()] {
			relations = append(relations, action)
		}
	}
	var adoptions []carrieradoption.Action
	for _, action := range after.CarrierAdoptions() {
		if subjects[action.Subject()] {
			adoptions = append(adoptions, action)
		}
	}
	var orders []reconcile.RelationOrderDecision
	for _, order := range after.RelationOrders() {
		for _, retirement := range expectation.Handoffs().Retirements() {
			if order.Target() == retirement.Target() && order.Scope() == retirement.Scope() {
				orders = append(orders, order)
				break
			}
		}
	}
	return relations, orders, adoptions
}

func PrintCarrierReconciliationWithOptions(output io.Writer, result reconcile.Result, options HumanOptions) {
	relations, orders := result.Relations(), result.RelationOrders()
	adoptions, absences := result.CarrierAdoptions(), result.CarrierAbsences()
	if expectation, present := result.CarrierHandoff(); present {
		subjects := make(map[topology.SubjectID]bool)
		keys := make(map[durablecarrier.CarrierFactKey]bool)
		for _, acquisition := range expectation.Handoffs().Acquisitions() {
			subjects[acquisition.Action().Subject()] = true
		}
		for _, retirement := range expectation.Handoffs().Retirements() {
			keys[retirement.Claim().FactKey()] = true
		}
		var unabsorbed []reconcile.RelationAction
		for _, action := range relations {
			if !subjects[action.Subject()] {
				unabsorbed = append(unabsorbed, action)
			}
		}
		relations = unabsorbed
		var unabsorbedAdoptions []carrieradoption.Action
		for _, action := range adoptions {
			if !subjects[action.Subject()] {
				unabsorbedAdoptions = append(unabsorbedAdoptions, action)
			}
		}
		adoptions = unabsorbedAdoptions
		var ordinaryAbsences []carrierabsence.Action
		for _, action := range absences {
			if !keys[action.Claim().FactKey()] {
				ordinaryAbsences = append(ordinaryAbsences, action)
			}
		}
		absences = ordinaryAbsences
	}
	PrintRelationActionsWithOptions(output, relations, options)
	PrintRelationOrderActionsWithOptions(output, orders, options)
	PrintCarrierAdoptionActionsWithOptions(output, adoptions, options)
	PrintCarrierAbsenceActionsWithOptions(output, absences, options)
	PrintCarrierHandoffsWithOptions(output, result, options)
}

func PrintCarrierHandoffProgress(output io.Writer, result reconcile.Result, progress *applyworkflow.CarrierHandoffProgress) {
	expectation, present := result.CarrierHandoff()
	if !present || progress == nil {
		return
	}
	fmt.Fprintf(output, "handoff progress: %d/%d retirements settled; fresh replan=%t; apply complete=%t\n", len(progress.RetiredClaims), len(expectation.Handoffs().Retirements()), progress.Requalified, progress.Complete)
}

func PrintCarrierReconciliationAdoptionResultsWithOptions(
	output io.Writer,
	result reconcile.Result,
	receipts []durablecarrier.ManagedCarrierClaim,
	executionErr error,
	executionAttempted bool,
	options HumanOptions,
) {
	actions := result.CarrierAdoptions()
	if expectation, present := result.CarrierHandoff(); present {
		actions = expectation.After().CarrierAdoptions()
	}
	PrintCarrierAdoptionResultsWithOptions(output, actions, receipts, executionErr, executionAttempted, options)
}
