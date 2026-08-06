package journal

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type removalRelationKey struct {
	scope       target.Scope
	destination output.Destination
}

type capturedRemovalCandidate struct {
	action   pathMutation
	before   recovery.BeforePathState
	expected recovery.ExpectedPathState
}

func aggregateWholeDocumentBeforeState(
	action pathMutation,
	state recovery.BeforePathState,
) recovery.BeforePathState {
	state.BackupPath = ""
	if !state.Existed {
		return state
	}
	state.Kind = recovery.PathKindFile
	state.ContentHash = string(action.LivePathHash)
	state.PathMode = recovery.NewPermissionMode(action.LivePathMode)
	return state
}

func aggregateWholeDocumentExpectedState(
	action pathMutation,
	state recovery.ExpectedPathState,
) recovery.ExpectedPathState {
	if !state.Existed {
		return state
	}
	state.Kind = recovery.PathKindFile
	state.ContentHash = string(action.ExpectedPathHash)
	state.PathMode = recovery.NewPermissionMode(action.ExpectedPathMode)
	return state
}

func captureRemovalIntents(
	ctx context.Context,
	demands recovery.RemovalDemandSet,
	resolver func(output.Destination) (string, error),
) ([]recovery.RemovalIntent, error) {
	if resolver == nil {
		return nil, fmt.Errorf("removal intent resolver is required")
	}

	usedNames := make(map[string]struct{}, demands.Len())
	result := make([]recovery.RemovalIntent, 0, demands.Len())
	for _, demand := range demands.Demands() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved, err := resolver(demand.Destination())
		if err != nil {
			return nil, fmt.Errorf("resolve removal intent destination %q: %w", demand.Destination(), err)
		}
		namespace, err := captureRemovalNamespace(ctx, resolved, usedNames)
		if err != nil {
			return nil, fmt.Errorf("capture removal intent namespace %q: %w", demand.Destination(), err)
		}
		intent, err := recovery.NewRemovalIntent(demand, namespace)
		if err != nil {
			return nil, fmt.Errorf("build removal intent %q: %w", demand.Destination(), err)
		}
		result = append(result, intent)
	}
	return result, nil
}

