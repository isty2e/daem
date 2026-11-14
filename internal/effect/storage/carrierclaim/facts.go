package carrierclaim

import durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"

// DecodeRegistryFacts reads strict default or supported retired registry bytes
// without observing owner keys. Conversion must qualify independent access
// evidence; these facts do not replace a normal Store's live guards.
func DecodeRegistryFacts(content []byte) (durablecarrier.GlobalCarrierClaims, error) {
	return decode(content)
}
