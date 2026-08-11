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

func prepareRecoveryBackupsForTest(
	t *testing.T,
	ctx context.Context,
	authority *mutationAuthority,
	plan recovery.Plan,
	actions []recoveryHostAction,
) {
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
	active, err := journal.CaptureActiveJournalAuthority(ctx, authority.filesystem, entry)
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
	if err := authority.prepareRecoveryBackups(ctx, plan.OperationDir(), actions); err != nil {
		t.Fatal(err)
	}
}

func beginGeneralRecoveryExecutionForTest(t *testing.T, authority *mutationAuthority) {
	t.Helper()
	if authority.preparedRetirement == nil {
		if err := authority.physicalWorkBudget.ConcludeRetirementNotApplicable(); err != nil {
			t.Fatal(err)
		}
	}
	if authority.removalCleanupExecution == nil {
		execution, err := authority.physicalWorkBudget.BeginReservedCleanupLifecycle()
		if err != nil {
			t.Fatal(err)
		}
		authority.removalCleanupExecution = execution
	}
	if !authority.semanticWitness.initialized {
		semantic, err := authority.physicalWorkBudget.BeginReservedSemanticExecution()
		if err != nil {
			t.Fatal(err)
		}
		authority.semanticExecutionWorkBudget = semantic
		host, control, err := authority.physicalWorkBudget.BeginGeneralExecution()
		if err != nil {
			t.Fatal(err)
		}
		if err := authority.generalTraversalPhase.advance(control); err != nil {
			t.Fatal(err)
		}
		authority.hostExecutionTraversal = host
		authority.generalExecutionWorkBudget = host
		return
	}
	if err := authority.beginGeneralRecoveryExecution(); err != nil {
		t.Fatal(err)
	}
}

func installGeneralRecoveryExecutionBudgetForTest(t *testing.T, authority *mutationAuthority) {
	t.Helper()
	parent, err := recovery.NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := recovery.NewArtifactWork(0, 0)
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
	pathBudget, err := recovery.NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	authority.generalExecutionWorkBudget = host
	authority.hostExecutionTraversal = pathBudget
}

func mustObservedPathAuthority(t *testing.T, path string) pathauthority.Exact {
	t.Helper()
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatalf("ObservePersistedDirectoryEntryAuthority(%q): %v", path, err)
	}
	return authority.Exact()
}
