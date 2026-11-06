package removalnamespace

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

// ChargeRemovalPathWork normalizes a physical cleanup path into component work.
func ChargeRemovalPathWork(budget *physicalwork.Budget, value string) error {
	depth, err := removalPathComponentCount(value)
	if err != nil {
		return err
	}
	return budget.AdmitPathComponents(depth)
}

// ReserveForwardRemovalExecutionWork reserves the path visits reachable after
// one forward-removal state begins effects.
func ReserveForwardRemovalExecutionWork(budget *physicalwork.Budget, destinationPaths []string) error {
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

// ReserveForwardRootedCleanupWork charges an observation and the storage cleanup
// envelope for one reachable removal state.
func ReserveForwardRootedCleanupWork(budget *physicalwork.Budget, destination rootedpath.Destination, work workmeasure.ArtifactWork, directory bool) (physicalwork.ForwardRemovalCapacity, error) {
	validationWork, err := destination.ParentChainValidationWork()
	if err != nil {
		return physicalwork.ForwardRemovalCapacity{}, err
	}
	return budget.ReserveForwardRemoval(work, directory, validationWork)
}

// ReserveRootedCleanupWork charges the storage cleanup envelope for one candidate.
func ReserveRootedCleanupWork(budget *physicalwork.Budget, destination rootedpath.Destination, work workmeasure.ArtifactWork, directory bool) error {
	validationWork, err := destination.ParentChainValidationWork()
	if err != nil {
		return err
	}
	return budget.ReserveRootedCleanup(work, directory, validationWork)
}

// ReserveScratchRootedCleanupWork charges rollback-stage cleanup to the operation.
func ReserveScratchRootedCleanupWork(budget *physicalwork.Budget, destination rootedpath.Destination, work workmeasure.ArtifactWork) error {
	validationWork, err := destination.ParentChainValidationWork()
	if err != nil {
		return err
	}
	return budget.ReserveScratchCleanup(work, validationWork)
}

// ReserveRemovalExecutionObservationWork reserves the physical-path visits of a
// ready cleanup candidate after preflight.
func ReserveRemovalExecutionObservationWork(budget *physicalwork.Budget, destinationPath, residuePath, cleanupPath string) error {
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
	return budget.ReserveExecutionObservations(destinationDepth, residueDepth, cleanupDepth, mutationfs.RootedAbsencePathObservationCount)
}

// ReserveRemovalCleanupLifecycleWork reserves post-effect cleanup preflight and
// execution before any recovery effect.
func ReserveRemovalCleanupLifecycleWork(budget *physicalwork.Budget, destinationPath, residuePath, cleanupPath string) error {
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
	return budget.ReserveCleanupLifecycle(destinationDepth, residueDepth, cleanupDepth, mutationfs.RootedAbsencePathObservationCount)
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

// Paths returns the same-parent slots selected by durable namespace authority.
func Paths(namespace recovery.RemovalNamespaceAuthority) (string, string, error) {
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
	return filepath.Join(parent, namespace.Names().Residue()), filepath.Join(parent, namespace.Names().Cleanup()), nil
}
