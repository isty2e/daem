package qualification

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/removalnamespace"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output"
)

type RemovalReadOptions struct {
	Filesystem mutationfs.RootedReader
	Resolver   func(output.Destination) (string, error)
}

func AssessRemovalCleanupPlan(
	ctx context.Context,
	plan recovery.Plan,
	options RemovalReadOptions,
	budget *physicalwork.Budget,
) (recovery.Plan, error) {
	intents := plan.RemovalIntents()
	assessed := make([]recovery.RemovalCleanupObligation, 0, len(intents))
	for index, intent := range intents {
		if err := ctx.Err(); err != nil {
			return recovery.Plan{}, err
		}
		if err := budget.AdmitObservation(); err != nil {
			return recovery.Plan{}, err
		}
		obligation, err := assessRemovalCleanupIntent(ctx, intent, options, budget)
		if err != nil {
			return recovery.Plan{}, fmt.Errorf("assess removal intent[%d] cleanup: %w", index, err)
		}
		assessed = append(assessed, obligation)
	}
	return plan.WithRemovalCleanupAssessment(assessed)
}

func assessRemovalCleanupIntent(
	ctx context.Context,
	intent recovery.RemovalIntent,
	options RemovalReadOptions,
	budget *physicalwork.Budget,
) (recovery.RemovalCleanupObligation, error) {
	return observeRemovalCleanupIntent(ctx, intent, options, budget, true)
}

func ObserveRemovalCleanupIntent(ctx context.Context, intent recovery.RemovalIntent, options RemovalReadOptions, budget *physicalwork.Budget) (recovery.RemovalCleanupObligation, error) {
	return observeRemovalCleanupIntent(ctx, intent, options, budget, false)
}

