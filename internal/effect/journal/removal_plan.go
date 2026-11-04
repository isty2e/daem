package journal

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/removalnamespace"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func assessRemovalCleanupPlan(
	ctx context.Context,
	plan recovery.Plan,
	options PlanLoadOptions,
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
	options PlanLoadOptions,
	budget *physicalwork.Budget,
) (recovery.RemovalCleanupObligation, error) {
	return observeRemovalCleanupIntent(ctx, intent, options, budget, true)
}

func observeRemovalCleanupIntent(
	ctx context.Context,
	intent recovery.RemovalIntent,
	options PlanLoadOptions,
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
		residuePath, cleanupPath, err := RemovalNamespacePaths(intent.Namespace())
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
			if err := ReserveRemovalExecutionObservationWork(budget, physicalPath, residuePath, cleanupPath); err != nil {
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
				if err := ReserveRootedCleanupWork(
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

// ChargeRemovalPathWork normalizes one physical cleanup path into bounded
// component work without moving path interpretation into the pure recovery
// model.
func ChargeRemovalPathWork(
	budget *physicalwork.Budget,
	value string,
) error {
	depth, err := removalPathComponentCount(value)
	if err != nil {
		return err
	}
	return budget.AdmitPathComponents(depth)
}

// ReserveForwardRemovalExecutionWork reserves every physical path visit that
// one reachable forward-removal state may perform after effects begin.
func ReserveForwardRemovalExecutionWork(
	budget *physicalwork.Budget,
	destinationPaths []string,
) error {
	depths := make([]int, 0, len(destinationPaths))
	for _, destinationPath := range destinationPaths {
		destinationDepth, err := removalPathComponentCount(destinationPath)
		if err != nil {
			return err
		}
		depths = append(depths, destinationDepth)
	}
	return budget.ReserveForwardExecutionPathWork(depths)
}

// ReserveForwardRootedCleanupWork charges one future observation plus the
// complete bounded storage cleanup envelope for a reachable removal state.
func ReserveForwardRootedCleanupWork(
	budget *physicalwork.Budget,
	destination rootedpath.Destination,
	work workmeasure.ArtifactWork,
	directory bool,
) (physicalwork.ForwardRemovalCapacity, error) {
	validationWork, err := destination.ParentChainValidationWork()
	if err != nil {
		return physicalwork.ForwardRemovalCapacity{}, err
	}
	return budget.ReserveForwardRemoval(work, directory, validationWork)
}

// ReserveRootedCleanupWork charges the complete bounded storage cleanup
// envelope for one effect-time candidate.
func ReserveRootedCleanupWork(
	budget *physicalwork.Budget,
	destination rootedpath.Destination,
	work workmeasure.ArtifactWork,
	directory bool,
) error {
	validationWork, err := destination.ParentChainValidationWork()
	if err != nil {
		return err
	}
	return budget.ReserveRootedCleanup(work, directory, validationWork)
}

// ReserveScratchRootedCleanupWork charges the complete rollback-stage cleanup
// envelope against the operation budget.
func ReserveScratchRootedCleanupWork(
	budget *physicalwork.Budget,
	destination rootedpath.Destination,
	work workmeasure.ArtifactWork,
) error {
	validationWork, err := destination.ParentChainValidationWork()
	if err != nil {
		return err
	}
	return budget.ReserveScratchCleanup(work, validationWork)
}

// ReserveRemovalExecutionObservationWork reserves every physical-path visit
// performed by one ready cleanup candidate after preflight.
func ReserveRemovalExecutionObservationWork(
	budget *physicalwork.Budget,
	destinationPath string,
	residuePath string,
	cleanupPath string,
) error {
	destinationDepth, err := removalPathComponentCount(destinationPath)
	if err != nil {
		return err
	}
	residueDepth, err := removalPathComponentCount(residuePath)
	if err != nil {
		return err
	}
	cleanupDepth, err := removalPathComponentCount(cleanupPath)
	if err != nil {
		return err
	}
	return budget.ReserveExecutionObservations(
		destinationDepth,
		residueDepth,
		cleanupDepth,
		mutationfs.RootedAbsencePathObservationCount,
	)
}

// ReserveRemovalCleanupLifecycleWork reserves the complete post-effect
// cleanup preflight and execution path envelope before any recovery effect.
func ReserveRemovalCleanupLifecycleWork(
	budget *physicalwork.Budget,
	destinationPath string,
	residuePath string,
	cleanupPath string,
) error {
	destinationDepth, err := removalPathComponentCount(destinationPath)
	if err != nil {
		return err
	}
	residueDepth, err := removalPathComponentCount(residuePath)
	if err != nil {
		return err
	}
	cleanupDepth, err := removalPathComponentCount(cleanupPath)
	if err != nil {
		return err
	}
	return budget.ReserveCleanupLifecycle(
		destinationDepth,
		residueDepth,
		cleanupDepth,
		mutationfs.RootedAbsencePathObservationCount,
	)
}

func removalPathComponentCount(value string) (int, error) {
	clean := filepath.Clean(value)
	if strings.TrimSpace(value) == "" || clean == "." {
		return 0, fmt.Errorf("removal path is required")
	}
	volume := filepath.VolumeName(clean)
	relative := strings.TrimPrefix(clean, volume)
	relative = strings.Trim(relative, string(filepath.Separator))
	if relative == "" {
		return 0, nil
	}
	return len(strings.Split(relative, string(filepath.Separator))), nil
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

// RemovalNamespacePaths returns the two exact same-parent slot paths selected
// by durable namespace authority.
func RemovalNamespacePaths(
	namespace recovery.RemovalNamespaceAuthority,
) (string, string, error) {
	var parent string
	switch namespace.Variant() {
	case recovery.RemovalNamespaceExistingParent:
		provenance, present := namespace.ParentProvenance()
		if !present {
			return "", "", fmt.Errorf("existing-parent namespace lacks parent provenance")
		}
		parent = provenance.PhysicalRoot()
	case recovery.RemovalNamespaceInitiallyAbsentParent:
		provenance, present := namespace.RetainedAncestorProvenance()
		if !present {
			return "", "", fmt.Errorf("initially-absent namespace lacks retained ancestor provenance")
		}
		parent = filepath.Join(provenance.PhysicalRoot(), filepath.FromSlash(namespace.MissingSuffix()))
	default:
		return "", "", fmt.Errorf("unsupported removal namespace variant %q", namespace.Variant())
	}
	return filepath.Join(parent, namespace.Names().Residue()),
		filepath.Join(parent, namespace.Names().Cleanup()), nil
}
