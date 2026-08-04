package aggregate

import (
	"fmt"
	"reflect"
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
		if codec.MaximumDocumentBytes() <= 0 {
			return CodecCatalog{}, fmt.Errorf("aggregate codec catalog[%d] has a non-positive document byte limit", index)
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
	if !ok || nilCodec(codec) || codec.ContractID() != contractID || codec.MaximumDocumentBytes() <= 0 {
		return nil, false
	}
	return codec, true
}

// ValidateContributionSet checks one complete render unit through its exact
// codec and verifies every subject's canonical topology correlation.
func (catalog CodecCatalog) ValidateContributionSet(set ContributionSet) error {
	canonical, err := NewContributionSet(set.Contributions())
	if err != nil {
		return err
	}
	for index, item := range canonical.Contributions() {
		if err := ValidateSubjectContract(item.SubjectID(), item.Contribution().Contract()); err != nil {
			return fmt.Errorf("aggregate contribution set[%d]: %w", index, err)
		}
	}
	codec, ok := catalog.Lookup(canonical.Contract().CodecContractID())
	if !ok {
		return fmt.Errorf(
			"aggregate codec contract %q is not admitted",
			canonical.Contract().CodecContractID(),
		)
	}
	return codec.ValidateContributions(canonical)
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
