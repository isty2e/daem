package journal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

type nativeRootCreation struct {
	selector   string
	physical   string
	provenance rootedpath.AuthorityProvenance
}

// CaptureNativeDocumentRoot retains an existing selected directory or confines
// missing-root publication to that same selector beneath an existing anchor.
// It never creates the directory; publication remains a journaled host effect.
func CaptureNativeDocumentRoot(ctx context.Context, selected string, budget rootedpath.PhysicalTraversalBudget) (NativeDocumentRoot, error) {
	if ctx == nil || budget == nil || !filepath.IsAbs(selected) || filepath.Clean(selected) != selected {
		return NativeDocumentRoot{}, fmt.Errorf("native root selection requires context, canonical selector and original allocation")
	}
	if err := ctx.Err(); err != nil {
		return NativeDocumentRoot{}, err
	}
	root, err := rootedpath.CaptureCanonicalRootBounded(selected, physicalwork.MaximumPhysicalPathDepth, budget)
	if err == nil {
		return NativeDocumentRoot{SelectedPath: selected, Root: root}, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return NativeDocumentRoot{}, err
	}
	// This uncreated child asks the existing capture owner for the missing
	// selected directory's anchor, not a grant over an ambient parent tree.
	root, destination, err := rootedpath.CaptureCanonicalDestinationBounded(filepath.Join(selected, ".daem-root-selection"), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return NativeDocumentRoot{}, err
	}
	path, err := destination.LexicalPath()
	if err != nil {
		return NativeDocumentRoot{}, errors.Join(err, root.Close())
	}
	result, err := nativeRootSelectionAt(ctx, selected, filepath.Dir(path), root, budget)
	if err != nil {
		return NativeDocumentRoot{}, errors.Join(err, root.Close())
	}
	return result, nil
}

func nativeRootSelectionAt(ctx context.Context, selector, physical string, root *rootedpath.CapturedRoot, budget rootedpath.PhysicalTraversalBudget) (NativeDocumentRoot, error) {
	authority, err := root.AuthorityBounded(budget)
	if err != nil {
		return NativeDocumentRoot{}, err
	}
	if authority.PhysicalRoot() == physical {
		if err := root.ValidateSelectionBounded(selector, physicalwork.MaximumPhysicalPathDepth, budget); err != nil {
			return NativeDocumentRoot{}, err
		}
		return NativeDocumentRoot{SelectedPath: selector, Root: root}, ctx.Err()
	}
	if !nativeRootContains(authority.PhysicalRoot(), physical) {
		return NativeDocumentRoot{}, fmt.Errorf("native root anchor does not contain its selected prefix")
	}
	provenance, err := authority.Provenance()
	if err != nil {
		return NativeDocumentRoot{}, err
	}
	return NativeDocumentRoot{SelectedPath: selector, Root: root, creation: &nativeRootCreation{selector: selector, physical: physical, provenance: provenance}}, ctx.Err()
}

func (selection NativeDocumentRoot) qualifyCreation(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) (string, error) {
	proof := selection.creation
	if proof == nil || selection.SelectedPath != proof.selector {
		return "", fmt.Errorf("native creation selection differs from its selector")
	}
	authority, err := selection.Root.AuthorityBounded(budget)
	if err != nil {
		return "", err
	}
	provenance, err := authority.Provenance()
	if err != nil || persistedNativeRoot(provenance) != persistedNativeRoot(proof.provenance) {
		return "", fmt.Errorf("native creation anchor incarnation changed")
	}
	if err := selection.Root.ValidateSelectionBounded(authority.PhysicalRoot(), physicalwork.MaximumPhysicalPathDepth, budget); err != nil {
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

func captureRecordedNativeRoot(ctx context.Context, selected string, expected recoveryRootProvenance, budget rootedpath.PhysicalTraversalBudget) (NativeDocumentRoot, error) {
	current, err := CaptureNativeDocumentRoot(ctx, selected, budget)
	if err != nil {
		return NativeDocumentRoot{}, err
	}
	authority, err := current.Root.AuthorityBounded(budget)
	if err != nil {
		return NativeDocumentRoot{}, errors.Join(err, current.Root.Close())
	}
	provenance, err := authority.Provenance()
	if err != nil {
		return NativeDocumentRoot{}, errors.Join(err, current.Root.Close())
	}
	if persistedNativeRoot(provenance) == expected {
		return current, nil
	}
	physical, err := rootedpath.ResolveReferentPathBounded(selected, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil || !nativeRootContains(expected.PhysicalRoot, physical) {
		return NativeDocumentRoot{}, errors.Join(fmt.Errorf("recorded native anchor does not contain the independently selected root"), err, current.Root.Close())
	}
	if err := current.Root.Close(); err != nil {
		return NativeDocumentRoot{}, err
	}
	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(expected.PhysicalRoot, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return NativeDocumentRoot{}, err
	}
	authority, err = root.AuthorityBounded(budget)
	if err == nil {
		provenance, err = authority.Provenance()
	}
	if err != nil || persistedNativeRoot(provenance) != expected {
		return NativeDocumentRoot{}, errors.Join(fmt.Errorf("recorded native anchor incarnation changed"), err, root.Close())
	}
	result, err := nativeRootSelectionAt(ctx, selected, physical, root, budget)
	if err != nil {
		return NativeDocumentRoot{}, errors.Join(err, root.Close())
	}
	return result, nil
}