func captureRemovalNamespace(
	ctx context.Context,
	resolved string,
	usedNames map[string]struct{},
) (recovery.RemovalNamespaceAuthority, error) {
	if err := ctx.Err(); err != nil {
		return recovery.RemovalNamespaceAuthority{}, err
	}
	absolute, err := filepath.Abs(filepath.Clean(resolved))
	if err != nil {
		return recovery.RemovalNamespaceAuthority{}, err
	}
	root, destination, err := rootedpath.CaptureDestination(absolute)
	if err != nil {
		return recovery.RemovalNamespaceAuthority{}, err
	}
	authority, authorityErr := root.Authority()
	if authorityErr != nil {
		_ = root.Close()
		return recovery.RemovalNamespaceAuthority{}, authorityErr
	}
	rootProvenance, provenanceErr := authority.Provenance()
	closeErr := root.Close()
	if provenanceErr != nil || closeErr != nil {
		return recovery.RemovalNamespaceAuthority{}, errors.Join(provenanceErr, closeErr)
	}
	retainedAncestor, err := recovery.NewManifestRootProvenance(
		rootProvenance.PhysicalRoot(),
		rootProvenance.ObjectFingerprint(),
		rootProvenance.MountFingerprint(),
	)
	if err != nil {
		return recovery.RemovalNamespaceAuthority{}, err
	}
	relativeParent := filepath.ToSlash(filepath.Dir(destination.Relative().Path()))
	parentPath := filepath.Join(authority.PhysicalRoot(), filepath.FromSlash(relativeParent))
	parentInfo, parentErr := os.Lstat(parentPath)
	if parentErr == nil {
		if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
			return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("removal parent %q is not a directory", parentPath)
		}
		parentRoot, err := rootedpath.CaptureRootNoFollow(parentPath)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("capture removal parent %q: %w", parentPath, err)
		}
		parentAuthority, authorityErr := parentRoot.Authority()
		if authorityErr != nil {
			_ = parentRoot.Close()
			return recovery.RemovalNamespaceAuthority{}, authorityErr
		}
		parentProvenance, provenanceErr := parentAuthority.Provenance()
		closeErr := parentRoot.Close()
		if provenanceErr != nil || closeErr != nil {
			return recovery.RemovalNamespaceAuthority{}, errors.Join(provenanceErr, closeErr)
		}
		persistedParent, err := recovery.NewManifestRootProvenance(
			parentProvenance.PhysicalRoot(),
			parentProvenance.ObjectFingerprint(),
			parentProvenance.MountFingerprint(),
		)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, err
		}
		retainedPath := filepath.Dir(parentPath)
		retainedRoot, err := rootedpath.CaptureRootNoFollow(retainedPath)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("capture retained removal ancestor %q: %w", retainedPath, err)
		}
		retainedAuthority, authorityErr := retainedRoot.Authority()
		if authorityErr != nil {
			_ = retainedRoot.Close()
			return recovery.RemovalNamespaceAuthority{}, authorityErr
		}
		retainedProvenance, provenanceErr := retainedAuthority.Provenance()
		closeErr = retainedRoot.Close()
		if provenanceErr != nil || closeErr != nil {
			return recovery.RemovalNamespaceAuthority{}, errors.Join(provenanceErr, closeErr)
		}
		persistedRetained, err := recovery.NewManifestRootProvenance(
			retainedProvenance.PhysicalRoot(),
			retainedProvenance.ObjectFingerprint(),
			retainedProvenance.MountFingerprint(),
		)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, err
		}
		missingSuffix, err := filepath.Rel(retainedPath, parentPath)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("derive removal parent suffix: %w", err)
		}
		missingSuffix = filepath.ToSlash(missingSuffix)
		names, err := allocateLogicalRemovalNames(parentProvenance.PhysicalRoot(), usedNames)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, err
		}
		return recovery.NewExistingParentAuthority(persistedParent, persistedRetained, missingSuffix, names)
	}
	if !os.IsNotExist(parentErr) {
		return recovery.RemovalNamespaceAuthority{}, fmt.Errorf("inspect removal parent %q: %w", parentPath, parentErr)
	}
	names, err := allocateLogicalRemovalNames("", usedNames)
	if err != nil {
		return recovery.RemovalNamespaceAuthority{}, err
	}
	return recovery.NewInitiallyAbsentParentAuthority(retainedAncestor, relativeParent, names)
}

func allocateLogicalRemovalNames(
	parent string,
	usedNames map[string]struct{},
) (mutationfs.LogicalRemovalNames, error) {
	for range 64 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return mutationfs.LogicalRemovalNames{}, fmt.Errorf("generate logical removal names: %w", err)
		}
		token := fmt.Sprintf("%x", random[:])
		candidate, err := mutationfs.NewLogicalRemovalNames(
			".daem-tombstone-"+token,
			".daem-cleanup-"+token,
		)
		if err != nil {
			return mutationfs.LogicalRemovalNames{}, err
		}
		if _, used := usedNames[candidate.Residue()]; used {
			continue
		}
		if _, used := usedNames[candidate.Cleanup()]; used {
			continue
		}
		if parent != "" {
			occupied := false
			for _, name := range []string{candidate.Residue(), candidate.Cleanup()} {
				_, statErr := os.Lstat(filepath.Join(parent, name))
				switch {
				case statErr == nil:
					occupied = true
				case !os.IsNotExist(statErr):
					return mutationfs.LogicalRemovalNames{}, fmt.Errorf("check logical removal name %q: %w", name, statErr)
				}
			}
			if occupied {
				continue
			}
		}
		usedNames[candidate.Residue()] = struct{}{}
		usedNames[candidate.Cleanup()] = struct{}{}
		return candidate, nil
	}
	return mutationfs.LogicalRemovalNames{}, fmt.Errorf("could not allocate unique logical removal names")
}
