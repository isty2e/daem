package namespace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// Presence is relevant only to a fresh choice. Recorded selections bypass this
// chooser rather than turning a current choice into frozen evidence.
func selectEndpointAlternative(ctx context.Context, candidates [2]string, budget rootedpath.PhysicalTraversalBudget) (string, configuration.PointAlternative, error) {
	if ctx == nil || budget == nil {
		return "", "", fmt.Errorf("endpoint selection requires context and original allocation")
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	for _, candidate := range candidates {
		if err := validateEndpointExpression(candidate); err != nil {
			return "", "", err
		}
	}
	if candidates[0] == candidates[1] {
		return candidates[0], configuration.PointPreferred, nil
	}
	present, err := endpointReferentPresent(ctx, candidates[0], budget)
	if err != nil {
		return "", "", err
	}
	if present {
		return candidates[0], configuration.PointPreferred, nil
	}
	return candidates[1], configuration.PointFallback, nil
}

type endpointSelectionBasis uint8

const (
	endpointFromChoice endpointSelectionBasis = iota + 1
	endpointFromRecord
)

type retainedEndpoint struct {
	basis       endpointSelectionBasis
	candidates  [2]string
	alternative configuration.PointAlternative
	selected    string
	physical    string
	witness     *rootedpath.CapturedRoot
}

// CaptureEndpointRoot retains a witness for one source-selected file point.
// Its parent descriptor is not an independent containing directory grant.
func CaptureEndpointRoot(ctx context.Context, candidates [2]string, budget rootedpath.PhysicalTraversalBudget) (RootSelection, error) {
	selected, alternative, err := selectEndpointAlternative(ctx, candidates, budget)
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
	return retainEndpointRoot(ctx, selection, retainedEndpoint{
		basis: endpointFromChoice, candidates: candidates, alternative: alternative,
		selected: selected, physical: physical,
	}, literalParent)
}

// CaptureRecordedEndpointRoot accepts an independently resolved recorded
// alternative. It never chooses a different alternative from current presence.
func CaptureRecordedEndpointRoot(ctx context.Context, selected string, alternative configuration.PointAlternative, expected rootedpath.AuthorityProvenance, budget rootedpath.PhysicalTraversalBudget) (RootSelection, error) {
	if ctx == nil || budget == nil {
		return RootSelection{}, fmt.Errorf("recorded endpoint selection requires context and original allocation")
	}
	if err := ctx.Err(); err != nil {
		return RootSelection{}, err
	}
	if err := alternative.Validate(); err != nil {
		return RootSelection{}, err
	}
	if err := expected.Validate(); err != nil {
		return RootSelection{}, err
	}
	if err := validateEndpointExpression(selected); err != nil {
		return RootSelection{}, err
	}
	physical, literalParent, err := resolveSelectedEndpoint(ctx, selected, budget)
	if canceled := ctx.Err(); canceled != nil {
		return RootSelection{}, canceled
	}
	if err != nil {
		return RootSelection{}, err
	}
	selection, err := CaptureRecordedRoot(ctx, filepath.Dir(physical), expected, budget)
	if err != nil {
		return RootSelection{}, err
	}
	return retainEndpointRoot(ctx, selection, retainedEndpoint{
		basis: endpointFromRecord, alternative: alternative, selected: selected, physical: physical,
	}, literalParent)
}

func retainEndpointRoot(ctx context.Context, parent RootSelection, endpoint retainedEndpoint, literalParent string) (RootSelection, error) {
	if parent.creation != nil && literalParent != parent.SelectedPath {
		return RootSelection{}, errors.Join(ErrNoTrustedWriterRoot, parent.Close())
	}
	endpoint.witness = parent.Root
	parent.endpoint, parent.Root, parent.literal = &endpoint, nil, ""
	if err := errors.Join(parent.validate(), ctx.Err()); err != nil {
		return RootSelection{}, errors.Join(err, parent.Close())
	}
	return parent, nil
}

func validateEndpointExpression(selected string) error {
	if !filepath.IsAbs(selected) || strings.ContainsRune(selected, '\x00') {
		return fmt.Errorf("endpoint candidate requires an absolute expression without a NUL byte")
	}
	return nil
}

func (endpoint *retainedEndpoint) validate() error {
	if endpoint == nil || endpoint.witness == nil {
		return fmt.Errorf("endpoint selection requires its retained witness")
	}
	if err := endpoint.alternative.Validate(); err != nil {
		return err
	}
	if err := validateEndpointExpression(endpoint.selected); err != nil {
		return err
	}
	if err := validateEndpointExpression(endpoint.physical); err != nil {
		return err
	}
	switch endpoint.basis {
	case endpointFromChoice:
		for _, candidate := range endpoint.candidates {
			if err := validateEndpointExpression(candidate); err != nil {
				return err
			}
		}
		selected := endpoint.candidates[0]
		if endpoint.alternative == configuration.PointFallback {
			if endpoint.candidates[0] == endpoint.candidates[1] {
				return fmt.Errorf("identical endpoint candidates have only the preferred role")
			}
			selected = endpoint.candidates[1]
		}
		if selected != endpoint.selected {
			return fmt.Errorf("endpoint alternative differs from its selected candidate")
		}
	case endpointFromRecord:
		if endpoint.candidates != ([2]string{}) {
			return fmt.Errorf("recorded endpoint cannot retain a current chooser")
		}
	default:
		return fmt.Errorf("endpoint selection basis is required")
	}
	return nil
}

func (endpoint *retainedEndpoint) qualify(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) error {
	if ctx == nil || budget == nil {
		return fmt.Errorf("endpoint qualification requires context and original allocation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := endpoint.validate(); err != nil {
		return err
	}
	if endpoint.basis == endpointFromChoice {
		selected, alternative, err := selectEndpointAlternative(ctx, endpoint.candidates, budget)
		if err != nil {
			return err
		}
		if selected != endpoint.selected || alternative != endpoint.alternative {
			return fmt.Errorf("native endpoint selection changed")
		}
	}
	physical, _, err := resolveSelectedEndpoint(ctx, endpoint.selected, budget)
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
