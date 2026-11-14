package document

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

// Restoration is a bound content/metadata candidate, not permission to
// mutate a host path or a decision about the durable recovery phase.
type Restoration struct {
	document        aggregate.Document
	root            recovery.PartialDocumentState
	valueFootprints []output.Footprint
	existence       bool
	mode            bool
	changed         bool
	initialized     bool
}

func (restoration Restoration) Validate() error {
	if !restoration.initialized {
		return fmt.Errorf("document restoration is required")
	}
	return nil
}

func (restoration Restoration) Document() aggregate.Document { return restoration.document }
func (restoration Restoration) Changed() bool                { return restoration.changed }
func (restoration Restoration) ValueFootprints() []output.Footprint {
	return slices.Clone(restoration.valueFootprints)
}
func (restoration Restoration) ChangesExistence() bool { return restoration.existence }
func (restoration Restoration) ChangesMode() bool      { return restoration.mode }
func (restoration Restoration) RegularFileMode() (recovery.PermissionMode, bool) {
	return restoration.root.RegularFileMode()
}

// RestoreBefore edits only the selected actual-before areas. Its caller must
// independently establish commit phase, namespace and publication authority.
func (comparison Comparison) RestoreBefore(ctx context.Context, current aggregate.Document, mode *recovery.PermissionMode) (Restoration, error) {
	match, err := comparison.Match(ctx, current, mode)
	if err != nil {
		return Restoration{}, err
	}
	currentRoot, err := recovery.NewPartialDocumentState(current.Exists(), mode, nil)
	if err != nil {
		return Restoration{}, err
	}
	if !currentRoot.SameRootMetadata(comparison.beforeState) && !currentRoot.SameRootMetadata(comparison.expectedState) {
		return Restoration{}, fmt.Errorf("document root differs from both recorded states")
	}
	if match.MatchesBefore() {
		return NewRestoration(current, mode, current, mode)
	}
	if comparison.whole {
		if !match.Recoverable() {
			return Restoration{}, fmt.Errorf("whole document differs from both recorded states")
		}
		BeforeMode := comparison.BeforeMode()
		return NewRestoration(current, mode, comparison.before, BeforeMode)
	}

	candidate := current
	var sequenceFootprints []output.Footprint
	for _, sequence := range comparison.sequences {
		observed, err := readArrayComparisonDocument(ctx, candidate, comparison.limits, sequence.Syntax())
		if err != nil {
			return Restoration{}, err
		}
		restored, changed, err := sequence.Restore(ctx, observed)
		if err != nil {
			return Restoration{}, err
		}
		if changed {
			footprint, err := output.NewKeyFootprint(sequence.ArrayPath())
			if err != nil {
				return Restoration{}, err
			}
			sequenceFootprints = append(sequenceFootprints, footprint)
		}
		candidate = aggregate.ExistingDocument(restored.Bytes())
	}
	if len(comparison.participants) != 0 {
		baselines, err := compose.ReadBaselines(ctx, comparison.before, comparison.limits, comparison.participants)
		if err != nil {
			return Restoration{}, err
		}
		restored, err := compose.Restore(ctx, candidate, comparison.limits, baselines)
		if err != nil {
			return Restoration{}, err
		}
		candidate = restored.Document()
	}
	if err := compose.ValidateImage(ctx, candidate, comparison.limits); err != nil {
		return Restoration{}, err
	}
	for _, sequence := range comparison.sequences {
		observed, err := readArrayComparisonDocument(ctx, candidate, comparison.limits, sequence.Syntax())
		if err != nil {
			return Restoration{}, err
		}
		matchesBefore, err := sequence.MatchesBefore(ctx, observed)
		if err != nil {
			return Restoration{}, err
		}
		if !matchesBefore {
			return Restoration{}, fmt.Errorf("restored document lost an original native array")
		}
	}
	candidateMode, err := comparison.selectedRestorationMode(candidate, current, mode)
	if err != nil {
		return Restoration{}, err
	}
	restoration, err := NewRestoration(current, mode, candidate, candidateMode)
	if err != nil {
		return Restoration{}, err
	}
	if !restoration.root.SameRootMetadata(comparison.beforeState) && !restoration.root.SameRootMetadata(comparison.expectedState) {
		return Restoration{}, fmt.Errorf("restored root is outside the recorded metadata envelope")
	}
	// A codec can retain a foreign parent absent from its original baseline.
	// Only an exact no-op can recognize that retry without waiving interference.
	if !match.Recoverable() && restoration.Changed() {
		return Restoration{}, fmt.Errorf("selected document differs from both recorded states")
	}
	final, err := readComparisonState(ctx, candidate, candidateMode, comparison.limits, comparison.participants)
	if err != nil {
		return Restoration{}, err
	}
	footprints, err := match.observed.ChangedValueFootprints(final)
	if err != nil {
		return Restoration{}, err
	}
	footprints = append(footprints, sequenceFootprints...)
	slices.SortFunc(footprints, output.CompareFootprints)
	restoration.valueFootprints = slices.Compact(footprints)
	if restoration.Changed() && len(restoration.valueFootprints) == 0 && !restoration.existence && !restoration.mode {
		return Restoration{}, fmt.Errorf("restoration changed outside its selected value and root envelope")
	}
	if err := ctx.Err(); err != nil {
		return Restoration{}, err
	}
	return restoration, nil
}

func (comparison Comparison) BeforeMode() *recovery.PermissionMode {
	mode, present := comparison.beforeState.RegularFileMode()
	if !present {
		return nil
	}
	return &mode
}

func (comparison Comparison) selectedRestorationMode(candidate, current aggregate.Document, mode *recovery.PermissionMode) (*recovery.PermissionMode, error) {
	if !candidate.Exists() {
		return nil, nil
	}
	if comparison.before.Exists() && comparison.expected.Exists() && !comparison.beforeState.SameRootMetadata(comparison.expectedState) {
		return comparison.BeforeMode(), nil
	}
	if current.Exists() {
		return mode, nil
	}
	if mode := comparison.BeforeMode(); mode != nil {
		return mode, nil
	}
	if mode, present := comparison.expectedState.RegularFileMode(); present {
		return &mode, nil
	}
	return nil, fmt.Errorf("restored document has no recorded regular-file mode")
}

func NewRestoration(current aggregate.Document, mode *recovery.PermissionMode, candidate aggregate.Document, candidateMode *recovery.PermissionMode) (Restoration, error) {
	beforeRoot, err := recovery.NewPartialDocumentState(current.Exists(), mode, nil)
	if err != nil {
		return Restoration{}, err
	}
	root, err := recovery.NewPartialDocumentState(candidate.Exists(), candidateMode, nil)
	if err != nil {
		return Restoration{}, err
	}
	var footprints []output.Footprint
	if !current.Equal(candidate) {
		footprints = []output.Footprint{output.WholePathFootprint()}
	}
	return Restoration{
		document: candidate, root: root, valueFootprints: footprints,
		existence: current.Exists() != candidate.Exists(),
		mode:      current.Exists() && candidate.Exists() && !beforeRoot.SameRootMetadata(root),
		changed:   !current.Equal(candidate) || !beforeRoot.SameRootMetadata(root), initialized: true,
	}, nil
}
