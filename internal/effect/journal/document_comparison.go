package journal

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

type DocumentComparisonInput struct {
	Before       aggregate.Document
	Expected     aggregate.Document
	BeforeMode   *recovery.PermissionMode
	ExpectedMode *recovery.PermissionMode
	Limits       configdocument.Limits
}

// DocumentComparison binds actual loaded witnesses to their comparison extent.
// It supplies no namespace, managed-membership or publication authority.
type DocumentComparison struct {
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

type DocumentComparisonMatch struct {
	before      bool
	expected    bool
	recoverable bool
}

func (match DocumentComparisonMatch) MatchesBefore() bool   { return match.before }
func (match DocumentComparisonMatch) MatchesExpected() bool { return match.expected }
func (match DocumentComparisonMatch) Recoverable() bool     { return match.recoverable }

func (comparison DocumentComparison) Validate() error {
	if !comparison.initialized {
		return fmt.Errorf("document comparison is required")
	}
	return nil
}

func (comparison DocumentComparison) MaximumDocumentBytes() int64 {
	return comparison.limits.MaximumBytes
}

func NewWholeDocumentComparison(ctx context.Context, input DocumentComparisonInput) (DocumentComparison, error) {
	comparison, err := newDocumentComparison(ctx, input, nil)
	if err != nil {
		return DocumentComparison{}, err
	}
	comparison.whole = true
	return comparison, nil
}

// NewSelectedDocumentComparison keeps dedicated package order separate from
// aggregate selections. An empty selection compares only regular-root metadata.
func NewSelectedDocumentComparison(ctx context.Context, input DocumentComparisonInput, participants []compose.Participant, order *piconfig.PackagePermutation) (DocumentComparison, error) {
	comparison, err := newDocumentComparison(ctx, input, participants)
	if err != nil {
		return DocumentComparison{}, err
	}
	if _, err := recovery.MatchPartialDocumentStates(comparison.beforeState, comparison.expectedState, comparison.beforeState); err != nil {
		return DocumentComparison{}, err
	}
	if order == nil {
		return comparison, nil
	}
	packages, err := output.NewKeyPath("packages")
	if err != nil {
		return DocumentComparison{}, err
	}
	for _, participant := range comparison.participants {
		for _, area := range participant.RewriteAreas() {
			if area.Overlaps(packages) {
				return DocumentComparison{}, fmt.Errorf("selected aggregate area overlaps dedicated Pi package order")
			}
		}
	}
	before, err := readPiComparisonDocument(ctx, input.Before, input.Limits)
	if err != nil {
		return DocumentComparison{}, err
	}
	expected, err := readPiComparisonDocument(ctx, input.Expected, input.Limits)
	if err != nil {
		return DocumentComparison{}, err
	}
	if !order.MatchesBefore(before) || !order.MatchesExpected(expected) {
		return DocumentComparison{}, fmt.Errorf("Pi package inverse does not match the loaded document witnesses")
	}
	if err := ctx.Err(); err != nil {
		return DocumentComparison{}, err
	}
	retained := *order
	comparison.order = &retained
	return comparison, nil
}

func newDocumentComparison(ctx context.Context, input DocumentComparisonInput, participants []compose.Participant) (DocumentComparison, error) {
	for _, document := range []aggregate.Document{input.Before, input.Expected} {
		if err := validateComparisonDocument(ctx, document, input.Limits); err != nil {
			return DocumentComparison{}, err
		}
	}
	before, err := readComparisonState(ctx, input.Before, input.BeforeMode, input.Limits, participants)
	if err != nil {
		return DocumentComparison{}, err
	}
	expected, err := readComparisonState(ctx, input.Expected, input.ExpectedMode, input.Limits, participants)
	if err != nil {
		return DocumentComparison{}, err
	}
	if err := ctx.Err(); err != nil {
		return DocumentComparison{}, err
	}
	return DocumentComparison{
		before: input.Before, expected: input.Expected,
		beforeState: before, expectedState: expected, limits: input.Limits,
		participants: append([]compose.Participant(nil), participants...), initialized: true,
	}, nil
}

func (comparison DocumentComparison) Match(ctx context.Context, current aggregate.Document, mode *recovery.PermissionMode) (DocumentComparisonMatch, error) {
	if err := comparison.Validate(); err != nil {
		return DocumentComparisonMatch{}, err
	}
	if err := validateComparisonDocument(ctx, current, comparison.limits); err != nil {
		return DocumentComparisonMatch{}, err
	}
	observed, err := readComparisonState(ctx, current, mode, comparison.limits, comparison.participants)
	if err != nil {
		return DocumentComparisonMatch{}, err
	}
	if comparison.whole {
		before := comparison.before.Equal(current) && comparison.beforeState.SameRootMetadata(observed)
		expected := comparison.expected.Equal(current) && comparison.expectedState.SameRootMetadata(observed)
		if err := ctx.Err(); err != nil {
			return DocumentComparisonMatch{}, err
		}
		return DocumentComparisonMatch{before: before, expected: expected, recoverable: before || expected}, nil
	}
	partial, err := recovery.MatchPartialDocumentStates(comparison.beforeState, comparison.expectedState, observed)
	if err != nil {
		return DocumentComparisonMatch{}, err
	}
	match := DocumentComparisonMatch{
		before: partial.MatchesBefore(), expected: partial.MatchesExpected(), recoverable: partial.Recoverable(),
	}
	if comparison.order != nil {
		document, err := readPiComparisonDocument(ctx, current, comparison.limits)
		if err != nil {
			return DocumentComparisonMatch{}, err
		}
		before, expected := comparison.order.MatchesBefore(document), comparison.order.MatchesExpected(document)
		match.before = match.before && before
		match.expected = match.expected && expected
		match.recoverable = match.recoverable && (before || expected)
	}
	if err := ctx.Err(); err != nil {
		return DocumentComparisonMatch{}, err
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

func validateComparisonDocument(ctx context.Context, document aggregate.Document, limits configdocument.Limits) error {
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
	if int64(len(document.Content())) > limits.MaximumBytes {
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
