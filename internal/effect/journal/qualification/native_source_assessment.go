package qualification

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
)

// NativeRecoveryAssessment binds observed phases to an actual inventory source.
// It supplies no execution lease or permission to change host/ownership state.
type NativeRecoveryAssessment struct {
	source     journalinventory.ActiveJournal
	assessment Assessment
	valid      bool
}

// ReadNativeSourceAssessment retains a validated source on SelectedPhaseError
// only for blocked disclosure. It exposes no document facts or admitted phase.
func ReadNativeSourceAssessment(ctx context.Context, source journalinventory.ActiveJournal, options NativeReadOptions) (NativeRecoveryAssessment, error) {
	journal, native := source.Record().Native()
	if !native {
		return NativeRecoveryAssessment{}, fmt.Errorf("native recovery requires a native16 inventory record")
	}
	options.Directory, options.Active = source.OperationDir(), source.PhysicalAuthority()
	read := options.Documents
	if err := journalinventory.ValidateActiveJournalRoot(ctx, read.Filesystem, read.Artifacts, read.Budget, options.Active); err != nil {
		return NativeRecoveryAssessment{}, err
	}
	assessment, phaseErr := AssessNativeDocuments(journal, ctx, options)
	if phaseErr != nil {
		var phase *SelectedPhaseError
		if !errors.As(phaseErr, &phase) || ctx.Err() != nil {
			return NativeRecoveryAssessment{}, phaseErr
		}
	}
	snapshot, err := ReadJournalSnapshot(ctx, read.Artifacts, read.Filesystem, read.Budget)
	if err != nil {
		return NativeRecoveryAssessment{}, err
	}
	if journalrecord.Fingerprint(snapshot.Content()) != source.Identity().JournalAuthorityFingerprint() {
		return NativeRecoveryAssessment{}, fmt.Errorf("native recovery journal changed during qualification")
	}
	if err := journalinventory.ValidateActiveJournalRoot(ctx, read.Filesystem, read.Artifacts, read.Budget, options.Active); err != nil {
		return NativeRecoveryAssessment{}, err
	}
	return NativeRecoveryAssessment{source: source, assessment: assessment, valid: true}, phaseErr
}

func ReassessNativeSource(ctx context.Context, source journalinventory.ActiveJournal, options NativeReadOptions, entry *rootedpath.EntryAuthority) (result NativeRecoveryAssessment, resultErr error) {
	journal, native := source.Record().Native()
	if !native {
		return NativeRecoveryAssessment{}, fmt.Errorf("native recovery requires a native16 inventory record")
	}
	options.Directory, options.Active = source.OperationDir(), source.PhysicalAuthority()
	read := options.Documents
	if err := ValidateNativeRevision(ctx, options.Directory, source.Identity().JournalAuthorityFingerprint(), options.Active, read.Filesystem, entry, read.Budget); err != nil {
		return NativeRecoveryAssessment{}, err
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(options.Directory, physicalwork.MaximumPhysicalPathDepth, read.Budget)
	if err != nil {
		return NativeRecoveryAssessment{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close(), ctx.Err())
		if resultErr != nil {
			result = NativeRecoveryAssessment{}
		}
	}()
	options.Documents.Artifacts = root
	assessment, err := AssessNativeDocuments(journal, ctx, options)
	if err != nil {
		return NativeRecoveryAssessment{}, err
	}
	if err := ValidateNativeRevision(ctx, options.Directory, source.Identity().JournalAuthorityFingerprint(), options.Active, read.Filesystem, entry, read.Budget); err != nil {
		return NativeRecoveryAssessment{}, err
	}
	return NativeRecoveryAssessment{source: source, assessment: assessment, valid: true}, nil
}

func (observed NativeRecoveryAssessment) MatchesSource(source journalinventory.ActiveJournal) bool {
	return observed.valid && observed.source.SameSource(source)
}

func (observed NativeRecoveryAssessment) Facts() AssessmentFacts { return observed.assessment.Facts() }

func (observed NativeRecoveryAssessment) Documents() Documents {
	return observed.assessment.Documents()
}

func (observed NativeRecoveryAssessment) RegistryScope() (ownership.DocumentScope, error) {
	return observed.assessment.RegistryScope()
}

func (observed NativeRecoveryAssessment) CleanBefore() bool {
	return observed.valid && observed.Facts().Classification() == recovery.ClassificationCleanBefore
}

func (observed NativeRecoveryAssessment) CleanAfter() bool {
	return observed.valid && observed.Facts().Classification() == recovery.ClassificationCleanAfter
}

func (observed NativeRecoveryAssessment) RequiresRollback() bool {
	return observed.valid && observed.Facts().Classification() == recovery.ClassificationNeedsRollback
}

func (observed NativeRecoveryAssessment) RequiresFinalization() bool {
	return observed.valid && observed.Facts().Classification() == recovery.ClassificationNeedsFinalize
}

func (observed NativeRecoveryAssessment) HostMatchesExpected() bool {
	return observed.valid && observed.Facts().HostMatchesExpected()
}

// HostReadyForStateCommit requires expected host data and prepared exact metadata.
func (observed NativeRecoveryAssessment) HostReadyForStateCommit() bool {
	return observed.valid && observed.Facts().HostReadyForStateCommit()
}

func (observed NativeRecoveryAssessment) RollbackClaims() ownership.ClaimConvergence {
	return observed.Facts().RollbackClaims()
}

func (observed NativeRecoveryAssessment) FinalizeClaims() ownership.ClaimConvergence {
	return observed.Facts().FinalizeClaims()
}

func (observed NativeRecoveryAssessment) FinalizeBindings() ownership.DocumentBindingConvergence {
	return observed.Facts().FinalizeBindings()
}

func (observed NativeRecoveryAssessment) ObservedPersistence() (durable.Snapshot, ownership.Registry, bool) {
	state, registry := observed.Facts().Persistence()
	return state, registry, observed.valid
}

func (observed NativeRecoveryAssessment) ExpectedClaimRemovals() []ownership.Claim {
	return observed.Facts().ExpectedClaimRemovals()
}

func (observed NativeRecoveryAssessment) MissingDocuments() []pathauthority.Provisional {
	return observed.Facts().MissingDocuments()
}

func (observed NativeRecoveryAssessment) PromoteDocument(ctx context.Context, index int) (journalrecord.Native, bool, error) {
	journal, native := observed.source.Record().Native()
	if !observed.valid || !native {
		return journalrecord.Native{}, false, fmt.Errorf("native promotion requires a source-bound observation")
	}
	return observed.assessment.PromoteDocument(ctx, journal, index)
}
