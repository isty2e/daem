package execute

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func executeRecoveryPlanWithOptionsForTest(
	ctx context.Context,
	plan recovery.Plan,
	paths Paths,
	options RecoveryOptions,
) error {
	if options.ActiveJournalAuthority.Validate() == nil ||
		plan.Blocked() ||
		options.Resolver == nil ||
		options.StateCodec == nil ||
		options.Filesystem == nil ||
		(options.reloadPlan == nil && options.StateReader == nil) {
		return ExecuteRecoveryPlanWithOptions(ctx, plan, paths, options)
	}

	parent := filepath.Dir(plan.OperationDir())
	root, err := rootedpath.CaptureRoot(parent)
	if err != nil {
		return err
	}
	entry, err := rootedpath.BindSelectedEntryAuthority(
		root,
		parent,
		plan.OperationDir(),
	)
	if err != nil {
		return errors.Join(err, root.Close())
	}
	active, err := journal.CaptureActiveJournalAuthority(
		ctx,
		options.Filesystem,
		entry,
	)
	closeErr := errors.Join(entry.Close(), root.Close())
	if err != nil {
		return errors.Join(err, closeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	options.ActiveJournalAuthority = active
	return ExecuteRecoveryPlanWithOptions(ctx, plan, paths, options)
}

func mustObservedPathAuthority(t *testing.T, path string) pathauthority.Exact {
	t.Helper()
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatalf("ObservePersistedDirectoryEntryAuthority(%q): %v", path, err)
	}
	return authority.Exact()
}
