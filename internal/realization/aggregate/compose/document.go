package compose

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// DocumentCandidate contains one final document and actual participant snapshots.
// It grants no physical write or recovery authority.
type DocumentCandidate struct {
	document aggregate.Document
	expected []aggregate.Snapshot
}

func (candidate DocumentCandidate) Document() aggregate.Document { return candidate.document }
func (candidate DocumentCandidate) Expected() []aggregate.Snapshot {
	return slices.Clone(candidate.expected)
}

// Render projects disjoint semantic candidates onto their shared original input.
// Callers must separately establish that all logical inputs bind one physical file.
func Render(ctx context.Context, document aggregate.Document, limits configdocument.Limits, edits []ProjectionEdit) (DocumentCandidate, error) {
	projections := make([]Participant, len(edits))
	for index, edit := range edits {
		projections[index] = edit.projection
	}
	original, err := prepare(ctx, document, limits, projections)
	if err != nil {
		return DocumentCandidate{}, err
	}
	var changes []configdocument.Change
	privateExpected := make([]aggregate.Snapshot, len(edits))
	for index, edit := range edits {
		if err := ctx.Err(); err != nil {
			return DocumentCandidate{}, err
		}
		before, failure := edit.projection.codec.Read(document, edit.projection.selection)
		if failure != nil {
			return DocumentCandidate{}, failure
		}
		if !before.Equal(edit.plan.Before()) {
			return DocumentCandidate{}, fmt.Errorf("projection before state changed")
		}
		rendered, failure := edit.projection.codec.Render(document, edit.plan)
		if failure != nil {
			return DocumentCandidate{}, failure
		}
		selected, err := projectedChanges(ctx, rendered.Document(), limits, edit.projection)
		if err != nil {
			return DocumentCandidate{}, err
		}
		changes = append(changes, selected...)
		privateExpected[index] = rendered.Expected()
	}
	updated, err := configdocument.Edit(ctx, original, changes)
	if err != nil {
		return DocumentCandidate{}, err
	}
	final := nativeDocument(updated)
	expected := make([]aggregate.Snapshot, 0, len(edits))
	for index, edit := range edits {
		actual, failure := edit.projection.codec.Read(final, edit.projection.selection)
		if failure != nil {
			return DocumentCandidate{}, failure
		}
		if !actual.Equal(privateExpected[index]) {
			return DocumentCandidate{}, fmt.Errorf("composed document differs from a semantic projection candidate")
		}
		if _, err := aggregate.NewRenderedDocument(final, edit.plan, actual); err != nil {
			return DocumentCandidate{}, err
		}
		expected = append(expected, actual)
	}
	if err := ctx.Err(); err != nil {
		return DocumentCandidate{}, err
	}
	return DocumentCandidate{final, expected}, nil
}

