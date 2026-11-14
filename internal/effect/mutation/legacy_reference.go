package mutation

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

var ErrLegacyReferenceUnprovable = errors.New("legacy path reference cannot be proven")

// ObserveLegacyReferenceBounded qualifies an encoding-proven candidate against
// the current exact namespace. It does not re-key an owner, prove file existence
// or kind, replace a recorded locator, or supply lease/effect authority. A missing
// namespace is lookup evidence, not an independently chosen creation spelling.
func ObserveLegacyReferenceBounded(
	ctx context.Context,
	expected pathauthority.Exact,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (PersistedDirectoryEntryAuthority, error) {
	if ctx == nil {
		return PersistedDirectoryEntryAuthority{}, fmt.Errorf("legacy reference observation requires context")
	}
	if err := ctx.Err(); err != nil {
		return PersistedDirectoryEntryAuthority{}, err
	}
	if budget == nil || maximumPhysicalDepth <= 0 {
		return PersistedDirectoryEntryAuthority{}, fmt.Errorf("legacy reference observation requires bounded traversal")
	}
	candidate, available := expected.LegacyLocatorCandidate()
	if !available {
		return PersistedDirectoryEntryAuthority{}, fmt.Errorf("%w for %q: encoding does not retain proven access", ErrLegacyReferenceUnprovable, expected.Key())
	}
	observed, err := ObservePersistedDirectoryEntryAuthorityBounded(candidate, maximumPhysicalDepth, budget)
	if cancellation := ctx.Err(); cancellation != nil {
		return PersistedDirectoryEntryAuthority{}, errors.Join(cancellation, err)
	}
	if err != nil {
		return PersistedDirectoryEntryAuthority{}, err
	}
	if !observed.Exact().Equal(expected) {
		return PersistedDirectoryEntryAuthority{}, fmt.Errorf("%w for %q: current namespace differs", ErrLegacyReferenceUnprovable, expected.Key())
	}
	return observed, nil
}
