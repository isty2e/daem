package journal

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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

func completeRemovalTransitionStates(
	action pathMutation,
	before recovery.BeforePathState,
	expected recovery.ExpectedPathState,
) (recovery.BeforePathState, recovery.ExpectedPathState) {
	before.BackupPath = ""
	if action.ContentPath == "" {
		return before, expected
	}

	before = recovery.BeforePathState{}
	if action.LivePathExists {
		before = recovery.BeforePathState{
			Existed:       true,
			PathExisted:   true,
			ParentExisted: true,
			Kind:          recovery.PathKindFile,
			ContentHash:   string(action.LivePathHash),
			PathMode:      recovery.NewPermissionMode(action.LivePathMode),
		}
	}
	expected = recovery.ExpectedPathState{}
	if action.ExpectedPathExists {
		expected = recovery.ExpectedPathState{
			Existed:     true,
			PathExisted: true,
			Kind:        recovery.PathKindFile,
			ContentHash: string(action.ExpectedPathHash),
			PathMode:    recovery.NewPermissionMode(action.ExpectedPathMode),
		}
	}
	return before, expected
}

func removalTransitionForCapture(
	scope target.Scope,
	destination output.Destination,
	before recovery.BeforePathState,
	expected recovery.ExpectedPathState,
	demands map[removalRelationKey]recovery.RemovalDemand,
) (*recoveryRemovalTransition, error) {
	demand, found := demands[removalRelationKey{scope: scope, destination: destination}]
	if !found {
		return nil, nil
	}

	beforeState, err := recovery.NewBeforeRemovalState(before)
	if err != nil {
		return nil, err
	}
	expectedState, err := recovery.NewExpectedRemovalState(expected)
	if err != nil {
		return nil, err
	}
	transition := &recoveryRemovalTransition{}
	for _, state := range demand.States() {
		if state.Equal(beforeState) {
			persisted := persistedBeforePathState(before)
			transition.Before = &persisted
		}
		if state.Equal(expectedState) {
			persisted := persistedExpectedPathState(expected)
			transition.ExpectedAfter = &persisted
		}
	}
	if transition.Before == nil && transition.ExpectedAfter == nil {
		return nil, nil
	}
	return transition, nil
}

func captureRemovalIntents(
	ctx context.Context,
	demands recovery.RemovalDemandSet,
	resolver func(output.Destination) (string, error),
) ([]recovery.RemovalIntent, error) {
	if resolver == nil {
		return nil, fmt.Errorf("removal intent resolver is required")
	}

	budget, err := recovery.NewPhysicalWorkBudget(demands.Len())
	if err != nil {
		return nil, err
	}
	type captureRequest struct {
		demand recovery.RemovalDemand
		path   string
	}
	requests := make([]captureRequest, 0, demands.Len())
	for _, demand := range demands.Demands() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved, err := resolver(demand.Destination())
		if err != nil {
			return nil, fmt.Errorf("resolve removal intent destination %q: %w", demand.Destination(), err)
		}
		absolute, err := canonicalRemovalIntentPath(resolved)
		if err != nil {
			return nil, fmt.Errorf("normalize removal intent destination %q: %w", demand.Destination(), err)
		}
		requests = append(requests, captureRequest{demand: demand, path: absolute})
	}

	usedNames := make(map[string]struct{}, demands.Len())
	result := make([]recovery.RemovalIntent, 0, demands.Len())
	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := budget.AdmitObservation(); err != nil {
			return nil, fmt.Errorf("admit removal intent observation %q: %w", request.demand.Destination(), err)
		}
		namespace, physicalPath, err := captureRemovalNamespace(
			ctx,
			request.path,
			usedNames,
			budget,
		)
		if err != nil {
			return nil, fmt.Errorf("capture removal intent namespace %q: %w", request.demand.Destination(), err)
		}
		if err := reserveRemovalIntentNamespacePathCapacity(budget, physicalPath, namespace); err != nil {
			return nil, fmt.Errorf("admit removal intent namespace capacity %q: %w", request.demand.Destination(), err)
		}
		intent, err := recovery.NewRemovalIntent(request.demand, namespace)
		if err != nil {
			return nil, fmt.Errorf("build removal intent %q: %w", request.demand.Destination(), err)
		}
		result = append(result, intent)
	}
	return result, nil
}

