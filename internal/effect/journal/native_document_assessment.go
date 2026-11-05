package journal

import (
	"context"
	"errors"
	"fmt"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type nativeDocumentRecoveryReadOptions struct {
	documents journalqualification.ReadOptions
	roots     journalnamespace.Roots
	state     durable.SnapshotReader
	registry  ownershipmutation.RegistryReader
	directory string
	active    ActiveJournalAuthority
	resolver  func(output.Destination) (string, error)
}

type nativeDocumentDisposition string

const (
	nativeDocumentsBefore   nativeDocumentDisposition = "documents_before"
	nativeDocumentsRollback nativeDocumentDisposition = "documents_rollback"
	nativeDocumentsAfter    nativeDocumentDisposition = "documents_after"
	nativeDocumentsFinalize nativeDocumentDisposition = "documents_finalize"
)

type nativeDocumentRecoveryStage struct {
	current     aggregate.Document
	mode        *recovery.PermissionMode
	identity    mutationfs.EntryIdentity
	restoration journaldocument.Restoration
	expected    bool
}

// This assessment joins ordinary entries and all document groups with one state
// and ownership phase. It grants no execution or retirement authority.
type nativeDocumentAssessmentFacts struct {
	documents        journalrecord.LoadedDocuments
	state            durable.Snapshot
	registry         ownership.Registry
	stages           []nativeDocumentRecoveryStage
	disposition      nativeDocumentDisposition
	rollback         ownership.ClaimConvergence
	finalize         ownership.ClaimConvergence
	bindings         ownership.DocumentBindingConvergence
	ordinary         nativeOrdinaryAssessment
	missingHistory   []pathauthority.Provisional
	expectedRemovals []ownership.Claim
	preparation      ownership.ClaimConvergence
	claimsReady      bool
}

type nativeDocumentAssessment struct {
	nativeDocumentAssessmentFacts
	qualified journalqualification.Documents
}

func assessDocumentRecovery(journal journalrecord.Native, ctx context.Context, options nativeDocumentRecoveryReadOptions) (nativeDocumentAssessment, error) {
	if ctx == nil || options.state == nil || options.registry == nil {
		return nativeDocumentAssessment{}, fmt.Errorf("native recovery context, selected state reader and registry reader are required")
	}
	qualified, err := journalqualification.QualifyDocuments(journal, ctx, options.documents, options.roots)
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	state, err := options.state(ctx)
	if err != nil {
		return nativeDocumentAssessment{}, fmt.Errorf("observe native recovery state: %w", err)
	}
	before, after := state.Equal(journal.Before()), state.Equal(journal.After())
	if !before && !after {
		return nativeDocumentAssessment{}, fmt.Errorf("native recovery state differs from both recorded Snapshots")
	}

	independent, err := journal.ClaimTransitions()
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	for _, transition := range independent {
		if !transition.Owner().Equal(journal.Owner()) {
			return nativeDocumentAssessment{}, fmt.Errorf("ordinary claim transition has another journal owner")
		}
	}
	preparation, rollback, finalize, err := nativeDocumentClaimConvergences(ctx, qualified.Records(), independent)
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	removals := append(rollback.ExpectedRemovals(), finalize.ExpectedRemovals()...)
	var missingHistory []pathauthority.Provisional
	for index := range qualified.Len() {
		namespace, present := qualified.NamespaceAt(index)
		if !present {
			return nativeDocumentAssessment{}, fmt.Errorf("native recovery document facts are incomplete")
		}
		loaded, present := qualified.Records().At(index)
		if !present {
			return nativeDocumentAssessment{}, fmt.Errorf("native recovery document facts are incomplete")
		}
		claims := loaded.Claims()
		if missing, provisional := namespace.Observation().Provisional(); provisional && len(claims.Before())+len(claims.After()) != 0 {
			missingHistory = append(missingHistory, missing)
		}
	}
	var registry ownership.Registry
	if len(missingHistory) == 0 {
		registry, err = options.registry.LoadForClaimRemovals(ctx, removals, physicalwork.MaximumPhysicalPathDepth, options.documents.Budget)
	} else {
		reader, supported := options.registry.(ownershipmutation.DocumentRegistryReader)
		if !supported {
			return nativeDocumentAssessment{}, fmt.Errorf("missing native history requires a document-qualified registry reader")
		}
		registry, err = reader.LoadForDocuments(ctx, journal.Owner(), missingHistory, removals, physicalwork.MaximumPhysicalPathDepth, options.documents.Budget)
	}
	if err != nil {
		return nativeDocumentAssessment{}, fmt.Errorf("observe native recovery registry: %w", err)
	}
	if err := validateNativeProvisionalRegistry(ctx, qualified, registry); err != nil {
		return nativeDocumentAssessment{}, err
	}
	ordinaryIntents, err := journal.ProvisionalAcquires()
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	claimsReady := len(ordinaryIntents) == 0
	for index := range qualified.Records().Len() {
		if err := ctx.Err(); err != nil {
			return nativeDocumentAssessment{}, err
		}
		loaded, present := qualified.Records().At(index)
		if !present {
			return nativeDocumentAssessment{}, fmt.Errorf("native recovery document facts are incomplete")
		}
		claimsReady = claimsReady && len(loaded.Provisional()) == 0
	}
	_, rollbackChanged, rollbackErr := rollback.Apply(registry)
	_, finalizeChanged, finalizeErr := finalize.Apply(registry)
	metadata, err := nativeDocumentBindingPhases(ctx, qualified.Records(), registry)
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	ordinary, err := assessOrdinaryRecovery(journal, ctx, options, registry)
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	if before && after && (!ordinaryClaimsUnchanged(independent) || !metadata.unchanged || !nativeDocumentClaimsUnchanged(qualified.Records()) || !metadata.ready || !claimsReady) {
		return nativeDocumentAssessment{}, fmt.Errorf("equal native Snapshots retain nontrivial document metadata work")
	}

	narrowed, err := narrowedDocumentGroups(journal, ctx, qualified.Records())
	if err != nil {
		return nativeDocumentAssessment{}, err
	}
	stages := make([]nativeDocumentRecoveryStage, 0, qualified.Records().Len())
	allBefore, allExpected := ordinary.entries.MatchesBefore(), ordinary.entries.MatchesExpected()
	for index := range qualified.Records().Len() {
		if err := ctx.Err(); err != nil {
			return nativeDocumentAssessment{}, err
		}
		loaded, present := qualified.Records().At(index)
		if !present {
			return nativeDocumentAssessment{}, fmt.Errorf("native recovery document facts are incomplete")
		}
		group := loaded.Group()
		namespace, present := qualified.NamespaceAt(index)
		if !present {
			return nativeDocumentAssessment{}, fmt.Errorf("native recovery document facts are incomplete")
		}
		current, mode, identity, err := namespace.ReadReferent(ctx, group.Comparison().MaximumDocumentBytes(), options.documents.Budget)
		if err != nil {
			return nativeDocumentAssessment{}, fmt.Errorf("observe native document[%d]: %w", index, err)
		}
		match, err := group.Comparison().Match(ctx, current, mode)
		if err != nil {
			return nativeDocumentAssessment{}, err
		}
		var restoration journaldocument.Restoration
		if _, narrowing := narrowed[index]; before && narrowing && current.Equal(group.Comparison().ExpectedDocument()) {
			root, err := recovery.NewPartialDocumentState(current.Exists(), mode, nil)
			if err != nil {
				return nativeDocumentAssessment{}, err
			}
			if !root.SameRootMetadata(group.Comparison().BeforeState()) && !root.SameRootMetadata(group.Comparison().ExpectedState()) {
				return nativeDocumentAssessment{}, fmt.Errorf("narrowed document root is outside the recorded metadata envelope")
			}
			restoration, err = journaldocument.NewRestoration(current, mode, group.Comparison().BeforeDocument(), group.Comparison().BeforeMode())
		} else {
			restoration, err = group.Comparison().RestoreBefore(ctx, current, mode)
		}
		if err != nil {
			if !match.Recoverable() && ctx.Err() == nil && !errors.Is(err, configdocument.ErrLimitExceeded) {
				return nativeDocumentAssessment{}, &nativeSelectedPhaseError{cause: err}
			}
			return nativeDocumentAssessment{}, fmt.Errorf("stage native document[%d]: %w", index, err)
		}
		allBefore = allBefore && !restoration.Changed()
		allExpected = allExpected && match.MatchesExpected()
		stages = append(stages, nativeDocumentRecoveryStage{current: current, mode: mode, identity: identity, restoration: restoration, expected: match.MatchesExpected()})
	}

	result := nativeDocumentAssessment{
		nativeDocumentAssessmentFacts: nativeDocumentAssessmentFacts{documents: qualified.Records(), state: state, registry: registry, stages: stages, rollback: rollback, finalize: finalize, bindings: metadata.convergence, preparation: preparation, claimsReady: claimsReady, ordinary: ordinary, missingHistory: missingHistory, expectedRemovals: removals},
		qualified:                     qualified,
	}
	switch {
	case after && allExpected && claimsReady && metadata.ready && finalizeErr == nil:
		result.disposition = nativeDocumentsAfter
		if finalizeChanged || !metadata.after {
			result.disposition = nativeDocumentsFinalize
		}
	case before && metadata.before && rollbackErr == nil:
		result.disposition = nativeDocumentsRollback
		if !after && allBefore && !rollbackChanged {
			result.disposition = nativeDocumentsBefore
		}
	default:
		return nativeDocumentAssessment{}, errors.Join(fmt.Errorf("native document state, host and registry phases do not admit recovery"), rollbackErr, finalizeErr)
	}
	if err := ctx.Err(); err != nil {
		return nativeDocumentAssessment{}, err
	}
	return result, nil
}

func nativeDocumentClaimConvergences(ctx context.Context, transitions journalrecord.LoadedDocuments, independent []ownershipmutation.ClaimTransition) (ownership.ClaimConvergence, ownership.ClaimConvergence, ownership.ClaimConvergence, error) {
	set, err := ownershipmutation.NewClaimTransitionSet(independent)
	if err != nil {
		return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
	}
	prepared, err := set.Preparation()
	if err != nil {
		return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
	}
	before, err := set.Rollback()
	if err != nil {
		return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
	}
	after, err := set.Finalization()
	if err != nil {
		return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
	}
	var prepareChanges, rollbackChanges, finalizeChanges []ownership.DocumentClaimChange
	for index := range transitions.Len() {
		if err := ctx.Err(); err != nil {
			return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
		}
		loaded, present := transitions.At(index)
		if !present {
			return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, fmt.Errorf("native recovery document facts are incomplete")
		}
		transition := loaded.Claims()
		if len(transition.Before()) == 0 && len(transition.After()) == 0 {
			continue
		}
		prepare, err := transition.Preparation()
		if err != nil {
			return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
		}
		rollback, err := transition.Rollback()
		if err != nil {
			return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
		}
		finalize, err := transition.Finalization()
		if err != nil {
			return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
		}
		prepareChanges, rollbackChanges, finalizeChanges = append(prepareChanges, prepare), append(rollbackChanges, rollback), append(finalizeChanges, finalize)
	}
	prepared, err = prepared.WithDocumentChanges(prepareChanges)
	if err != nil {
		return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
	}
	before, err = before.WithDocumentChanges(rollbackChanges)
	if err != nil {
		return ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, ownership.ClaimConvergence{}, err
	}
	after, err = after.WithDocumentChanges(finalizeChanges)
	return prepared, before, after, err
}

type nativeDocumentBindingPhase struct {
	before, after, ready, unchanged bool
	convergence                     ownership.DocumentBindingConvergence
}

func nativeDocumentBindingPhases(ctx context.Context, groups journalrecord.LoadedDocuments, registry ownership.Registry) (nativeDocumentBindingPhase, error) {
	phase := nativeDocumentBindingPhase{before: true, after: true, ready: true, unchanged: true}
	var exact []ownership.DocumentBindingIntent
	for index := range groups.Len() {
		loaded, present := groups.At(index)
		if !present {
			return nativeDocumentBindingPhase{}, fmt.Errorf("native recovery document facts are incomplete")
		}
		group := loaded.Group()
		for _, intent := range group.BindingIntents() {
			if err := ctx.Err(); err != nil {
				return nativeDocumentBindingPhase{}, err
			}
			current := ownership.NoDocumentBinding()
			if binding, found := registry.DocumentBinding(intent.Owner().Key(), intent.Destination()); found {
				var err error
				current, err = ownership.PresentDocumentBinding(binding)
				if err != nil {
					return nativeDocumentBindingPhase{}, err
				}
			}
			phase.before = phase.before && current.Equal(intent.Before())
			after, ready := intent.KnownAfter()
			phase.ready = phase.ready && ready
			phase.after = phase.after && ready && current.Equal(after)
			changed, err := intent.ChangesBinding()
			if err != nil {
				return nativeDocumentBindingPhase{}, err
			}
			phase.unchanged = phase.unchanged && !changed
			if ready {
				exact = append(exact, intent)
			}
		}
	}
	convergence, err := ownership.NewDocumentBindingConvergence(exact)
	if err != nil {
		return nativeDocumentBindingPhase{}, err
	}
	_, _, err = convergence.Apply(registry)
	if err != nil {
		return nativeDocumentBindingPhase{}, err
	}
	phase.convergence = convergence
	return phase, nil
}

func nativeDocumentClaimsUnchanged(transitions journalrecord.LoadedDocuments) bool {
	var before, after []ownership.Claim
	for index := range transitions.Len() {
		loaded, present := transitions.At(index)
		if !present {
			return false
		}
		transition := loaded.Claims()
		before, after = append(before, transition.Before()...), append(after, transition.After()...)
	}
	left, leftErr := ownership.NewRegistry(before)
	right, rightErr := ownership.NewRegistry(after)
	return leftErr == nil && rightErr == nil && left.Equal(right)
}

func ordinaryClaimsUnchanged(transitions []ownershipmutation.ClaimTransition) bool {
	for _, transition := range transitions {
		if !transition.Before().Equal(transition.Prepared()) || !transition.Before().Equal(transition.After()) {
			return false
		}
	}
	return true
}

func validateNativeProvisionalRegistry(ctx context.Context, qualified journalqualification.Documents, registry ownership.Registry) error {
	for index := range qualified.Records().Len() {
		loaded, present := qualified.Records().At(index)
		if !present {
			return fmt.Errorf("native recovery document facts are incomplete")
		}
		namespace, present := qualified.NamespaceAt(index)
		if !present {
			return fmt.Errorf("native recovery document facts are incomplete")
		}
		acquires := loaded.Provisional()
		for _, acquire := range acquires {
			if err := ctx.Err(); err != nil {
				return err
			}
			observation := namespace.Observation()
			var conflict ownership.Claim
			var found bool
			if exact, known := observation.Exact(); known {
				address, err := ownership.NewManagedAddress(exact, acquire.Intent().Footprint())
				if err != nil {
					return err
				}
				if err := acquire.Intent().AdmitAddress(address); err != nil {
					return err
				}
				conflict, found = registry.Conflict(address)
			} else {
				candidate, known := observation.Provisional()
				if !known || !candidate.Equal(acquire.Intent().Path()) {
					return fmt.Errorf("native provisional acquisition namespace changed")
				}
				conflict, found = registry.ProvisionalAncestorConflict(candidate)
			}
			if found {
				return fmt.Errorf("native provisional acquisition conflicts with a durable claim owned by %q", conflict.Owner().ManifestPath())
			}
		}
	}
	return ctx.Err()
}

func narrowedDocumentGroups(journal journalrecord.Native, ctx context.Context, groups journalrecord.LoadedDocuments) (map[int]struct{}, error) {
	before, after := make(map[aggregate.DocumentAddress]struct{}), make(map[aggregate.DocumentAddress]struct{})
	keyedAfter := make(map[aggregate.DocumentAddress]struct{})
	for _, row := range journal.After().ManagedAggregates() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, native := aggregate.HostConfigSyntaxForCodec(row.Contribution().CodecContractID()); native {
			keyedAfter[row.Contribution().Contract().Address().Document()] = struct{}{}
		}
	}
	for side, snapshot := range []durable.Snapshot{journal.Before(), journal.After()} {
		for _, row := range snapshot.ManagedPaths() {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id, found := topologyprojection.EntityID(row.Subject())
			if !found || id.Kind() != entity.KindHostConfig {
				continue
			}
			for _, target := range row.ConsumerTargets() {
				address, err := aggregate.NewDocumentAddress(target, row.Scope(), row.Destination())
				if err != nil {
					return nil, err
				}
				if side == 0 {
					before[address] = struct{}{}
				} else {
					after[address] = struct{}{}
				}
			}
		}
	}
	result := make(map[int]struct{})
	for index := range groups.Len() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		loaded, present := groups.At(index)
		if !present {
			return nil, fmt.Errorf("native recovery document facts are incomplete")
		}
		group := loaded.Group()
		if group.Comparison().WholeDocument() || !group.Comparison().BeforeDocument().Exists() || !group.Comparison().ExpectedDocument().Exists() {
			continue
		}
		for _, alias := range group.Aliases() {
			_, wasWhole := before[alias]
			_, staysWhole := after[alias]
			_, becomesKeyed := keyedAfter[alias]
			if wasWhole && !staysWhole && becomesKeyed {
				result[index] = struct{}{}
			}
		}
	}
	return result, nil
}