// Restore assembles baseline candidates; callers still guard current host state
// and mutation authority before publishing the result.
func Restore(ctx context.Context, document aggregate.Document, limits configdocument.Limits, baselines []ProjectionBaseline) (DocumentCandidate, error) {
	projections := make([]Participant, len(baselines))
	for index, baseline := range baselines {
		projections[index] = baseline.projection
	}
	original, err := prepare(ctx, document, limits, projections)
	if err != nil {
		return DocumentCandidate{}, err
	}
	beforeExisted := baselines[0].baseline.DocumentExisted()
	var changes []configdocument.Change
	privateExpected := make([]aggregate.Snapshot, len(baselines))
	for index, baseline := range baselines {
		if err := ctx.Err(); err != nil {
			return DocumentCandidate{}, err
		}
		if baseline.baseline.DocumentExisted() != beforeExisted {
			return DocumentCandidate{}, fmt.Errorf("projection recovery baselines disagree on document presence")
		}
		restored, failure := baseline.projection.codec.Restore(document, baseline.baseline)
		if failure != nil {
			return DocumentCandidate{}, failure
		}
		selected, err := projectedChanges(ctx, restored.Document(), limits, baseline.projection)
		if err != nil {
			return DocumentCandidate{}, err
		}
		changes = append(changes, selected...)
		privateExpected[index] = restored.Expected()
	}
	updated, err := configdocument.Edit(ctx, original, changes)
	if err != nil {
		return DocumentCandidate{}, err
	}
	if !beforeExisted {
		root, present, err := updated.Lookup(output.KeyPath{})
		if err != nil {
			return DocumentCandidate{}, err
		}
		if present && root.IsObject() && root.IsAtomic() && len(updated.Comments()) == 0 {
			updated, err = configdocument.Absent(projections[0].syntax, limits)
			if err != nil {
				return DocumentCandidate{}, err
			}
		}
	}
	final := nativeDocument(updated)
	expected := make([]aggregate.Snapshot, 0, len(baselines))
	for index, baseline := range baselines {
		actual, failure := baseline.projection.codec.Read(final, baseline.projection.selection)
		if failure != nil {
			return DocumentCandidate{}, failure
		}
		if !sameRestoredProjection(privateExpected[index], actual) {
			return DocumentCandidate{}, fmt.Errorf("composed document differs from a semantic recovery candidate")
		}
		if _, err := aggregate.NewRestoredDocumentWithExpected(final, baseline.baseline, actual); err != nil {
			return DocumentCandidate{}, err
		}
		expected = append(expected, actual)
	}
	if err := ctx.Err(); err != nil {
		return DocumentCandidate{}, err
	}
	return DocumentCandidate{final, expected}, nil
}

func prepare(ctx context.Context, document aggregate.Document, limits configdocument.Limits, projections []Participant) (configdocument.Document, error) {
	if ctx == nil {
		return configdocument.Document{}, fmt.Errorf("native document composition context is required")
	}
	if err := ctx.Err(); err != nil {
		return configdocument.Document{}, err
	}
	if err := limits.Validate(); err != nil {
		return configdocument.Document{}, err
	}
	if len(projections) == 0 {
		return configdocument.Document{}, fmt.Errorf("native document composition requires projections")
	}
	var areas []output.KeyPath
	for _, projection := range projections {
		if projection.codec == nil || len(projection.areas) == 0 || projection.syntax != projections[0].syntax {
			return configdocument.Document{}, fmt.Errorf("native document projection is invalid or uses a different grammar")
		}
		areas = append(areas, projection.areas...)
	}
	if len(output.TopmostKeyPaths(areas)) != len(areas) {
		return configdocument.Document{}, fmt.Errorf("native document projection rewrite areas overlap")
	}
	if err := document.Validate(); err != nil {
		return configdocument.Document{}, err
	}
	return parse(ctx, document, projections[0].syntax, limits)
}

func projectedChanges(ctx context.Context, document aggregate.Document, limits configdocument.Limits, projection Participant) ([]configdocument.Change, error) {
	parsed, err := parse(ctx, document, projection.syntax, limits)
	if err != nil {
		return nil, err
	}
	changes := make([]configdocument.Change, 0, len(projection.areas))
	for _, area := range projection.areas {
		value, present, err := parsed.Lookup(area)
		if err != nil {
			return nil, err
		}
		var change configdocument.Change
		if present {
			change, err = configdocument.Set(area, value)
		} else {
			change, err = configdocument.Remove(area)
		}
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func sameRestoredProjection(expected, actual aggregate.Snapshot) bool {
	left, right := expected.States(), actual.States()
	if len(left) != len(right) {
		return false
	}
	for index, state := range left {
		if _, native := aggregate.HostConfigSyntaxForCodec(state.Contract().CodecContractID()); native {
			if !state.SelectedValueEqual(right[index]) {
				return false
			}
		} else if !state.Equal(right[index]) {
			return false
		}
	}
	return true
}

func parse(ctx context.Context, document aggregate.Document, syntax configdocument.Syntax, limits configdocument.Limits) (configdocument.Document, error) {
	if !document.Exists() {
		return configdocument.Absent(syntax, limits)
	}
	return configdocument.Parse(ctx, document.Content(), syntax, limits)
}

func nativeDocument(document configdocument.Document) aggregate.Document {
	if !document.Exists() {
		return aggregate.AbsentDocument()
	}
	return aggregate.ExistingDocument(document.Bytes())
}