func canonicalRemovalIntentPath(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("removal intent path is required")
	}
	return filepath.Abs(filepath.Clean(value))
}

func reserveRemovalIntentNamespacePathCapacity(
	budget *recovery.PhysicalWorkBudget,
	destinationPath string,
	namespace recovery.RemovalNamespaceAuthority,
) error {
	residuePath, cleanupPath, err := RemovalNamespacePaths(namespace)
	if err != nil {
		return err
	}
	for _, slotPath := range []string{residuePath, cleanupPath} {
		if err := budget.AdmitObservation(); err != nil {
			return err
		}
		if err := ChargeRemovalPathWork(budget, slotPath); err != nil {
			return err
		}
	}
	return ReserveRemovalExecutionObservationWork(
		budget,
		destinationPath,
		residuePath,
		cleanupPath,
	)
}

func captureRemovalNamespace(
	ctx context.Context,
	absolute string,
	usedNames map[string]struct{},
	budget *recovery.PhysicalWorkBudget,
) (result recovery.RemovalNamespaceAuthority, physicalPath string, resultErr error) {
	if err := ctx.Err(); err != nil {
		return recovery.RemovalNamespaceAuthority{}, "", err
	}
	if !filepath.IsAbs(absolute) || filepath.Clean(absolute) != absolute {
		return recovery.RemovalNamespaceAuthority{}, "", fmt.Errorf(
			"removal namespace path must be canonical and absolute",
		)
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(
		absolute,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return recovery.RemovalNamespaceAuthority{}, "", err
	}
	defer func() {
		resultErr = errors.Join(resultErr, root.Close())
	}()
	authority := destination.Root()
	rootProvenance, provenanceErr := authority.Provenance()
	var pathErr error
	physicalPath, pathErr = destination.LexicalPath()
	if provenanceErr != nil || pathErr != nil {
		return recovery.RemovalNamespaceAuthority{}, "", errors.Join(provenanceErr, pathErr)
	}
	retainedAncestor, err := recovery.NewRootProvenance(
		rootProvenance.PhysicalRoot(),
		rootProvenance.ObjectFingerprint(),
		rootProvenance.MountFingerprint(),
	)
	if err != nil {
		return recovery.RemovalNamespaceAuthority{}, "", err
	}
	relativeParent := filepath.ToSlash(filepath.Dir(destination.Relative().Path()))
	if relativeParent == "." {
		persistedParent, err := recovery.NewRootProvenance(
			rootProvenance.PhysicalRoot(),
			rootProvenance.ObjectFingerprint(),
			rootProvenance.MountFingerprint(),
		)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, "", err
		}
		names, err := allocateLogicalRemovalNames(
			ctx,
			usedNames,
			func(ctx context.Context, candidate mutationfs.LogicalRemovalNames) (bool, error) {
				names := [2]string{candidate.Residue(), candidate.Cleanup()}
				observed, err := root.ChildrenExistNoFollow(ctx, names, budget)
				if err != nil {
					return false, err
				}
				return observed[0] || observed[1], nil
			},
		)
		if err != nil {
			return recovery.RemovalNamespaceAuthority{}, "", err
		}
		namespace, err := recovery.NewExistingParentAuthority(persistedParent, names)
		return namespace, physicalPath, err
	}
	names, err := allocateLogicalRemovalNames(ctx, usedNames, nil)
	if err != nil {
		return recovery.RemovalNamespaceAuthority{}, "", err
	}
	namespace, err := recovery.NewInitiallyAbsentParentAuthority(retainedAncestor, relativeParent, names)
	return namespace, physicalPath, err
}

func allocateLogicalRemovalNames(
	ctx context.Context,
	usedNames map[string]struct{},
	candidateOccupied func(context.Context, mutationfs.LogicalRemovalNames) (bool, error),
) (mutationfs.LogicalRemovalNames, error) {
	for range 64 {
		if err := ctx.Err(); err != nil {
			return mutationfs.LogicalRemovalNames{}, err
		}
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
		if candidateOccupied != nil {
			occupied, err := candidateOccupied(ctx, candidate)
			if err != nil {
				return mutationfs.LogicalRemovalNames{}, fmt.Errorf("check logical removal name pair: %w", err)
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
