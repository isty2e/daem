package document

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
)

type ComparisonInput struct {
	Before       aggregate.Document
	Expected     aggregate.Document
	BeforeMode   *recovery.PermissionMode
	ExpectedMode *recovery.PermissionMode
	Limits       configdocument.Limits
}

// Comparison binds actual loaded witnesses to their comparison extent.
// It supplies no namespace, managed-membership or publication authority.
type Comparison struct {
	before        aggregate.Document
	expected      aggregate.Document
	beforeState   recovery.PartialDocumentState
	expectedState recovery.PartialDocumentState
	limits        configdocument.Limits
	participants  []compose.Participant
	order         *piconfig.PackagePermutation
	whole         bool
	initialized   bool
}

type ComparisonMatch struct {
	before      bool
	expected    bool
	recoverable bool
}

func (match ComparisonMatch) MatchesBefore() bool   { return match.before }
func (match ComparisonMatch) MatchesExpected() bool { return match.expected }
func (match ComparisonMatch) Recoverable() bool     { return match.recoverable }

func (comparison Comparison) Validate() error {
	if !comparison.initialized {
		return fmt.Errorf("document comparison is required")
	}
	return nil
}

func (comparison Comparison) MaximumDocumentBytes() int64 {
	return comparison.limits.MaximumBytes
}

func (comparison Comparison) BeforeDocument() aggregate.Document   { return comparison.before }
func (comparison Comparison) ExpectedDocument() aggregate.Document { return comparison.expected }
func (comparison Comparison) BeforeState() recovery.PartialDocumentState {
	return comparison.beforeState
}

func (comparison Comparison) ExpectedState() recovery.PartialDocumentState {
	return comparison.expectedState
}
func (comparison Comparison) Limits() configdocument.Limits { return comparison.limits }
func (comparison Comparison) WholeDocument() bool           { return comparison.whole }

func (comparison Comparison) Changed() bool {
	return !comparison.before.Equal(comparison.expected) || !comparison.beforeState.SameRootMetadata(comparison.expectedState)
}

func NewWholeComparison(ctx context.Context, input ComparisonInput) (Comparison, error) {
	comparison, err := newDocumentComparison(ctx, input, nil)
	if err != nil {
		return Comparison{}, err
	}
	comparison.whole = true
	return comparison, nil
}

// NewSelectedComparison keeps dedicated package order separate from
// aggregate selections. An empty selection compares only regular-root metadata.
func NewSelectedComparison(ctx context.Context, input ComparisonInput, participants []compose.Participant, order *piconfig.PackagePermutation) (Comparison, error) {
	comparison, err := newDocumentComparison(ctx, input, participants)
	if err != nil {
		return Comparison{}, err
	}
	if _, err := recovery.MatchPartialDocumentStates(comparison.beforeState, comparison.expectedState, comparison.beforeState); err != nil {
		return Comparison{}, err
	}
	if order == nil {
		return comparison, nil
	}
	packages, err := output.NewKeyPath("packages")
	if err != nil {
		return Comparison{}, err
	}
	for _, participant := range comparison.participants {
		for _, area := range participant.RewriteAreas() {
			if area.Overlaps(packages) {
				return Comparison{}, fmt.Errorf("selected aggregate area overlaps dedicated Pi package order")
			}
		}
	}
	before, err := readPiComparisonDocument(ctx, input.Before, input.Limits)
	if err != nil {
		return Comparison{}, err
	}
	expected, err := readPiComparisonDocument(ctx, input.Expected, input.Limits)
	if err != nil {
		return Comparison{}, err
	}
	if !order.MatchesBefore(before) || !order.MatchesExpected(expected) {
		return Comparison{}, fmt.Errorf("Pi package inverse does not match the loaded document witnesses")
	}
	if err := ctx.Err(); err != nil {
		return Comparison{}, err
	}
	retained := *order
	comparison.order = &retained
	return comparison, nil
}

