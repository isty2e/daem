package profile

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// NativeAggregateOperationContract binds a selected native codec to one logical
// host placement. Its required observations are declarations, not observed facts
// or filesystem, publication or recovery authority.
type NativeAggregateOperationContract struct {
	selection aggregate.Selection
	placement configuration.Placement
	codec     aggregate.Codec
}

func NewNativeAggregateOperationContract(catalog aggregate.CodecCatalog, selection aggregate.Selection) (NativeAggregateOperationContract, error) {
	placement, err := nativeAggregateOperationPlacement(selection)
	if err != nil {
		return NativeAggregateOperationContract{}, err
	}
	codec, admitted := catalog.Lookup(selection.CodecContractID())
	if !admitted {
		return NativeAggregateOperationContract{}, fmt.Errorf("native aggregate operation requires its selected codec")
	}
	return NativeAggregateOperationContract{selection: selection, placement: placement, codec: codec}, nil
}

func nativeAggregateOperationPlacement(selection aggregate.Selection) (configuration.Placement, error) {
	if _, native := aggregate.HostConfigSyntaxForCodec(selection.CodecContractID()); !native {
		return configuration.Placement{}, fmt.Errorf("native aggregate operation requires a native selection")
	}
	contracts := selection.Contracts()
	if len(contracts) == 0 {
		return configuration.Placement{}, fmt.Errorf("native aggregate operation requires a nonempty selection")
	}
	var expected configuration.Placement
	for index, contract := range contracts {
		placement, err := HostConfigPlacementForContract(contract)
		if err != nil {
			return configuration.Placement{}, fmt.Errorf("native aggregate operation contract[%d]: %w", index, err)
		}
		if index != 0 && placement != expected {
			return configuration.Placement{}, fmt.Errorf("native aggregate operation mixes host placements")
		}
		expected = placement
	}
	return expected, nil
}

func (operation NativeAggregateOperationContract) Validate() error {
	placement, err := nativeAggregateOperationPlacement(operation.selection)
	if err != nil {
		return err
	}
	if placement != operation.placement || operation.codec == nil ||
		operation.codec.ContractID() != operation.selection.CodecContractID() ||
		operation.codec.MaximumDocumentBytes() <= 0 {
		return fmt.Errorf("native aggregate operation differs from its bound profile or codec")
	}
	return nil
}

func (operation NativeAggregateOperationContract) ValidateSelection(selection aggregate.Selection) error {
	if err := operation.Validate(); err != nil {
		return err
	}
	expected, actual := operation.selection.Contracts(), selection.Contracts()
	if len(expected) != len(actual) {
		return fmt.Errorf("native aggregate operation selection differs from its bound contracts")
	}
	for index, contract := range expected {
		if !contract.Equal(actual[index]) {
			return fmt.Errorf("native aggregate operation selection differs from its bound contracts")
		}
	}
	return nil
}

func (operation NativeAggregateOperationContract) Selection() aggregate.Selection {
	return operation.selection
}

func (operation NativeAggregateOperationContract) Codec() aggregate.Codec {
	return operation.codec
}

func (operation NativeAggregateOperationContract) Placement() configuration.Placement {
	return operation.placement
}

func (operation NativeAggregateOperationContract) RequiredObservations() []configuration.Observation {
	return operation.placement.RequiredObservations()
}
