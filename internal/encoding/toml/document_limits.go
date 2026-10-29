package tomlsyntax

import "fmt"

// DocumentLimits derives scanner capacity from an already byte-bounded document
// and its selected depth. It does not select the shared host-document policy.
func DocumentLimits(bytes int64, depth int) (Limits, error) {
	maximumInt := int64(int(^uint(0) >> 1))
	if bytes < 0 || depth <= 0 || bytes >= maximumInt || int64(depth) > maximumInt/2-2 {
		return Limits{}, fmt.Errorf("TOML document bounds are not representable")
	}
	units := bytes + 1
	factor := int64(depth+2) * 2
	if units > maximumInt/factor {
		return Limits{}, fmt.Errorf("TOML document work bound overflows")
	}
	return Limits{
		MaximumDepth: depth, MaximumContainers: int(units), MaximumKeyBytes: int(units),
		MaximumWork: int(units * factor), MaximumPathWork: int(units * factor),
	}, nil
}
