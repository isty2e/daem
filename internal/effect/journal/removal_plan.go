package journal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func assessRemovalCleanupPlan(
	ctx context.Context,
	plan recovery.Plan,
	options PlanLoadOptions,
	budget *recovery.PhysicalWorkBudget,
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
	budget *recovery.PhysicalWorkBudget,
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
		recovery.MaximumPhysicalPathDepth,
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
	namespace, err := ObserveRemovalNamespace(ctx, root, destination, intent.Namespace(), budget)
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
		if obligation.Readiness() == recovery.RemovalCleanupReady {
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
	work  recovery.ArtifactWork
}

func observeRemovalSlotForPlan(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	root *rootedpath.CapturedRoot,
	slotPath string,
	budget *recovery.PhysicalWorkBudget,
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
	budget *recovery.PhysicalWorkBudget,
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
	budget *recovery.PhysicalWorkBudget,
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
	budget *recovery.PhysicalWorkBudget,
	destination rootedpath.Destination,
	work recovery.ArtifactWork,
	directory bool,
) (recovery.ForwardRemovalCapacity, error) {
	validationWork, err := destination.ParentChainValidationWork()
	if err != nil {
		return recovery.ForwardRemovalCapacity{}, err
	}
	return budget.ReserveForwardRemoval(work, directory, validationWork)
}

// ReserveRootedCleanupWork charges the complete bounded storage cleanup
// envelope for one effect-time candidate.
func ReserveRootedCleanupWork(
	budget *recovery.PhysicalWorkBudget,
	destination rootedpath.Destination,
	work recovery.ArtifactWork,
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
	budget *recovery.PhysicalWorkBudget,
	destination rootedpath.Destination,
	work recovery.ArtifactWork,
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
	budget *recovery.PhysicalWorkBudget,
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
	budget *recovery.PhysicalWorkBudget,
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
	budget *recovery.PhysicalWorkBudget,
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
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
}

// ObserveRemovalNamespace compares fresh parent facts with the exact durable
// namespace relation. It performs no slot observation or mutation.
func ObserveRemovalNamespace(
	ctx context.Context,
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	namespace recovery.RemovalNamespaceAuthority,
	budget *recovery.PhysicalWorkBudget,
) (recovery.RemovalNamespaceObservation, error) {
	if err := ctx.Err(); err != nil {
		return recovery.RemovalNamespaceObservation{}, err
	}
	current, err := root.AuthorityBounded(budget)
	if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
		return recovery.RemovalNamespaceObservation{}, cancellationErr
	}
	if err != nil {
		if !isRootedRemovalAuthorityFailure(err) {
			return recovery.RemovalNamespaceObservation{}, err
		}
		status, detail := classifyRemovalRootObservationFailure(err)
		return newRemovalNamespaceObservation(status, detail)
	}
	if !current.Equal(destination.Root()) {
		return newRemovalNamespaceObservation(
			recovery.RemovalNamespaceChanged,
			"destination belongs to a different captured root",
		)
	}
	hostPath, err := destination.LexicalPath()
	if err != nil {
		return recovery.RemovalNamespaceObservation{}, err
	}
	switch namespace.Variant() {
	case recovery.RemovalNamespaceExistingParent:
		parent, parentPresent := namespace.ParentProvenance()
		if !parentPresent {
			return recovery.RemovalNamespaceObservation{}, fmt.Errorf("existing-parent removal namespace has no parent provenance")
		}
		parentPath := parent.PhysicalRoot()
		if filepath.Clean(filepath.Dir(filepath.Clean(hostPath))) != filepath.Clean(parentPath) {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"destination parent no longer matches captured existing parent",
			)
		}
		persisted, err := rootedpath.NewAuthorityProvenance(
			parent.PhysicalRoot(), parent.ObjectFingerprint(), parent.MountFingerprint(),
		)
		if err != nil {
			return recovery.RemovalNamespaceObservation{}, err
		}
		currentParent, err := observePersistedRemovalRoot(current, persisted, budget)
		if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
			return recovery.RemovalNamespaceObservation{}, cancellationErr
		}
		if err != nil {
			if !isRootedRemovalAuthorityFailure(err) {
				return recovery.RemovalNamespaceObservation{}, err
			}
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"captured parent authority no longer matches; unlink and relocation cannot be distinguished",
			)
		}
		if err := persisted.Match(currentParent); err != nil {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"captured parent authority no longer matches; unlink and relocation cannot be distinguished",
			)
		}
		return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
	case recovery.RemovalNamespaceInitiallyAbsentParent:
		ancestor, present := namespace.RetainedAncestorProvenance()
		if !present {
			return recovery.RemovalNamespaceObservation{}, fmt.Errorf("initially-absent removal namespace has no retained ancestor provenance")
		}
		wantParent := filepath.Join(ancestor.PhysicalRoot(), filepath.FromSlash(namespace.MissingSuffix()))
		parent := filepath.Dir(filepath.Clean(hostPath))
		if filepath.Clean(wantParent) != parent {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"destination parent no longer matches captured missing suffix",
			)
		}
		persisted, err := rootedpath.NewAuthorityProvenance(
			ancestor.PhysicalRoot(), ancestor.ObjectFingerprint(), ancestor.MountFingerprint(),
		)
		if err != nil {
			return recovery.RemovalNamespaceObservation{}, err
		}
		currentAncestor, err := observePersistedRemovalRoot(current, persisted, budget)
		if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
			return recovery.RemovalNamespaceObservation{}, cancellationErr
		}
		if err != nil {
			if !isRootedRemovalAuthorityFailure(err) {
				return recovery.RemovalNamespaceObservation{}, err
			}
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"retained ancestor authority no longer matches",
			)
		}
		if err := persisted.Match(currentAncestor); err != nil {
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"retained ancestor authority no longer matches",
			)
		}
		if err := ctx.Err(); err != nil {
			return recovery.RemovalNamespaceObservation{}, err
		}
		if current.PhysicalRoot() == filepath.Clean(parent) {
			return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
		}
		parentRoot, err := rootedpath.CaptureRootNoFollowBounded(
			parent,
			recovery.MaximumPhysicalPathDepth,
			budget,
		)
		if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
			return recovery.RemovalNamespaceObservation{}, cancellationErr
		}
		if errors.Is(err, fs.ErrNotExist) {
			return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
		}
		if err != nil {
			var failure *rootedpath.Failure
			if errors.As(err, &failure) && failure.Kind() == rootedpath.FailureRootReplaced {
				return newRemovalNamespaceObservation(
					recovery.RemovalNamespaceChanged,
					"initially absent parent is not a directory",
				)
			}
			if !errors.As(err, &failure) {
				return recovery.RemovalNamespaceObservation{}, err
			}
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceUnavailable,
				"initially absent parent authority could not be captured",
			)
		}
		defer parentRoot.Close()
		currentParent, err := parentRoot.AuthorityBounded(budget)
		if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
			return recovery.RemovalNamespaceObservation{}, cancellationErr
		}
		if err != nil {
			if !isRootedRemovalAuthorityFailure(err) {
				return recovery.RemovalNamespaceObservation{}, err
			}
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceUnavailable,
				"initially absent parent authority could not be read",
			)
		}
		if err := persisted.MatchDescendant(currentParent); err != nil {
			if cancellationErr := removalNamespaceCancellation(ctx, err); cancellationErr != nil {
				return recovery.RemovalNamespaceObservation{}, cancellationErr
			}
			return newRemovalNamespaceObservation(
				recovery.RemovalNamespaceChanged,
				"initially absent parent authority no longer matches retained ancestor",
			)
		}
		if err := ctx.Err(); err != nil {
			return recovery.RemovalNamespaceObservation{}, err
		}
		return newRemovalNamespaceObservation(recovery.RemovalNamespaceMatched, "")
	default:
		return recovery.RemovalNamespaceObservation{}, fmt.Errorf("unsupported removal namespace variant %q", namespace.Variant())
	}
}