func observeRemovalCleanupIntent(
	ctx context.Context,
	intent recovery.RemovalIntent,
	options RemovalReadOptions,
	budget *physicalwork.Budget,
	reserveExecution bool,
) (recovery.RemovalCleanupObligation, error) {
	if options.Resolver == nil {
		return unavailableRemovalCleanupObligation(intent, "destination resolver is unavailable")
	}
	hostPath, err := options.Resolver(intent.Destination())
	if err != nil {
		return unavailableRemovalCleanupObligation(intent, "destination could not be resolved")
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(
		hostPath,
		physicalwork.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return unavailableRemovalCleanupObligation(intent, "destination authority could not be captured")
	}
	defer root.Close()
	physicalPath, err := destination.LexicalPath()
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	namespace, err := removalnamespace.Observe(ctx, root, destination, intent.Namespace(), budget)
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	entries, err := unavailableRemovalResidueObservation()
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	if namespace.Status() == recovery.RemovalNamespaceMatched {
		residuePath, cleanupPath, err := removalnamespace.Paths(intent.Namespace())
		if err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
		residue, err := observeRemovalSlotForPlan(
			ctx, options.Filesystem, root, residuePath, budget, "residue",
		)
		if err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
		cleanup, err := observeRemovalSlotForPlan(
			ctx, options.Filesystem, root, cleanupPath, budget, "cleanup stage",
		)
		if err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
		entries = recovery.NewRemovalResidueObservation(residue.entry, cleanup.entry)
		obligation, err := intent.AssessCleanup(namespace, entries)
		if err != nil {
			return recovery.RemovalCleanupObligation{}, err
		}
		work := cleanup.work
		kind := cleanup.entry.Kind()
		if obligation.Action() == recovery.RemovalCleanupActionPromoteResidue {
			work = residue.work
			kind = residue.entry.Kind()
		}
		if reserveExecution && obligation.Readiness() == recovery.RemovalCleanupReady {
			if err := removalnamespace.ReserveRemovalExecutionObservationWork(budget, physicalPath, residuePath, cleanupPath); err != nil {
				return recovery.RemovalCleanupObligation{}, err
			}
			var reserveErr error
			if kind == recovery.PathKindDirectory {
				reserveErr = budget.ReserveDirectoryReobservation(work)
			} else {
				reserveErr = budget.ReserveReobservation(work)
			}
			if reserveErr != nil {
				return recovery.RemovalCleanupObligation{}, reserveErr
			}
			if kind == recovery.PathKindDirectory || kind == recovery.PathKindFile {
				if err := removalnamespace.ReserveRootedCleanupWork(
					budget,
					destination,
					work,
					kind == recovery.PathKindDirectory,
				); err != nil {
					return recovery.RemovalCleanupObligation{}, err
				}
			}
		}
		return obligation, nil
	}
	return intent.AssessCleanup(namespace, entries)
}

func unavailableRemovalCleanupObligation(
	intent recovery.RemovalIntent,
	detail string,
) (recovery.RemovalCleanupObligation, error) {
	namespace, err := recovery.NewRemovalNamespaceObservation(
		recovery.RemovalNamespaceUnavailable,
		detail,
	)
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	entries, err := unavailableRemovalResidueObservation()
	if err != nil {
		return recovery.RemovalCleanupObligation{}, err
	}
	return intent.AssessCleanup(namespace, entries)
}

func unavailableRemovalResidueObservation() (recovery.RemovalResidueObservation, error) {
	residue, err := recovery.NewRemovalResidueEntryObservation(
		recovery.RemovalResidueEntryUnavailable,
		"", "", nil, "", "residue was not observed",
	)
	if err != nil {
		return recovery.RemovalResidueObservation{}, err
	}
	cleanup, err := recovery.NewRemovalResidueEntryObservation(
		recovery.RemovalResidueEntryUnavailable,
		"", "", nil, "", "cleanup stage was not observed",
	)
	if err != nil {
		return recovery.RemovalResidueObservation{}, err
	}
	return recovery.NewRemovalResidueObservation(residue, cleanup), nil
}

type removalPlanSlotObservation struct {
	entry recovery.RemovalResidueEntryObservation
	work  workmeasure.ArtifactWork
}

func observeRemovalSlotForPlan(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	root *rootedpath.CapturedRoot,
	slotPath string,
	budget *physicalwork.Budget,
	role string,
) (removalPlanSlotObservation, error) {
	capability, err := acquireRemovalObservationSlot(root, slotPath, budget)
	if err != nil {
		entry, entryErr := unavailableRemovalEntry(role + " authority could not be bound")
		return removalPlanSlotObservation{entry: entry}, entryErr
	}
	entry, _, work, observeErr := ObserveRootedRemovalEntry(
		ctx,
		filesystem,
		capability,
		budget,
		budget.RemainingTreeWork(),
	)
	closeErr := capability.Close()
	if observeErr != nil {
		return removalPlanSlotObservation{}, observeErr
	}
	if closeErr != nil {
		entry, entryErr := unavailableRemovalEntry(role + " authority could not be closed")
		return removalPlanSlotObservation{entry: entry}, entryErr
	}
	return removalPlanSlotObservation{entry: entry, work: work}, nil
}

func unavailableRemovalEntry(detail string) (recovery.RemovalResidueEntryObservation, error) {
	return recovery.NewRemovalResidueEntryObservation(
		recovery.RemovalResidueEntryUnavailable,
		"", "", nil, "", detail,
	)
}

func acquireRemovalObservationSlot(
	root *rootedpath.CapturedRoot,
	slotPath string,
	budget *physicalwork.Budget,
) (rootedpath.CommitCapability, error) {
	rootAuthority, err := root.AuthorityBounded(budget)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(rootAuthority.PhysicalRoot(), filepath.Clean(slotPath))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("removal slot escaped retained root")
	}
	rootRelative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(relative))
	if err != nil {
		return nil, err
	}
	bound, err := rootAuthority.Bind(rootRelative)
	if err != nil {
		return nil, err
	}
	return root.AcquireBounded(
		bound,
		physicalwork.MaximumPhysicalPathDepth,
		budget,
	)
}
