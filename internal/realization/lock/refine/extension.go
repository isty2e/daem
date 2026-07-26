package refine

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// Extensions refines desired extension relations into canonical locked contracts.
func Extensions(extensions []extension.Extension) ([]lock.LockedSubjectContract, error) {
	if len(extensions) == 0 {
		return nil, nil
	}
	model, err := extensiontopology.Lower(extensions)
	if err != nil {
		return nil, err
	}
	graph := model.Graph()

	contracts := make([]lock.LockedSubjectContract, 0, len(extensions))
	for index, value := range extensions {
		contract, err := extensionLockedSubjectContract(graph, value)
		if err != nil {
			return nil, fmt.Errorf("extension[%d]: %w", index, err)
		}
		contracts = append(contracts, contract)
	}
	sortLockedSubjectContracts(contracts)
	return contracts, nil
}

func extensionLockedSubjectContract(
	graph topology.Graph,
	value extension.Extension,
) (lock.LockedSubjectContract, error) {
	if err := value.Validate(); err != nil {
		return lock.LockedSubjectContract{}, err
	}
	subjectID, err := extensiontopology.Relation(value)
	if err != nil {
		return lock.LockedSubjectContract{}, err
	}
	if !graph.Contains(subjectID) {
		return lock.LockedSubjectContract{}, fmt.Errorf("extension topology is missing relation subject %q", subjectID)
	}
	relationKey, err := extensiontopology.HostVisibleRelationKey(value.CarrierKey())
	if err != nil {
		return lock.LockedSubjectContract{}, err
	}
	subjectKey, err := hostrelation.NewSubjectKey(relationKey)
	if err != nil {
		return lock.LockedSubjectContract{}, err
	}
	return lock.NewDelegatedRelationCarrierContract(
		value.ID(),
		value.CarrierKey(),
		subjectID,
		subjectKey,
	)
}
