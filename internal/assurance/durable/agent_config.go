package durable

import "github.com/isty2e/daem/internal/realization/aggregate"

func (snapshot Snapshot) ContainsAgentConfig() bool {
	for _, state := range snapshot.managedPaths {
		if aggregate.IsAgentConfigDocumentSubject(state.Subject()) {
			return true
		}
	}
	for _, state := range snapshot.managedAggregates {
		if _, native := aggregate.AgentConfigSyntaxForCodec(state.Contribution().Contract().CodecContractID()); native {
			return true
		}
	}
	return false
}
