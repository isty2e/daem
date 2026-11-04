package aggregate

import "cmp"

// DocumentCodecKey identifies a logical codec observation owner, not a complete
// selected frontier, physical file or runtime capability.
type DocumentCodecKey struct {
	document DocumentAddress
	codec    CodecContractID
}

func NewDocumentCodecKey(document DocumentAddress, codec CodecContractID) (DocumentCodecKey, error) {
	key := DocumentCodecKey{document: document, codec: codec}
	if err := key.Validate(); err != nil {
		return DocumentCodecKey{}, err
	}
	return key, nil
}

func (key DocumentCodecKey) Validate() error {
	if err := key.document.Validate(); err != nil {
		return err
	}
	return validateToken("aggregate codec contract id", string(key.codec))
}

func (key DocumentCodecKey) DocumentAddress() DocumentAddress { return key.document }
func (key DocumentCodecKey) CodecContractID() CodecContractID { return key.codec }

func (key DocumentCodecKey) Compare(other DocumentCodecKey) int {
	for _, order := range []int{
		cmp.Compare(key.document.Target(), other.document.Target()),
		cmp.Compare(key.document.Scope(), other.document.Scope()),
		cmp.Compare(key.document.AggregateRoot().String(), other.document.AggregateRoot().String()),
		cmp.Compare(key.codec, other.codec),
	} {
		if order != 0 {
			return order
		}
	}
	return 0
}

func (contract ProjectionContract) DocumentCodecKey() DocumentCodecKey {
	return DocumentCodecKey{document: contract.address.document, codec: contract.codecContractID}
}

func (selection Selection) DocumentCodecKey() DocumentCodecKey {
	if len(selection.contracts) == 0 {
		return DocumentCodecKey{}
	}
	return selection.contracts[0].DocumentCodecKey()
}
