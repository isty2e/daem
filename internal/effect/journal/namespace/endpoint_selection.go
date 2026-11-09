package namespace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

// SelectEndpointBounded observes the preferred referent before selecting either
// expression. The result is a current location fact, not filesystem authority.
func SelectEndpointBounded(ctx context.Context, candidates [2]string, budget rootedpath.PhysicalTraversalBudget) (string, error) {
	if ctx == nil || budget == nil {
		return "", fmt.Errorf("endpoint selection requires context and original allocation")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for _, candidate := range candidates {
		if !filepath.IsAbs(candidate) || strings.ContainsRune(candidate, '\x00') {
			return "", fmt.Errorf("endpoint candidate requires an absolute expression without a NUL byte")
		}
	}
	if candidates[0] == candidates[1] {
		return candidates[0], nil
	}
	present, err := endpointReferentPresent(ctx, candidates[0], budget)
	if err != nil {
		return "", err
	}
	if present {
		return candidates[0], nil
	}
	return candidates[1], nil
}

type retainedEndpoint struct {
	candidates [2]string
	selected   string
	physical   string
	witness    *rootedpath.CapturedRoot
}

// CaptureEndpointRoot retains a witness for one source-selected file point.
// Its parent descriptor is not an independent containing directory grant.
func CaptureEndpointRoot(ctx context.Context, candidates [2]string, budget rootedpath.PhysicalTraversalBudget) (RootSelection, error) {
	selected, err := SelectEndpointBounded(ctx, candidates, budget)
	if err != nil {
		return RootSelection{}, err
	}
	physical, literalParent, err := resolveSelectedEndpoint(ctx, selected, budget)
	if canceled := ctx.Err(); canceled != nil {
		return RootSelection{}, canceled
	}
	if err != nil {
		return RootSelection{}, err
	}
	selection, err := CaptureRoot(ctx, filepath.Dir(physical), budget)
	if err != nil {
		return RootSelection{}, err
	}
	if selection.creation != nil {
		if literalParent != selection.SelectedPath {
			return RootSelection{}, errors.Join(ErrNoTrustedWriterRoot, selection.Root.Close())
		}
	}
	selection.endpoint = &retainedEndpoint{candidates: candidates, selected: selected, physical: physical, witness: selection.Root}
	selection.Root = nil
	if err := ctx.Err(); err != nil {
		return RootSelection{}, errors.Join(err, selection.Close())
	}
	return selection, nil
}

func (endpoint *retainedEndpoint) qualify(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) error {
	selected, err := SelectEndpointBounded(ctx, endpoint.candidates, budget)
	if err != nil {
		return err
	}
	if selected != endpoint.selected {
		return fmt.Errorf("native endpoint selection changed")
	}
	physical, _, err := resolveSelectedEndpoint(ctx, selected, budget)
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if err != nil || physical != endpoint.physical {
		return errors.Join(fmt.Errorf("native endpoint referent changed"), err)
	}
	return nil
}

func resolveSelectedEndpoint(ctx context.Context, selected string, budget rootedpath.PhysicalTraversalBudget) (string, string, error) {
	route, parent, err := endpointRoutePath(ctx, selected, budget)
	if err != nil {
		return "", "", err
	}
	physical, err := rootedpath.ResolveReferentPathBounded(route, physicalwork.MaximumPhysicalPathDepth, budget)
	if canceled := ctx.Err(); canceled != nil {
		return "", "", canceled
	}
	return physical, parent, err
}

func endpointRoutePath(ctx context.Context, selected string, budget rootedpath.PhysicalTraversalBudget) (string, string, error) {
	literalParent, leaf := filepath.Split(selected)
	if leaf == "" {
		return "", "", fmt.Errorf("endpoint selector requires a file name")
	}
	parent, err := rootedpath.ResolveRootSelectorBounded(literalParent, physicalwork.MaximumPhysicalPathDepth, budget)
	if canceled := ctx.Err(); canceled != nil {
		return "", "", canceled
	}
	if err != nil {
		return "", "", err
	}
	return filepath.Join(parent, leaf), parent, nil
}

func endpointReferentPresent(ctx context.Context, expression string, budget rootedpath.PhysicalTraversalBudget) (present bool, err error) {
	physical, err := rootedpath.ResolveFutureReferentPathBounded(expression, physicalwork.MaximumPhysicalPathDepth, budget)
	if canceled := ctx.Err(); canceled != nil {
		return false, canceled
	}
	if err != nil {
		return false, err
	}
	parent, err := CaptureRootSelector(ctx, filepath.Dir(physical), budget)
	if err != nil {
		return false, err
	}
	defer func() {
		err = errors.Join(err, parent.Root.Close(), ctx.Err())
		if err != nil {
			present = false
		}
	}()
	if parent.creation != nil {
		return false, nil
	}
	return parent.Root.ChildExistsNoFollow(ctx, filepath.Base(physical), budget)
}
