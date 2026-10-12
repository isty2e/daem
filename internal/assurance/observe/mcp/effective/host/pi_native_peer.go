package host

import (
	"fmt"

	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

// piNativePeer is operation-local coordination evidence, not write authority.
type piNativePeer struct {
	path        string
	definitions []aggregate.SubjectContribution
	retiring    bool
}

func nativePeerProjections(input Input) (map[topology.SubjectID]piNativePeer, error) {
	previous := make(map[topology.SubjectID]aggregate.SubjectContribution, len(input.Previous))
	for _, projection := range input.Previous {
		if projection.Contribution().CodecContractID() == aggregate.MCPCodecPiNativeStdio {
			previous[projection.SubjectID()] = projection
		}
	}
	peers := make(map[topology.SubjectID]piNativePeer)
	add := func(projection aggregate.SubjectContribution, retiring bool) error {
		contribution := projection.Contribution()
		if contribution.CodecContractID() != aggregate.MCPCodecPiNativeStdio {
			return nil
		}
		subject := projection.SubjectID()
		if _, duplicate := peers[subject]; duplicate {
			return fmt.Errorf("duplicate Native Pi peer projection for %q", subject)
		}
		path, err := input.ResolveDestination(contribution.AggregateRoot())
		if err != nil {
			return err
		}
		peer := piNativePeer{path: path, definitions: []aggregate.SubjectContribution{projection}, retiring: retiring}
		if baseline, present := previous[subject]; present && !retiring {
			peer.definitions = append(peer.definitions, baseline)
		}
		peers[subject] = peer
		return nil
	}
	for _, contract := range input.Contracts {
		projection, present, err := contract.ManagedAggregateContribution()
		if err != nil {
			return nil, err
		}
		if present {
			if err := add(projection, false); err != nil {
				return nil, err
			}
		}
	}
	for _, projection := range input.Retiring {
		if err := add(projection, true); err != nil {
			return nil, err
		}
	}
	return peers, nil
}

func (peer piNativePeer) matches(path string, content []byte, codec aggregate.Codec) bool {
	if peer.path != path {
		return false
	}
	for _, projection := range peer.definitions {
		contribution := projection.Contribution()
		selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
		if err == nil && compareNormalDefinition(content, contribution, selection, codec) == mcpeffective.DefinitionEquivalenceEquivalent {
			return true
		}
	}
	return false
}
