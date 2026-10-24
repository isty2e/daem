package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

// DocumentRestoration is a bound content/metadata candidate, not permission to
// mutate a host path or a decision about the durable recovery phase.
type DocumentRestoration struct {
	document    aggregate.Document
	root        recovery.PartialDocumentState
	changed     bool
	initialized bool
}

func (restoration DocumentRestoration) Validate() error {
	if !restoration.initialized {
		return fmt.Errorf("document restoration is required")
	}
	return nil
}

func (restoration DocumentRestoration) Document() aggregate.Document { return restoration.document }
func (restoration DocumentRestoration) Changed() bool                { return restoration.changed }
func (restoration DocumentRestoration) RegularFileMode() (recovery.PermissionMode, bool) {
	return restoration.root.RegularFileMode()
}

// RestoreBefore edits only the selected actual-before areas. Its caller must
// independently establish commit phase, namespace and publication authority.
func (comparison DocumentComparison) RestoreBefore(ctx context.Context, current aggregate.Document, mode *recovery.PermissionMode) (DocumentRestoration, error) {
	match, err := comparison.Match(ctx, current, mode)
	if err != nil {
		return DocumentRestoration{}, err
	}
	currentRoot, err := recovery.NewPartialDocumentState(current.Exists(), mode, nil)
	if err != nil {
		return DocumentRestoration{}, err
	}
	if !currentRoot.SameRootMetadata(comparison.beforeState) && !currentRoot.SameRootMetadata(comparison.expectedState) {
		return DocumentRestoration{}, fmt.Errorf("document root differs from both recorded states")
	}
	if match.MatchesBefore() {
		return newDocumentRestoration(current, mode, current, mode)
	}
	if comparison.whole {
		if !match.Recoverable() {
			return DocumentRestoration{}, fmt.Errorf("whole document differs from both recorded states")
		}
		beforeMode := comparison.beforeMode()
		return newDocumentRestoration(current, mode, comparison.before, beforeMode)
	}

	candidate := current
	if comparison.order != nil {
		observed, err := readPiComparisonDocument(ctx, candidate, comparison.limits)
		if err != nil {
			return DocumentRestoration{}, err
		}
		restored, _, err := comparison.order.Restore(observed)
		if err != nil {
			return DocumentRestoration{}, err
		}
		candidate = aggregate.ExistingDocument(restored)
	}
	if len(comparison.participants) != 0 {
		baselines, err := compose.ReadBaselines(ctx, comparison.before, comparison.limits, comparison.participants)
		if err != nil {
			return DocumentRestoration{}, err
		}
		restored, err := compose.Restore(ctx, candidate, comparison.limits, baselines)
		if err != nil {
			return DocumentRestoration{}, err
		}
		candidate = restored.Document()
	}
	if err := validateComparisonDocument(ctx, candidate, comparison.limits); err != nil {
		return DocumentRestoration{}, err
	}
	if comparison.order != nil {
		observed, err := readPiComparisonDocument(ctx, candidate, comparison.limits)
		if err != nil {
			return DocumentRestoration{}, err
		}
		if !comparison.order.MatchesBefore(observed) {
			return DocumentRestoration{}, fmt.Errorf("restored document lost the original Pi package array")
		}
	}
	candidateMode, err := comparison.selectedRestorationMode(candidate, current, mode)
	if err != nil {
		return DocumentRestoration{}, err
	}
	restoration, err := newDocumentRestoration(current, mode, candidate, candidateMode)
	if err != nil {
		return DocumentRestoration{}, err
	}
	if !restoration.root.SameRootMetadata(comparison.beforeState) && !restoration.root.SameRootMetadata(comparison.expectedState) {
		return DocumentRestoration{}, fmt.Errorf("restored root is outside the recorded metadata envelope")
	}
	// A codec can retain a foreign parent absent from its original baseline.
	// Only an exact no-op can recognize that retry without waiving interference.
	if !match.Recoverable() && restoration.Changed() {
		return DocumentRestoration{}, fmt.Errorf("selected document differs from both recorded states")
	}
	if err := ctx.Err(); err != nil {
		return DocumentRestoration{}, err
	}
	return restoration, nil
}

func (comparison DocumentComparison) beforeMode() *recovery.PermissionMode {
	mode, present := comparison.beforeState.RegularFileMode()
	if !present {
		return nil
	}
	return &mode
}

func (comparison DocumentComparison) selectedRestorationMode(candidate, current aggregate.Document, mode *recovery.PermissionMode) (*recovery.PermissionMode, error) {
	if !candidate.Exists() {
		return nil, nil
	}
	if current.Exists() {
		return mode, nil
	}
	if mode := comparison.beforeMode(); mode != nil {
		return mode, nil
	}
	if mode, present := comparison.expectedState.RegularFileMode(); present {
		return &mode, nil
	}
	return nil, fmt.Errorf("restored document has no recorded regular-file mode")
}

func newDocumentRestoration(current aggregate.Document, mode *recovery.PermissionMode, candidate aggregate.Document, candidateMode *recovery.PermissionMode) (DocumentRestoration, error) {
	beforeRoot, err := recovery.NewPartialDocumentState(current.Exists(), mode, nil)
	if err != nil {
		return DocumentRestoration{}, err
	}
	root, err := recovery.NewPartialDocumentState(candidate.Exists(), candidateMode, nil)
	if err != nil {
		return DocumentRestoration{}, err
	}
	return DocumentRestoration{
		document: candidate, root: root,
		changed: !current.Equal(candidate) || !beforeRoot.SameRootMetadata(root), initialized: true,
	}, nil
}
