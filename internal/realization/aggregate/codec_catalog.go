package aggregate

import (
	"fmt"
	"reflect"

	"github.com/isty2e/daem/internal/topology"
)

// CodecCatalog is an immutable collection of pure aggregate codec ports.
// Boundary composition supplies concrete implementations; inner consumers
// retain only this canonical capability.
type CodecCatalog struct {
	byContract map[CodecContractID]Codec
}

// NewCodecCatalog validates and owns one codec per contract identity.
func NewCodecCatalog(codecs []Codec) (CodecCatalog, error) {
	if len(codecs) == 0 {
		return CodecCatalog{}, fmt.Errorf("aggregate codec catalog is required")
	}
	byContract := make(map[CodecContractID]Codec, len(codecs))
	for index, codec := range codecs {
		if nilCodec(codec) {
			return CodecCatalog{}, fmt.Errorf("aggregate codec catalog[%d] is nil", index)
		}
		contractID := codec.ContractID()
		if contractID == "" {
			return CodecCatalog{}, fmt.Errorf("aggregate codec catalog[%d] has an empty contract identity", index)
		}
		if _, duplicate := byContract[contractID]; duplicate {
			return CodecCatalog{}, fmt.Errorf("aggregate codec contract %q is registered more than once", contractID)
		}
		byContract[contractID] = codec
	}
	return CodecCatalog{byContract: byContract}, nil
}

// Lookup returns the codec implementing contractID.
func (catalog CodecCatalog) Lookup(contractID CodecContractID) (Codec, bool) {
	if contractID == "" {
		return nil, false
	}
	codec, ok := catalog.byContract[contractID]
	if !ok || nilCodec(codec) || codec.ContractID() != contractID {
		return nil, false
	}
	return codec, true
}

// ValidateSubjectContribution checks one opaque contribution through its exact
// codec and verifies its canonical topology correlation.
func (catalog CodecCatalog) ValidateSubjectContribution(
	subject topology.SubjectID,
	contribution ManagedContribution,
) error {
	if err := contribution.Validate(); err != nil {
		return err
	}
	if err := ValidateSubjectContract(subject, contribution.Contract()); err != nil {
		return err
	}
	codec, ok := catalog.Lookup(contribution.CodecContractID())
	if !ok {
		return fmt.Errorf(
			"aggregate codec contract %q is not admitted",
			contribution.CodecContractID(),
		)
	}
	return codec.ValidateContribution(contribution)
}

func nilCodec(codec Codec) bool {
	if codec == nil {
		return true
	}
	value := reflect.ValueOf(codec)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
