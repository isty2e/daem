package qualification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func (assessment Assessment) Facts() AssessmentFacts { return assessment.facts }

func (assessment Assessment) Documents() Documents { return assessment.qualified }

func (facts AssessmentFacts) Persistence() (durable.Snapshot, ownership.Registry) {
	return facts.state, facts.registry
}

func (facts AssessmentFacts) Classification() recovery.Classification {
	switch facts.disposition {
	case nativeDocumentsBefore:
		return recovery.ClassificationCleanBefore
	case nativeDocumentsAfter:
		return recovery.ClassificationCleanAfter
	case nativeDocumentsRollback:
		return recovery.ClassificationNeedsRollback
	case nativeDocumentsFinalize:
		return recovery.ClassificationNeedsFinalize
	default:
		return recovery.ClassificationBlocked
	}
}

func (facts AssessmentFacts) DocumentCount() int { return len(facts.stages) }

func (facts AssessmentFacts) DocumentAt(index int) (DocumentAssessment, bool) {
	if index < 0 || index >= len(facts.stages) || index >= facts.documents.Len() {
		return DocumentAssessment{}, false
	}
	return facts.stages[index], true
}

func (document DocumentAssessment) CurrentState() (aggregate.Document, *recovery.PermissionMode, mutationfs.EntryIdentity) {
	mode := document.mode
	if mode != nil {
		copy := *mode
		mode = &copy
	}
	return document.current, mode, document.identity
}

func (document DocumentAssessment) Restoration() journaldocument.Restoration {
	return document.restoration
}

func (document DocumentAssessment) MatchesExpected() bool { return document.expected }

func (facts AssessmentFacts) RollbackClaims() ownership.ClaimConvergence { return facts.rollback }

func (facts AssessmentFacts) FinalizeClaims() ownership.ClaimConvergence { return facts.finalize }

func (facts AssessmentFacts) FinalizeBindings() ownership.DocumentBindingConvergence {
	return facts.bindings
}

func (facts AssessmentFacts) ExpectedClaimRemovals() []ownership.Claim {
	return slices.Clone(facts.expectedRemovals)
}

func (facts AssessmentFacts) MissingDocuments() []pathauthority.Provisional {
	return slices.Clone(facts.missingHistory)
}

func (facts AssessmentFacts) OrdinaryActions() []recovery.Action {
	return facts.ordinary.entries.Actions()
}

func (facts AssessmentFacts) OrdinaryCheckpoints() []recovery.RemovalCleanupObligation {
	return slices.Clone(facts.ordinary.checkpoints)
}

func (facts AssessmentFacts) OrdinaryMatchesExpected(entry recovery.Entry) (bool, error) {
	matched, err := recovery.AssessEntries([]recovery.Entry{entry}, recovery.ClonePathEvidence(facts.ordinary.paths), recovery.CloneBackupEvidence(facts.ordinary.backups))
	return matched.MatchesExpected(), err
}

func (facts AssessmentFacts) HostMatchesExpected() bool {
	if !facts.ordinary.entries.MatchesExpected() {
		return false
	}
	for _, stage := range facts.stages {
		if !stage.expected {
			return false
		}
	}
	return true
}

func (facts AssessmentFacts) HostReadyForStateCommit() bool {
	if !facts.HostMatchesExpected() || !facts.claimsReady {
		return false
	}
	for index := range facts.documents.Len() {
		loaded, present := facts.documents.At(index)
		if !present {
			return false
		}
		for _, intent := range loaded.Group().BindingIntents() {
			if _, ready := intent.KnownAfter(); !ready {
				return false
			}
		}
	}
	_, changed, err := facts.preparation.Apply(facts.registry)
	return err == nil && !changed
}

func (assessment Assessment) PromoteDocument(ctx context.Context, journal journalrecord.Native, index int) (journalrecord.Native, bool, error) {
	if !assessment.facts.state.Equal(journal.Before()) || index < 0 || index >= len(assessment.facts.stages) || !assessment.facts.stages[index].expected {
		return journalrecord.Native{}, false, fmt.Errorf("native promotion requires observed Before state and a published expected document")
	}
	namespace, present := assessment.qualified.NamespaceAt(index)
	if !present {
		return journalrecord.Native{}, false, fmt.Errorf("native recovery document facts are incomplete")
	}
	exact, known := namespace.Observation().Exact()
	if !known {
		return journalrecord.Native{}, false, fmt.Errorf("published native document still has no exact namespace")
	}
	return journal.PromoteDocument(ctx, index, namespace.Addresses(), exact)
}

func PrepareClaims(ctx context.Context, documents journalrecord.LoadedDocuments, transitions []ownershipmutation.ClaimTransition) (ownership.ClaimConvergence, error) {
	prepared, _, _, err := nativeDocumentClaimConvergences(ctx, documents, transitions)
	return prepared, err
}

func (facts AssessmentFacts) SameObservedAuthority(other AssessmentFacts, scope ownership.DocumentScope) bool {
	if facts.disposition != other.disposition || !facts.state.Equal(other.state) || facts.ordinary.observationFingerprint() != other.ordinary.observationFingerprint() || len(facts.stages) != len(other.stages) {
		return false
	}
	if same, err := scope.Matches(facts.registry, other.registry); err != nil || !same {
		return false
	}
	for index, stage := range facts.stages {
		candidate := other.stages[index]
		if !stage.current.Equal(candidate.current) || !sameRecoveryMode(stage.mode, candidate.mode) || (stage.identity == nil) != (candidate.identity == nil) || stage.identity != nil && !stage.identity.Equal(candidate.identity) {
			return false
		}
	}
	return true
}

func sameRecoveryMode(left, right *recovery.PermissionMode) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func (facts AssessmentFacts) ObservationFingerprint() string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00", facts.disposition)
	for _, stage := range facts.stages {
		mode := "absent"
		if stage.mode != nil {
			mode = fmt.Sprintf("%04o", *stage.mode)
		}
		content := sha256.Sum256(stage.current.Content())
		fmt.Fprintf(hash, "%t\x00%s\x00%x\x00", stage.current.Exists(), mode, content)
	}
	fmt.Fprintf(hash, "%s\x00", facts.ordinary.observationFingerprint())
	return hex.EncodeToString(hash.Sum(nil))
}

func (ordinary nativeOrdinaryAssessment) observationFingerprint() string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%t\x00%t\x00%t\x00", ordinary.entries.MatchesBefore(), ordinary.entries.MatchesExpected(), ordinary.entries.Recoverable())
	for _, path := range ordinary.paths {
		mode := "absent"
		if path.PathMode != nil {
			mode = fmt.Sprintf("%04o", *path.PathMode)
		}
		path.PathMode = nil
		fmt.Fprintf(hash, "%#v\x00%s\x00", path, mode)
	}
	for _, backup := range ordinary.backups {
		fmt.Fprintf(hash, "%#v\x00", backup)
	}
	return hex.EncodeToString(hash.Sum(nil))
}
