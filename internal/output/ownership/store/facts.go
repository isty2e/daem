package store

import "github.com/isty2e/daem/internal/output/ownership"

// DecodeRegistryFacts reads strict default or supported legacy registry bytes
// without observing recorded path keys. Conversion must qualify independent
// access evidence; these facts do not replace a normal Store's live guards.
func DecodeRegistryFacts(content []byte) (ownership.Registry, error) {
	return decode(content)
}
