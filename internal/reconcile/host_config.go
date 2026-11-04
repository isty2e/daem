package reconcile

import "github.com/isty2e/daem/internal/realization/aggregate"

func (result Result) ContainsHostConfig() bool {
	if len(result.nativeNarrowings) != 0 {
		return true
	}
	for _, decision := range result.managedPaths {
		if aggregate.IsHostConfigDocumentSubject(decision.Subject()) {
			return true
		}
	}
	for _, decision := range result.aggregates {
		if _, native := aggregate.HostConfigSyntaxForCodec(decision.CodecContractID()); native {
			return true
		}
	}
	return false
}
