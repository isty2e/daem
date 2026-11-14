package document

import (
	"fmt"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/realization/profile"
)

// Participant admits the same selected rewrite areas for forward publication
// and native journal reconstruction.
func Participant(selection aggregate.Selection, codecs aggregate.CodecCatalog) (compose.Participant, error) {
	codec, found := codecs.Lookup(selection.CodecContractID())
	if !found {
		return compose.Participant{}, fmt.Errorf("document selection codec is not admitted by the operation")
	}
	if _, native := aggregate.HostConfigSyntaxForCodec(selection.CodecContractID()); native {
		for _, contract := range selection.Contracts() {
			if _, err := profile.HostConfigPlacementForContract(contract); err != nil {
				return compose.Participant{}, err
			}
		}
	}
	grammar, areas, err := codec.RewriteLayout(selection)
	if err != nil {
		return compose.Participant{}, err
	}
	syntax, err := configdocument.ParseSyntax(grammar)
	if err != nil {
		return compose.Participant{}, err
	}
	return compose.NewParticipant(codec, selection, syntax, areas)
}