func isRootedRemovalAuthorityFailure(err error) bool {
	var failure *rootedpath.Failure
	return errors.As(err, &failure)
}

func observePersistedRemovalRoot(
	bound rootedpath.Authority,
	persisted rootedpath.AuthorityProvenance,
	budget *recovery.PhysicalWorkBudget,
) (rootedpath.Authority, error) {
	if filepath.Clean(bound.PhysicalRoot()) == filepath.Clean(persisted.PhysicalRoot()) {
		return bound, nil
	}
	root, err := rootedpath.CaptureRootNoFollowBounded(
		persisted.PhysicalRoot(),
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return rootedpath.Authority{}, err
	}
	defer root.Close()
	return root.AuthorityBounded(budget)
}

func classifyRemovalRootObservationFailure(
	err error,
) (recovery.RemovalNamespaceObservationStatus, string) {
	var failure *rootedpath.Failure
	if errors.As(err, &failure) &&
		(failure.Kind() == rootedpath.FailureRootReplaced ||
			failure.Kind() == rootedpath.FailureMountChanged) {
		return recovery.RemovalNamespaceChanged, "captured destination authority no longer matches"
	}
	return recovery.RemovalNamespaceUnavailable, "captured destination authority could not be observed"
}

func removalNamespaceCancellation(ctx context.Context, err error) error {
	if cancellationErr := removalObservationCancellation(err); cancellationErr != nil {
		return cancellationErr
	}
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func newRemovalNamespaceObservation(
	status recovery.RemovalNamespaceObservationStatus,
	detail string,
) (recovery.RemovalNamespaceObservation, error) {
	return recovery.NewRemovalNamespaceObservation(status, detail)
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
