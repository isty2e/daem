package projection

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type NativeWholeAdoptionInput struct {
	Source                 lock.LockedSubjectContract
	Before                 durable.Snapshot
	Current                observe.ManagedPathEvidence
	Ownership              observe.NativeDocumentClaimObservation
	ManageUnmanagedMatches bool
}

// NativeWholeAdoptionPlan keeps a source-bound Record and its state-only key
// retirements together. Blocked classification never consumes prior subjects.
type NativeWholeAdoptionPlan struct {
	decision    reconcile.ManagedPathDecision
	retired     []topology.SubjectID
	initialized bool
}

func (plan NativeWholeAdoptionPlan) Validate() error {
	if !plan.initialized {
		return fmt.Errorf("native whole adoption plan is required")
	}
	return nil
}

func (plan NativeWholeAdoptionPlan) Decision() reconcile.ManagedPathDecision { return plan.decision }

func (plan NativeWholeAdoptionPlan) RetiredSubjects() []topology.SubjectID {
	return slices.Clone(plan.retired)
}

func BuildNativeWholeAdoption(ctx context.Context, input NativeWholeAdoptionInput) (NativeWholeAdoptionPlan, error) {
	if ctx == nil {
		return NativeWholeAdoptionPlan{}, fmt.Errorf("native whole adoption context is required")
	}
	if err := ctx.Err(); err != nil {
		return NativeWholeAdoptionPlan{}, err
	}
	if err := input.Ownership.Validate(); err != nil {
		return NativeWholeAdoptionPlan{}, err
	}
	placement, native := input.Source.HostConfigPlacement()
	materialized, supplied := input.Source.MaterializedFileIdentity()
	fileUse, file := input.Source.ExactFileUse()
	if !native || !supplied || !file || fileUse.Scope() != placement.Scope() {
		return NativeWholeAdoptionPlan{}, fmt.Errorf("native whole adoption requires its actual whole-file source")
	}
	projection, err := lock.NewHostConfigDocumentSubjectContract(input.Source.EntityID(), placement)
	if err != nil {
		return NativeWholeAdoptionPlan{}, err
	}
	expected, err := NewManagedPathExpectation(projection)
	if err != nil {
		return NativeWholeAdoptionPlan{}, err
	}
	facts := expected.decisionInput()
	facts.DesiredHash = materialized.ContentHash()
	facts.DesiredFileMode = managedFileDesiredMode(facts.PermissionPolicy, facts.DesiredFileMode, fileUse.Executable())
	facts.LiveHash, facts.LiveFileMode = input.Current.ContentHash(), input.Current.FileMode()
	if input.Current.Subject() != facts.Subject || input.Current.Destination() != facts.Destination {
		return NativeWholeAdoptionPlan{}, fmt.Errorf("native whole source and current document are not correlated")
	}
	document, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
	if err != nil {
		return NativeWholeAdoptionPlan{}, err
	}
	if !slices.Contains(input.Ownership.Aliases(), document) {
		return NativeWholeAdoptionPlan{}, fmt.Errorf("native whole source has no observed document alias")
	}
	block := func(reason reconcile.ActionReason, detail string) (NativeWholeAdoptionPlan, error) {
		decision, err := newManagedPathBlocked(facts, reason, detail).canonical()
		if err != nil {
			return NativeWholeAdoptionPlan{}, err
		}
		if err := ctx.Err(); err != nil {
			return NativeWholeAdoptionPlan{}, err
		}
		return NativeWholeAdoptionPlan{decision: decision, initialized: true}, nil
	}
	if !input.Current.Exists() {
		return block(reconcile.ReasonDriftedOutput, "native whole adoption requires an existing document; it cannot create a replacement")
	}
	rows := make(map[topology.SubjectID]durable.ManagedAggregateState)
	for _, row := range input.Before.ManagedAggregates() {
		if err := ctx.Err(); err != nil {
			return NativeWholeAdoptionPlan{}, err
		}
		rows[row.Subject()] = row
	}
	retired := input.Ownership.BeforeSubjects()
	footprints := make(map[output.Footprint]struct{})
	for _, subject := range retired {
		if err := ctx.Err(); err != nil {
			return NativeWholeAdoptionPlan{}, err
		}
		row, present := rows[subject]
		owner, _, key := topologyprojection.HostConfigKeyIdentity(subject)
		if !present || !key || owner != input.Source.EntityID() || row.Contribution().Contract().Address().Document() != document {
			return block(reconcile.ReasonOwnershipStateConflict, "native whole adoption would consume another declaration or document participant")
		}
		if document.Scope() == target.ScopeGlobal {
			footprints[row.Contribution().Footprint()] = struct{}{}
		}
	}
	for _, binding := range input.Ownership.CurrentBindings() {
		if !binding.Owner().Equal(input.Ownership.Owner()) || binding.Destination() != document.AggregateRoot() {
			return block(reconcile.ReasonOwnershipConflict, "native whole adoption overlaps another stored document participant")
		}
	}
	before := input.Ownership.BeforeClaims()
	if len(before) != len(footprints) {
		return block(reconcile.ReasonOwnershipClaimMissing, "native whole adoption lacks the exact retiring claim fragment")
	}
	for _, claim := range before {
		if _, present := footprints[claim.Address().Footprint()]; !present {
			return block(reconcile.ReasonOwnershipStateConflict, "native whole adoption claims differ from its retiring keys")
		}
	}
	for _, claim := range input.Ownership.CurrentClaims() {
		if !claim.OwnedBy(input.Ownership.Owner()) || claim.State() != ownership.ClaimActive || !slices.ContainsFunc(before, claim.Equal) {
			return block(reconcile.ReasonOwnershipConflict, "native whole adoption overlaps ownership outside its active retiring fragment")
		}
	}
	decision, err := reconcileManagedPathDesired(facts, input.Current, durable.ManagedPathState{}, false, nil, input.ManageUnmanagedMatches).canonical()
	if err != nil {
		return NativeWholeAdoptionPlan{}, err
	}
	if decision.Kind() != reconcile.ManagedPathRecord && !decision.IsBlocked() {
		return NativeWholeAdoptionPlan{}, fmt.Errorf("native widening cannot become a host mutation")
	}
	if decision.Kind() != reconcile.ManagedPathRecord {
		retired = nil
	}
	if err := ctx.Err(); err != nil {
		return NativeWholeAdoptionPlan{}, err
	}
	return NativeWholeAdoptionPlan{decision: decision, retired: retired, initialized: true}, nil
}
