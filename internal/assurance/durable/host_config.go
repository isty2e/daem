package durable

import "github.com/isty2e/daem/internal/realization/aggregate"

func (snapshot Snapshot) ContainsHostConfig() bool {
	for _, state := range snapshot.managedPaths {
		if aggregate.IsHostConfigDocumentSubject(state.Subject()) {
			return true
		}
	}
	for _, state := range snapshot.managedAggregates {
		if _, native := aggregate.HostConfigSyntaxForCodec(state.Contribution().Contract().CodecContractID()); native {
			return true
		}
	}
	return false
}
