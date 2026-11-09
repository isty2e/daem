package namespace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// ReservationSelector retains a captured locator outcome without interpreting
// symlinks or parent components. Optional absence and failure remain distinct;
// neither a filesystem handle nor writer authority is retained.
type ReservationSelector struct {
	reference output.RootReference
	path      string
	source    string
	failure   error
	absent    bool
}

func NewReservationSelector(reference output.RootReference, selected string) (ReservationSelector, error) {
	if err := reference.Validate(); err != nil {
		return ReservationSelector{}, fmt.Errorf("reservation root reference: %w", err)
	}
	if strings.ContainsRune(selected, '\x00') || !filepath.IsAbs(selected) {
		return ReservationSelector{}, fmt.Errorf("reservation selector must be an absolute path without a NUL byte")
	}
	return ReservationSelector{reference: reference, path: selected, source: "selected location"}, nil
}

func UnavailableReservationSelector(reference output.RootReference, source string, cause error) (ReservationSelector, error) {
	if err := reference.Validate(); err != nil {
		return ReservationSelector{}, err
	}
	if cause == nil || source == "" {
		return ReservationSelector{}, fmt.Errorf("unavailable reservation selector requires its source and cause")
	}
	return ReservationSelector{reference: reference, source: source, failure: cause}, nil
}

func AbsentReservationSelector(reference output.RootReference, source string) (ReservationSelector, error) {
	contracts, err := configuration.NativeProtectionLocations()
	if err != nil {
		return ReservationSelector{}, err
	}
	for _, contract := range contracts {
		if contract.Reference() == reference && contract.Optional() && source != "" {
			return ReservationSelector{reference: reference, source: source, absent: true}, nil
		}
	}
	return ReservationSelector{}, fmt.Errorf("only a declared optional protection location may be absent")
}

func (selector ReservationSelector) DescribedBy(source string) ReservationSelector {
	if source != "" {
		selector.source = source
	}
	return selector
}

func (selector ReservationSelector) Reference() output.RootReference { return selector.reference }
func (selector ReservationSelector) Path() string                    { return selector.path }

type ReservationSelectors struct {
	selectors map[output.RootReference]ReservationSelector
}

func NewReservationSelectors(selectors []ReservationSelector) (ReservationSelectors, error) {
	selected := make(map[output.RootReference]ReservationSelector, len(selectors))
	for _, selector := range selectors {
		if err := selector.reference.Validate(); err != nil {
			return ReservationSelectors{}, fmt.Errorf("reservation selector: %w", err)
		}
		if selector.absent {
			if selector.path != "" || selector.failure != nil {
				return ReservationSelectors{}, fmt.Errorf("absent reservation selector has contradictory location data")
			}
			if _, err := AbsentReservationSelector(selector.reference, selector.source); err != nil {
				return ReservationSelectors{}, err
			}
		} else if selector.failure == nil {
			if _, err := NewReservationSelector(selector.reference, selector.path); err != nil {
				return ReservationSelectors{}, err
			}
		} else if selector.path != "" || selector.source == "" {
			return ReservationSelectors{}, fmt.Errorf("unavailable reservation selector has contradictory location data")
		}
		if previous, present := selected[selector.reference]; present {
			if previous.path != selector.path || previous.source != selector.source || previous.absent != selector.absent || previous.failure != nil || selector.failure != nil {
				return ReservationSelectors{}, fmt.Errorf("conflicting selectors for reservation root %q (%s)", selector.reference.Identity(), selector.reference.LocatorPolicy())
			}
		}
		selected[selector.reference] = selector
	}

	return ReservationSelectors{selectors: selected}, nil
}

type nativeRootCreation struct {
	selector   string
	physical   string
	provenance rootedpath.AuthorityProvenance
}

// CaptureRootSelector resolves the locator before any relative document Join,
// while retaining that expression for fresh qualification of the captured root.
func CaptureRootSelector(ctx context.Context, selector string, budget rootedpath.PhysicalTraversalBudget) (RootSelection, error) {
	if ctx == nil || budget == nil {
		return RootSelection{}, fmt.Errorf("native root locator requires context and original allocation")
	}
	if err := ctx.Err(); err != nil {
		return RootSelection{}, err
	}
	physical, err := rootedpath.ResolveRootSelectorBounded(selector, physicalwork.MaximumPhysicalPathDepth, budget)
	if canceled := ctx.Err(); canceled != nil {
		return RootSelection{}, canceled
	}
	if err != nil {
		return RootSelection{}, err
	}
	selection, err := CaptureRoot(ctx, physical, budget)
	if err != nil {
		return RootSelection{}, err
	}
	selection.literal = selector
	return selection, nil
}

func (selection RootSelection) qualifyLocator(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) error {
	if selection.endpoint != nil {
		return selection.endpoint.qualify(ctx, budget)
	}
	if selection.literal == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	physical, err := rootedpath.ResolveRootSelectorBounded(selection.literal, physicalwork.MaximumPhysicalPathDepth, budget)
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if err != nil || physical != selection.SelectedPath {
		return errors.Join(fmt.Errorf("native root locator no longer identifies its captured document base"), err)
	}
	return ctx.Err()
}

