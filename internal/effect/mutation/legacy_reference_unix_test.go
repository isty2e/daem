//go:build darwin || linux

package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestObserveLegacyReferenceBoundedKeepsFreshIdentityAndAccess(t *testing.T) {
	expected := legacyReferenceFixture(t)
	budget := &legacyReferenceBudget{}
	actual, err := ObserveLegacyReferenceBounded(t.Context(), expected.Exact(), mutationfs.MaximumPhysicalPathDepth, budget)
	if err != nil || !actual.Reference().Equal(expected.Reference()) || budget.calls == 0 {
		t.Fatal("legacy reference was not freshly and boundedly qualified", actual, budget, err)
	}
	missing := filepath.Join(filepath.Dir(expected.Reference().Locator()), "Missing.json")
	absent, err := ObservePersistedDirectoryEntryAuthority(missing)
	if err != nil {
		t.Fatal(err)
	}
	actual, err = ObserveLegacyReferenceBounded(t.Context(), absent.Exact(), mutationfs.MaximumPhysicalPathDepth, &legacyReferenceBudget{})
	if err != nil || !actual.Exact().Equal(absent.Exact()) || actual.Reference().Validate() != nil {
		t.Fatal("entry namespace observation was confused with file existence", err)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only legacy observation created an entry", err)
	}
}

func TestObserveLegacyReferenceBoundedRefusesNamespaceMismatchAndBudget(t *testing.T) {
	current := legacyReferenceFixture(t)
	other := pathtest.Exact(current.Exact().Key())
	if current.Exact().Equal(other) {
		other = pathtest.DarwinCaseSensitive(current.Exact().Key())
	}
	mismatchBudget := &legacyReferenceBudget{}
	actual, err := ObserveLegacyReferenceBounded(t.Context(), other, mutationfs.MaximumPhysicalPathDepth, mismatchBudget)
	if !errors.Is(err, ErrLegacyReferenceUnprovable) || !actual.Reference().IsZero() || mismatchBudget.calls == 0 {
		t.Fatal("fresh namespace mismatch was silently re-keyed", actual, err)
	}
	denied := errors.New("deny physical traversal")
	budget := &legacyReferenceBudget{deny: denied}
	actual, err = ObserveLegacyReferenceBounded(t.Context(), current.Exact(), mutationfs.MaximumPhysicalPathDepth, budget)
	if !errors.Is(err, denied) || !actual.Reference().IsZero() || budget.calls == 0 {
		t.Fatal("legacy observation bypassed its budget or returned partial evidence", actual, budget, err)
	}
	actual, err = ObserveLegacyReferenceBounded(t.Context(), current.Exact(), mutationfs.MaximumPhysicalPathDepth, nil)
	if err == nil || !actual.Reference().IsZero() {
		t.Fatal("legacy observation admitted an unbounded call", actual, err)
	}
}

func TestObserveLegacyReferenceBoundedRefusesSameFamilyAlias(t *testing.T) {
	expected := legacyReferenceFixture(t)
	path := expected.Reference().Locator()
	parent := filepath.Dir(path)
	moved := parent + "-moved"
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, parent); err != nil {
		t.Fatal(err)
	}
	current, err := ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	if current.Exact().Witness() != expected.Exact().Witness() || current.Exact().Equal(expected.Exact()) {
		t.Fatal("alias fixture did not expose a same-family identity change", current, expected)
	}
	budget := &legacyReferenceBudget{}
	actual, err := ObserveLegacyReferenceBounded(t.Context(), expected.Exact(), mutationfs.MaximumPhysicalPathDepth, budget)
	if !errors.Is(err, ErrLegacyReferenceUnprovable) || !actual.Reference().IsZero() || budget.calls == 0 {
		t.Fatal("fresh alias observation bypassed strict exact equality", actual, budget, err)
	}
}

func TestObserveLegacyReferenceBoundedCancellationPrecedesOtherFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	actual, err := ObserveLegacyReferenceBounded(ctx, pathauthority.Exact{}, 0, nil)
	if !errors.Is(err, context.Canceled) || !actual.Reference().IsZero() {
		t.Fatal("cancellation was lost to invalid or unavailable input", actual, err)
	}
	current := legacyReferenceFixture(t)
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	concurrentFailure := errors.New("simultaneous traversal failure")
	budget := &legacyReferenceBudget{cancel: cancel, deny: concurrentFailure}
	actual, err = ObserveLegacyReferenceBounded(ctx, current.Exact(), mutationfs.MaximumPhysicalPathDepth, budget)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, concurrentFailure) || !actual.Reference().IsZero() || budget.calls == 0 {
		t.Fatal("cancellation or its concurrent observation cause was lost", actual, budget, err)
	}
}

type legacyReferenceBudget struct {
	calls  int
	deny   error
	cancel context.CancelFunc
}

func (budget *legacyReferenceBudget) AdmitPathComponents(int) error {
	budget.calls++
	if budget.cancel != nil {
		budget.cancel()
	}
	return budget.deny
}
