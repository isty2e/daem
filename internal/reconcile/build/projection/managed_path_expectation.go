package projection

import (
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// ManagedPathExpectation is manifest-derived expected lock membership for one
// canonical entity-backed path projection. It contains no Supply or current fact.
type ManagedPathExpectation struct {
	contract lock.LockedSubjectContract
}

// NewManagedPathExpectation admits one canonical projection contract as
// expected lock membership.
func NewManagedPathExpectation(contract lock.LockedSubjectContract) (ManagedPathExpectation, error) {
	realization, ok := contract.Realization()
	if !ok {
		return ManagedPathExpectation{}, fmt.Errorf("managed path expectation requires realization")
	}
	_, ok = realization.ManagedPathProjection()
	if !ok {
		return ManagedPathExpectation{}, fmt.Errorf("managed path expectation requires managed path projection")
	}
	if _, entityBacked := topologyprojection.EntityID(contract.SubjectID()); !entityBacked {
		return ManagedPathExpectation{}, fmt.Errorf("managed path expectation requires entity-backed projection subject")
	}
	return ManagedPathExpectation{contract: contract}, nil
}

func (expectation ManagedPathExpectation) decisionInput() reconcile.ManagedPathDecisionInput {
	realization, _ := expectation.contract.Realization()
	projection, _ := realization.ManagedPathProjection()
	return reconcile.ManagedPathDecisionInput{
		Subject: expectation.contract.SubjectID(), ConsumerTargets: projection.ConsumerTargets(),
		Scope: projection.Scope(), Destination: output.Destination(projection.Destination()),
		ContentKind: projection.ContentKind(), PlacementMode: projection.PlacementMode(),
		PermissionPolicy: projection.PermissionPolicy(), DesiredFileMode: managedPathProjectionExactMode(projection),
	}
}

// Subject returns the expected projection identity.
func (expectation ManagedPathExpectation) Subject() topology.SubjectID {
	return expectation.subject()
}

func managedPathProjectionExactMode(projection realization.ManagedPathProjection) os.FileMode {
	exactMode, present := projection.ExactPermissionMode()
	if !present {
		return 0
	}
	return exactMode.FileMode()
}

func (expectation ManagedPathExpectation) subject() topology.SubjectID {
	return expectation.contract.SubjectID()
}

func (expectation ManagedPathExpectation) matches(contract lock.LockedSubjectContract) bool {
	return expectation.contract.Equal(contract)
}

func managedPathExpectationIndex(values []ManagedPathExpectation) (map[topology.SubjectID]ManagedPathExpectation, error) {
	result := make(map[topology.SubjectID]ManagedPathExpectation, len(values))
	for index, value := range values {
		subject := value.subject()
		if subject.IsZero() {
			return nil, fmt.Errorf("managed path expectation[%d] is invalid", index)
		}
		if _, duplicate := result[subject]; duplicate {
			return nil, fmt.Errorf("duplicate managed path expectation[%d] for subject %q", index, subject)
		}
		result[subject] = value
	}
	return result, nil
}
