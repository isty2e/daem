package publication

import (
	"context"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
)

func (captured CapturedNativeJournal) AssessRecovery(ctx context.Context, options journalqualification.NativeRecoveryOptions) (journalqualification.NativeRecoveryAssessment, error) {
	if err := captured.Validate(); err != nil {
		return journalqualification.NativeRecoveryAssessment{}, err
	}
	return journalqualification.ReassessNativeSource(ctx, captured.source, journalqualification.NativeReadOptions{
		Documents: journalqualification.ReadOptions{Filesystem: options.Filesystem, Budget: captured.budget, Limits: captured.publication.limits, Codecs: options.Codecs, OrdinaryCapabilities: captured.publication.ordinaryCapabilities},
		Roots:     captured.publication.roots, State: options.State, Registry: options.Registry, Resolver: captured.publication.resolver,
	}, options.OperationAuthority)
}
