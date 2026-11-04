package mutation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// DirectoryEntryObservationPlan retains qualified read-only layout, not the
// result of a later observation or any mutation authority.
type DirectoryEntryObservationPlan struct {
	path   string
	depth  int
	layout rootedpath.PathLayout
}

func PrepareDirectoryEntryAuthorityBounded(ctx context.Context, path string, depth int, budget PhysicalObservationBudget) (DirectoryEntryObservationPlan, error) {
	if ctx == nil || budget == nil {
		return DirectoryEntryObservationPlan{}, fmt.Errorf("directory-entry preparation requires context and allocation")
	}
	if err := ctx.Err(); err != nil {
		return DirectoryEntryObservationPlan{}, err
	}
	if err := validatePathObservation(path, PathEffectDirectoryEntry); err != nil {
		return DirectoryEntryObservationPlan{}, err
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return DirectoryEntryObservationPlan{}, err
	}
	layout, err := rootedpath.CaptureDirectoryEntryLayoutBounded(absolute, depth, budget)
	if err != nil {
		return DirectoryEntryObservationPlan{}, err
	}
	if err := ctx.Err(); err != nil {
		return DirectoryEntryObservationPlan{}, err
	}
	return DirectoryEntryObservationPlan{path: path, depth: depth, layout: layout}, nil
}

func (plan DirectoryEntryObservationPlan) ObserveBounded(ctx context.Context, budget PhysicalObservationBudget) (DirectoryEntryAuthorityObservation, error) {
	if ctx == nil || budget == nil {
		return DirectoryEntryAuthorityObservation{}, fmt.Errorf("directory-entry observation requires context and allocation")
	}
	identity, err := observePreparedPath(ctx, plan.path, PathEffectDirectoryEntry, plan.layout, plan.depth, budget)
	if err != nil {
		return DirectoryEntryAuthorityObservation{}, err
	}
	return directoryEntryAuthorityObservation(identity)
}

func (plan DirectoryEntryObservationPlan) ReserveObservation(budget PhysicalObservationBudget) error {
	if budget == nil {
		return fmt.Errorf("directory-entry observation allocation is required")
	}
	return reservePreparedPath(plan.layout, PathEffectDirectoryEntry, plan.depth, budget)
}

func reservePreparedPath(layout rootedpath.PathLayout, effect PathEffect, depth int, budget PhysicalObservationBudget) error {
	if err := layout.ReserveObservation(depth, budget); err != nil {
		return err
	}
	if effect == PathEffectReferent {
		if err := rootedpath.ChargeAbsolutePath(layout.CanonicalPath(), depth, budget); err != nil {
			return err
		}
		if err := budget.AdmitPhysicalWork(0, 1, 0); err != nil {
			return err
		}
	}
	return reservePlatformIdentityWork(layout.CanonicalPath(), effect, depth, budget)
}

func observePreparedPath(ctx context.Context, path string, effect PathEffect, layout rootedpath.PathLayout, depth int, budget PhysicalObservationBudget) (canonicalPath, error) {
	if err := ctx.Err(); err != nil {
		return canonicalPath{}, err
	}
	physical, suffix, err := layout.ResolveBounded(depth, budget)
	if err != nil {
		return canonicalPath{}, err
	}
	selection := pathSelection{anchorPath: physical, missingComponents: suffix, finalEntryMayExist: len(suffix) == 1}
	if effect == PathEffectReferent {
		if err := rootedpath.ChargeAbsolutePath(layout.CanonicalPath(), depth, budget); err != nil {
			return canonicalPath{}, err
		}
		if err := budget.AdmitPhysicalWork(0, 1, 0); err != nil {
			return canonicalPath{}, err
		}
		info, err := os.Lstat(layout.CanonicalPath())
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return canonicalPath{}, fmt.Errorf("prepared referent became an alias")
			}
			selection = pathSelection{anchorPath: layout.CanonicalPath()}
		} else if !errors.Is(err, os.ErrNotExist) {
			return canonicalPath{}, err
		}
	}
	return canonicalPathIdentityFromSelectionBounded(path, selection, effect, depth, budget)
}
