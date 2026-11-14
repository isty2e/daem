package readiness

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/isty2e/daem/internal/assurance/observe"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// Native whole-file aliases use their containing observation, never a second
// expansion of the legacy HOME spelling into a different physical document.
func observeManagedPaths(ctx context.Context, resolver liveobserve.DestinationResolver, requests []liveobserve.ManagedPathRequest, native ...nativeAssessmentSources) ([]observe.ManagedPathEvidence, error) {
	var ordinary []liveobserve.ManagedPathRequest
	var projected []observe.ManagedPathEvidence
	type requestKey struct {
		subject     topology.SubjectID
		destination output.Destination
	}
	seen := make(map[requestKey]struct{})
	for _, request := range requests {
		if !aggregate.IsHostConfigDocumentSubject(request.Subject) {
			ordinary = append(ordinary, request)
			continue
		}
		if request.ContentKind != realization.PathProjectionFile || len(native) == 0 || native[0].input == nil || native[0].input.Documents == nil {
			return nil, fmt.Errorf("native whole-file observation requires its selected document batch")
		}
		selected, err := native[0].input.wholeFileAlias(request.Subject, request.Destination)
		if err != nil {
			return nil, err
		}
		key := requestKey{subject: request.Subject, destination: request.Destination}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		evidence, err := nativeWholePathEvidence(ctx, request.Subject, selected.Address(), native[0].input.Documents.Evidence())
		if err != nil {
			return nil, err
		}
		projected = append(projected, evidence)
	}
	result, err := liveobserve.ManagedPathEvidence(ctx, resolver, ordinary)
	if err != nil {
		return nil, err
	}
	result = append(result, projected...)
	slices.SortFunc(result, func(left, right observe.ManagedPathEvidence) int {
		if order := topology.CompareSubjectID(left.Subject(), right.Subject()); order != 0 {
			return order
		}
		return cmp.Compare(left.Destination().String(), right.Destination().String())
	})
	return result, nil
}

// wholeFileAlias returns the selected lookup route for one native whole-file
// subject. Its root lifetime remains borrowed from the containing observation.
func (input *NativeAssessmentInput) wholeFileAlias(subject topology.SubjectID, destination output.Destination) (journalnamespace.Alias, error) {
	identity, native := topologyprojection.EntityID(subject)
	if !native || !aggregate.IsHostConfigDocumentSubject(subject) || input == nil || input.Documents == nil {
		return journalnamespace.Alias{}, fmt.Errorf("native whole-file request requires its selected document batch")
	}
	for _, namespace := range input.Documents.Evidence().Namespaces() {
		for _, alias := range namespace.Aliases() {
			address := alias.Address()
			selected, err := aggregate.HostConfigDocumentSubject(identity, address.Target(), address.Scope())
			if err == nil && selected == subject && address.AggregateRoot() == destination {
				return alias, nil
			}
		}
	}
	return journalnamespace.Alias{}, fmt.Errorf("native whole-file request has no selected namespace")
}

func nativeWholePathEvidence(ctx context.Context, subject topology.SubjectID, alias aggregate.DocumentAddress, batch journalobservation.Batch) (observe.ManagedPathEvidence, error) {
	if err := ctx.Err(); err != nil {
		return observe.ManagedPathEvidence{}, err
	}
	document, mode, observed := batch.Document(alias)
	if !observed || document.Exists() && mode == nil {
		return observe.ManagedPathEvidence{}, fmt.Errorf("native whole-file observation requires its retained original document and mode")
	}
	var fileMode os.FileMode
	var hash artifact.ContentHash
	if document.Exists() {
		fileMode = mode.FileMode()
		hash = artifact.HashFileContentWithExecutable(document.Content(), fileMode&0o111 != 0)
	}
	return observe.NewManagedPathEvidence(subject, alias.AggregateRoot(), document.Exists(), hash, fileMode)
}
