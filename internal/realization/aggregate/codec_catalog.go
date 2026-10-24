package aggregate

import "fmt"

// CodecCatalog is an immutable collection of pure aggregate codec ports.
// Boundary composition supplies concrete implementations; inner consumers
// retain only this canonical capability.
type CodecCatalog struct {
	contributions ContributionCatalog
}

// NewCodecCatalog validates and owns one codec per contract identity.
func NewCodecCatalog(codecs []Codec) (CodecCatalog, error) {
	validators := make([]ContributionValidator, len(codecs))
	for index, codec := range codecs {
		validators[index] = codec
	}
	contributions, err := NewContributionCatalog(validators)
	if err != nil {
		return CodecCatalog{}, err
	}
	for index, codec := range codecs {
		if codec.MaximumDocumentBytes() <= 0 {
			return CodecCatalog{}, fmt.Errorf("aggregate codec catalog[%d] has a non-positive document byte limit", index)
		}
	}
	return CodecCatalog{contributions: contributions}, nil
}

// Lookup returns the codec implementing contractID.
func (catalog CodecCatalog) Lookup(contractID CodecContractID) (Codec, bool) {
	validator, admitted := catalog.contributions.Lookup(contractID)
	if !admitted {
		return nil, false
	}
	codec, ok := validator.(Codec)
	if !ok || codec.MaximumDocumentBytes() <= 0 {
		return nil, false
	}
	return codec, true
}

// ValidateContributionSet checks one complete render unit through its exact
// codec and verifies every subject's canonical topology correlation.
func (catalog CodecCatalog) ValidateContributionSet(set ContributionSet) error {
	return validateContributionSet(set, func(id CodecContractID) (ContributionValidator, bool) {
		return catalog.Lookup(id)
	})
}

// Contributions exposes the same admitted ports without document operations.
func (catalog CodecCatalog) Contributions() ContributionCatalog {
	return catalog.contributions
}