func newDocumentComparison(ctx context.Context, input ComparisonInput, participants []compose.Participant) (Comparison, error) {
	for _, document := range []aggregate.Document{input.Before, input.Expected} {
		if err := ValidateImage(ctx, document, input.Limits); err != nil {
			return Comparison{}, err
		}
	}
	before, err := readComparisonState(ctx, input.Before, input.BeforeMode, input.Limits, participants)
	if err != nil {
		return Comparison{}, err
	}
	expected, err := readComparisonState(ctx, input.Expected, input.ExpectedMode, input.Limits, participants)
	if err != nil {
		return Comparison{}, err
	}
	if err := ctx.Err(); err != nil {
		return Comparison{}, err
	}
	return Comparison{
		before: input.Before, expected: input.Expected,
		beforeState: before, expectedState: expected, limits: input.Limits,
		participants: append([]compose.Participant(nil), participants...), initialized: true,
	}, nil
}

func (comparison Comparison) Match(ctx context.Context, current aggregate.Document, mode *recovery.PermissionMode) (ComparisonMatch, error) {
	if err := comparison.Validate(); err != nil {
		return ComparisonMatch{}, err
	}
	if err := ValidateImage(ctx, current, comparison.limits); err != nil {
		return ComparisonMatch{}, err
	}
	observed, err := readComparisonState(ctx, current, mode, comparison.limits, comparison.participants)
	if err != nil {
		return ComparisonMatch{}, err
	}
	if comparison.whole {
		before := comparison.before.Equal(current) && comparison.beforeState.SameRootMetadata(observed)
		expected := comparison.expected.Equal(current) && comparison.expectedState.SameRootMetadata(observed)
		if err := ctx.Err(); err != nil {
			return ComparisonMatch{}, err
		}
		return ComparisonMatch{before: before, expected: expected, recoverable: before || expected}, nil
	}
	partial, err := recovery.MatchPartialDocumentStates(comparison.beforeState, comparison.expectedState, observed)
	if err != nil {
		return ComparisonMatch{}, err
	}
	match := ComparisonMatch{
		before: partial.MatchesBefore(), expected: partial.MatchesExpected(), recoverable: partial.Recoverable(),
	}
	if comparison.order != nil {
		document, err := readPiComparisonDocument(ctx, current, comparison.limits)
		if err != nil {
			return ComparisonMatch{}, err
		}
		before, expected := comparison.order.MatchesBefore(document), comparison.order.MatchesExpected(document)
		match.before = match.before && before
		match.expected = match.expected && expected
		match.recoverable = match.recoverable && (before || expected)
	}
	if err := ctx.Err(); err != nil {
		return ComparisonMatch{}, err
	}
	return match, nil
}

func readPiComparisonDocument(ctx context.Context, document aggregate.Document, limits configdocument.Limits) (piconfig.Document, error) {
	content := document.Content()
	if _, err := configdocument.Parse(ctx, content, configdocument.JSON, limits); err != nil {
		return piconfig.Document{}, err
	}
	return piconfig.Parse(content)
}

func ValidateImage(ctx context.Context, document aggregate.Document, limits configdocument.Limits) error {
	if ctx == nil {
		return fmt.Errorf("document comparison context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if err := document.Validate(); err != nil {
		return err
	}
	if document.ContentSize() > limits.MaximumBytes {
		return fmt.Errorf("document comparison exceeds the selected byte limit")
	}
	return nil
}

func readComparisonState(ctx context.Context, document aggregate.Document, mode *recovery.PermissionMode, limits configdocument.Limits, participants []compose.Participant) (recovery.PartialDocumentState, error) {
	var snapshots []aggregate.Snapshot
	if len(participants) != 0 {
		baselines, err := compose.ReadBaselines(ctx, document, limits, participants)
		if err != nil {
			return recovery.PartialDocumentState{}, err
		}
		for _, baseline := range baselines {
			snapshots = append(snapshots, baseline.Snapshot())
		}
	}
	return recovery.NewPartialDocumentState(document.Exists(), mode, snapshots)
}
