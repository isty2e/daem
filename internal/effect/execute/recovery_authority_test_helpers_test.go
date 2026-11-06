package execute

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
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
	active, err := journalinventory.CaptureActiveJournalAuthority(
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

func prepareRecoveryBackupsForTest(
	t *testing.T,
	ctx context.Context,
	authority *mutationAuthority,
	plan recovery.Plan,
	actions []hostmutation.RestorationAction,
) {
	t.Helper()
	active := bindRecoveryBackupSourceForTest(t, ctx, authority, plan)
	if err := authority.hostBackups().Prepare(ctx, plan.OperationDir(), hostmutation.RestorationBackupRequests(actions), active); err != nil {
		t.Fatal(err)
	}
}

func bindRecoveryBackupSourceForTest(
	t *testing.T,
	ctx context.Context,
	authority *mutationAuthority,
	plan recovery.Plan,
) journalinventory.ActiveJournalAuthority {
	t.Helper()
	parent := filepath.Dir(plan.OperationDir())
	root, err := rootedpath.CaptureRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := rootedpath.BindSelectedEntryAuthority(root, parent, plan.OperationDir())
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	active, err := journalinventory.CaptureActiveJournalAuthority(ctx, authority.filesystem, entry)
	closeErr := errors.Join(entry.Close(), root.Close())
	if err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	fingerprint, err := plan.JournalAuthorityFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.setJournalExecutionBasis(fingerprint, active); err != nil {
		t.Fatal(err)
	}
	return active
}

func beginUnclassifiedRecoveryExecutionForTest(t *testing.T, authority *mutationAuthority) {
	t.Helper()
	if authority.preparedRetirement == nil {
		if err := authority.physicalWorkBudget.ConcludeRetirementNotApplicable(); err != nil {
			t.Fatal(err)
		}
	}
	if err := authority.removals.PrepareCleanup(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.physicalWorkBudget.BeginReservedSemanticExecution(); err != nil {
		t.Fatal(err)
	}
	host, control, err := authority.physicalWorkBudget.BeginGeneralExecution()
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.generalTraversalPhase.Advance(control, host); err != nil {
		t.Fatal(err)
	}
	authority.generalExecutionWorkBudget = host
}

func installGeneralRecoveryExecutionBudgetForTest(t *testing.T, authority *mutationAuthority) {
	t.Helper()
	parent, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := workmeasure.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for range 16 {
		if err := parent.ReserveGeneralFileObservation(empty); err != nil {
			t.Fatal(err)
		}
	}
	if err := parent.ConcludeScratchCleanupNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.BeginReservedCleanupLifecycle(); err != nil {
		t.Fatal(err)
	}
	if err := parent.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	host, _, err := parent.BeginGeneralExecution()
	if err != nil {
		t.Fatal(err)
	}
	pathBudget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	authority.generalExecutionWorkBudget = host
	if err := authority.generalTraversalPhase.Advance(authority.physicalWorkBudget, pathBudget); err != nil {
		t.Fatal(err)
	}
}

func mustObservedPathAuthority(t *testing.T, path string) pathauthority.Exact {
	t.Helper()
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatalf("ObservePersistedDirectoryEntryAuthority(%q): %v", path, err)
	}
	return authority.Exact()
}
