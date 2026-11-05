package execute

import (
	"context"
	"fmt"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func applyForwardRemovalCertificates(
	managed []effectpreparation.ManagedPathEffect,
	aggregates []effectpreparation.AggregateEffect,
	payloads payload.PayloadSet,
	demands recovery.RemovalDemandSet,
) ([]removaloperation.ForwardCandidate, error) {
	certificates := make([]removaloperation.ForwardCandidate, 0, len(managed)+len(aggregates))
	for index, effect := range managed {
		if !effect.RequiresPayload() {
			continue
		}
		state, err := expectedManagedPathRemovalState(effect)
		if err != nil {
			return nil, fmt.Errorf("managed effect[%d] expected removal state: %w", index, err)
		}
		relation := removaloperation.Relation{Scope: effect.Scope(), Destination: effect.Destination()}
		if !removalStateIsDemanded(demands, relation, state) {
			continue
		}
		stateKind, _, err := state.Kind()
		if err != nil {
			return nil, fmt.Errorf("managed effect[%d] forward removal state: %w", index, err)
		}
		prepared, present := payloads.LookupSubject(effect.Subject())
		if !present {
			return nil, fmt.Errorf(
				"managed effect[%d] forward removal payload is missing for subject %s/%s %q",
				index,
				effect.Subject().Kind(),
				effect.Subject().Namespace(),
				effect.Subject().Key(),
			)
		}
		if file, isFile := prepared.File(); isFile && stateKind == recovery.PathKindFile {
			contentBytes := int64(len(file.Bytes()))
			certificates = append(certificates, constantForwardRemovalCertificate(
				relation,
				state,
				0,
				contentBytes,
			))
			continue
		}
		verified, err := effectpreparation.ManagedPathPayload(effect, payloads)
		if err != nil {
			return nil, fmt.Errorf("managed effect[%d] forward removal directory payload: %w", index, err)
		}
		directory, isDirectory := verified.Directory()
		if !isDirectory {
			return nil, fmt.Errorf("managed effect[%d] forward removal payload has no content variant", index)
		}
		certificates = append(certificates, removaloperation.ForwardCandidate{
			Relation: relation,
			State:    state,
			Measure: func(ctx context.Context, maximum workmeasure.ArtifactWork) (workmeasure.ArtifactWork, error) {
				return measureDirectoryArtifactRemovalWork(
					ctx,
					directory.View(),
					directory.Identity(),
					maximum,
				)
			},
		})
	}
	for index, effect := range aggregates {
		if effect.Kind() != effectpreparation.AggregateEffectCreate {
			continue
		}
		state, err := aggregateDocumentExpectedRemovalState(effect)
		if err != nil {
			return nil, fmt.Errorf("aggregate effect[%d] expected removal state: %w", index, err)
		}
		relation := removaloperation.Relation{Scope: effect.Scope(), Destination: effect.Destination()}
		if !removalStateIsDemanded(demands, relation, state) {
			continue
		}
		document := effect.Rendered().Document()
		if !document.Exists() {
			continue
		}
		certificates = append(certificates, constantForwardRemovalCertificate(
			relation,
			state,
			0,
			int64(len(document.Content())),
		))
	}
	return certificates, nil
}

func constantForwardRemovalCertificate(
	relation removaloperation.Relation,
	state recovery.RemovalState,
	entries int,
	bytes int64,
) removaloperation.ForwardCandidate {
	return removaloperation.ForwardCandidate{
		Relation: relation,
		State:    state,
		Measure: func(context.Context, workmeasure.ArtifactWork) (workmeasure.ArtifactWork, error) {
			return workmeasure.NewArtifactWork(entries, bytes)
		},
	}
}

func removalStateIsDemanded(
	demands recovery.RemovalDemandSet,
	relation removaloperation.Relation,
	state recovery.RemovalState,
) bool {
	for _, demand := range demands.Demands() {
		if demand.Scope() != relation.Scope || demand.Destination() != relation.Destination {
			continue
		}
		for _, admitted := range demand.States() {
			if admitted.Equal(state) {
				return true
			}
		}
	}
	return false
}

func measureDirectoryArtifactRemovalWork(
	ctx context.Context,
	view access.View,
	identity artifact.ExactIdentity,
	maximum workmeasure.ArtifactWork,
) (workmeasure.ArtifactWork, error) {
	maximumEntries := min(maximum.Entries(), recovery.MaximumArtifactTreeEntries)
	maximumBytes := min(maximum.Bytes(), recovery.MaximumArtifactTreeBytes)
	traversal, err := access.NewTraversalLimit(
		uint64(maximumEntries+1),
		max(int64(1), maximumBytes),
	)
	if err != nil {
		return workmeasure.ArtifactWork{}, err
	}
	structure, err := access.NewTreeStructureLimit(
		max(1, maximumEntries),
		recovery.MaximumArtifactTreeDepth,
	)
	if err != nil {
		return workmeasure.ArtifactWork{}, err
	}
	measurement, err := view.MeasureVerifiedDirectory(
		ctx,
		identity,
		traversal,
		structure,
	)
	if err != nil {
		return workmeasure.ArtifactWork{}, err
	}
	return workmeasure.NewArtifactWork(
		measurement.DescendantEntries(),
		measurement.RegularFileBytes(),
	)
}