// CaptureRoot retains an existing selected directory or confines
// missing-root publication to that same selector beneath an existing anchor.
// It never creates the directory; publication remains a journaled host effect.
func CaptureRoot(ctx context.Context, selected string, budget rootedpath.PhysicalTraversalBudget) (RootSelection, error) {
	if ctx == nil || budget == nil || !filepath.IsAbs(selected) || filepath.Clean(selected) != selected {
		return RootSelection{}, fmt.Errorf("native root selection requires context, canonical selector and original allocation")
	}
	if err := ctx.Err(); err != nil {
		return RootSelection{}, err
	}
	root, err := rootedpath.CaptureCanonicalRootBounded(selected, physicalwork.MaximumPhysicalPathDepth, budget)
	if err == nil {
		return RootSelection{SelectedPath: selected, Root: root}, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return RootSelection{}, err
	}
	// This uncreated child asks the existing capture owner for the missing
	// selected directory's anchor, not a grant over an ambient parent tree.
	root, destination, err := rootedpath.CaptureCanonicalDestinationBounded(filepath.Join(selected, ".daem-root-selection"), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return RootSelection{}, err
	}
	path, err := destination.LexicalPath()
	if err != nil {
		return RootSelection{}, errors.Join(err, root.Close())
	}
	result, err := nativeRootSelectionAt(ctx, selected, filepath.Dir(path), root, budget)
	if err != nil {
		return RootSelection{}, errors.Join(err, root.Close())
	}
	return result, nil
}

func nativeRootSelectionAt(ctx context.Context, selector, physical string, root *rootedpath.CapturedRoot, budget rootedpath.PhysicalTraversalBudget) (RootSelection, error) {
	authority, err := root.AuthorityBounded(budget)
	if err != nil {
		return RootSelection{}, err
	}
	if authority.PhysicalRoot() == physical {
		if err := root.ValidateSelectionBounded(selector, physicalwork.MaximumPhysicalPathDepth, budget); err != nil {
			return RootSelection{}, err
		}
		return RootSelection{SelectedPath: selector, Root: root}, ctx.Err()
	}
	if !nativeRootContains(authority.PhysicalRoot(), physical) {
		return RootSelection{}, fmt.Errorf("native root anchor does not contain its selected prefix")
	}
	provenance, err := authority.Provenance()
	if err != nil {
		return RootSelection{}, err
	}
	return RootSelection{SelectedPath: selector, Root: root, creation: &nativeRootCreation{selector: selector, physical: physical, provenance: provenance}}, ctx.Err()
}

func (selection RootSelection) qualifyCreation(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) (string, error) {
	proof := selection.creation
	if proof == nil || selection.SelectedPath != proof.selector {
		return "", fmt.Errorf("native creation selection differs from its selector")
	}
	authority, err := selection.capturedRoot().AuthorityBounded(budget)
	if err != nil {
		return "", err
	}
	provenance, err := authority.Provenance()
	if err != nil || provenance != proof.provenance {
		return "", fmt.Errorf("native creation anchor incarnation changed")
	}
	if err := selection.capturedRoot().ValidateSelectionBounded(authority.PhysicalRoot(), physicalwork.MaximumPhysicalPathDepth, budget); err != nil {
		return "", err
	}
	physical, err := rootedpath.ResolveReferentPathBounded(proof.selector, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil || physical != proof.physical || !nativeRootContains(authority.PhysicalRoot(), physical) {
		return "", errors.Join(fmt.Errorf("native selected creation prefix changed"), err)
	}
	// A present selector must still be a directory, not a regular final entry.
	current, err := rootedpath.CaptureCanonicalRootBounded(proof.selector, physicalwork.MaximumPhysicalPathDepth, budget)
	if err == nil {
		err = current.Close()
	} else if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return "", err
	}
	return physical, ctx.Err()
}

func nativeRootContains(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	_, err = rootedpath.NewRelativeDestination(filepath.ToSlash(relative))
	return err == nil
}

func CaptureRecordedRoot(ctx context.Context, selected string, expected rootedpath.AuthorityProvenance, budget rootedpath.PhysicalTraversalBudget) (RootSelection, error) {
	if err := expected.Validate(); err != nil {
		return RootSelection{}, err
	}
	current, err := CaptureRootSelector(ctx, selected, budget)
	if err != nil {
		return RootSelection{}, err
	}
	authority, err := current.Root.AuthorityBounded(budget)
	if err != nil {
		return RootSelection{}, errors.Join(err, current.Root.Close())
	}
	provenance, err := authority.Provenance()
	if err != nil {
		return RootSelection{}, errors.Join(err, current.Root.Close())
	}
	if provenance == expected {
		return current, nil
	}
	physical, err := rootedpath.ResolveRootSelectorBounded(selected, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil || !nativeRootContains(expected.PhysicalRoot(), physical) {
		return RootSelection{}, errors.Join(fmt.Errorf("recorded native anchor does not contain the independently selected root"), err, current.Root.Close())
	}
	if err := current.Root.Close(); err != nil {
		return RootSelection{}, err
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(expected.PhysicalRoot(), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return RootSelection{}, err
	}
	authority, err = root.AuthorityBounded(budget)
	if err == nil {
		provenance, err = authority.Provenance()
	}
	if err != nil || provenance != expected {
		return RootSelection{}, errors.Join(fmt.Errorf("recorded native anchor incarnation changed"), err, root.Close())
	}
	result, err := nativeRootSelectionAt(ctx, physical, physical, root, budget)
	if err != nil {
		return RootSelection{}, errors.Join(err, root.Close())
	}
	result.literal = selected
	return result, nil
}
