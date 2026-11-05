package document

import (
	"fmt"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

// Participant admits the same selected rewrite areas for forward publication
// and native journal reconstruction.
func Participant(selection aggregate.Selection, codecs aggregate.CodecCatalog) (compose.Participant, error) {
	codec, found := codecs.Lookup(selection.CodecContractID())
	if !found {
		return compose.Participant{}, fmt.Errorf("document selection codec is not admitted by the operation")
	}
	var syntax configdocument.Syntax
	var areas []output.KeyPath
	if grammar, native := aggregate.HostConfigSyntaxForCodec(selection.CodecContractID()); native {
		var err error
		syntax, err = configdocument.ParseSyntax(grammar)
		if err != nil {
			return compose.Participant{}, err
		}
		for _, contract := range selection.Contracts() {
			if _, err := profile.HostConfigPlacementForContract(contract); err != nil {
				return compose.Participant{}, err
			}
			areas = append(areas, contract.Address().KeyPath())
		}
	} else {
		address := selection.DocumentAddress()
		if address.Target() != target.TargetPi && address.Target() != target.TargetCodex {
			return compose.Participant{}, fmt.Errorf("native document composition requires an admitted Pi or Codex participant")
		}
		for _, contract := range selection.Contracts() {
			placement, found := aggregate.MCPPlacementForCodec(aggregate.MCPPlacementID(contract.Address().PlacementID()), contract.CodecContractID())
			if !found || placement.Target() != address.Target() || placement.Scope() != address.Scope() || placement.ConfigPath() != address.AggregateRoot() {
				return compose.Participant{}, fmt.Errorf("document MCP selection differs from its admitted placement")
			}
			footprint, err := output.FootprintFromLegacy(output.ContentPath(placement.ContentPathPrefix()))
			if err != nil {
				return compose.Participant{}, err
			}
			area, key := footprint.Key()
			if !key {
				return compose.Participant{}, fmt.Errorf("MCP rewrite area must be a selected key")
			}
			areas = append(areas, area)
		}
		syntax = configdocument.JSON
		if address.Target() == target.TargetCodex {
			syntax = configdocument.TOML
		}
	}
	return compose.NewParticipant(codec, selection, syntax, output.TopmostKeyPaths(areas))
}
