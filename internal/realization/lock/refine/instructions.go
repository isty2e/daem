package refine

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type instructionPlacement struct {
	placement profile.ManagedPathPlacement
	mode      realization.PathProjectionMode
}

// InstructionsPathProjections refines one Instructions value into admitted file placements.
func InstructionsPathProjections(value instructions.Instructions) ([]lock.LockedSubjectContract, error) {
	placements := make(map[string]instructionPlacement)
	placementByAddress := make(map[string]string)
	renderings := value.Renderings()
	for _, selectedTarget := range value.Targets() {
		rendering, ok := renderings[selectedTarget]
		if !ok {
			var err error
			rendering, err = instructions.NewRendering("", instructions.RenderModeCopy)
			if err != nil {
				return nil, err
			}
		}
		destination, err := instructionDestination(selectedTarget, value.Scope(), rendering.RenderTo())
		if err != nil {
			return nil, fmt.Errorf("instructions %q target %q: %w", value.ID().Name(), selectedTarget, err)
		}
		placement, err := profile.ManagedFilePlacementFor(
			entity.KindInstructions,
			selectedTarget,
			value.Scope(),
			destination,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"instructions %q target %q: render_to %q for target %q scope %q resolves to %q, which is not an admitted instruction placement destination: %w",
				value.ID().Name(),
				selectedTarget,
				rendering.RenderTo(),
				selectedTarget,
				value.Scope(),
				destination,
				err,
			)
		}
		mode, err := instructionProjectionMode(rendering.Mode())
		if err != nil {
			return nil, fmt.Errorf("instructions %q target %q: %w", value.ID().Name(), selectedTarget, err)
		}

		address := string(value.Scope()) + "\x00" + destination
		if existingID, occupied := placementByAddress[address]; occupied && existingID != placement.ID() {
			return nil, fmt.Errorf(
				"instructions %q placement ids %q and %q claim destination %q",
				value.ID().Name(),
				existingID,
				placement.ID(),
				destination,
			)
		}
		placementByAddress[address] = placement.ID()

		existing, shared := placements[placement.ID()]
		if !shared {
			placements[placement.ID()] = instructionPlacement{placement: placement, mode: mode}
			continue
		}
		if existing.mode != mode {
			return nil, fmt.Errorf(
				"instructions %q placement %q has conflicting render modes %q and %q",
				value.ID().Name(),
				placement.ID(),
				existing.mode,
				mode,
			)
		}
		merged, err := profile.MergeManagedPathPlacements(existing.placement, placement)
		if err != nil {
			return nil, fmt.Errorf("instructions %q placement %q: %w", value.ID().Name(), placement.ID(), err)
		}
		existing.placement = merged
		placements[placement.ID()] = existing
	}

	ids := make([]string, 0, len(placements))
	for id := range placements {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	contracts := make([]lock.LockedSubjectContract, 0, len(ids))
	for _, id := range ids {
		selected := placements[id]
		writeRoute, err := profile.ManagedPathOperationRoute(selected.placement, profile.OperationWrite)
		if err != nil {
			return nil, fmt.Errorf("select instructions %q placement %q write route: %w", value.ID().Name(), id, err)
		}
		removeRoute, err := profile.ManagedPathOperationRoute(selected.placement, profile.OperationRemove)
		if err != nil {
			return nil, fmt.Errorf("select instructions %q placement %q remove route: %w", value.ID().Name(), id, err)
		}
		spec, err := selected.placement.Realize(selected.placement.Root(), selected.mode, writeRoute)
		if err != nil {
			return nil, fmt.Errorf("realize instructions %q placement %q: %w", value.ID().Name(), id, err)
		}
		subjectID, err := topologyprojection.Subject(value.ID(), id)
		if err != nil {
			return nil, fmt.Errorf("lower instructions %q projection topology: %w", value.ID().Name(), err)
		}
		contract, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
			EntityID:      value.ID(),
			SubjectID:     subjectID,
			Realization:   spec,
			WriteRouteID:  writeRoute.RouteID(),
			RemoveRouteID: removeRoute.RouteID(),
		})
		if err != nil {
			return nil, fmt.Errorf("lock instructions %q projection %q: %w", value.ID().Name(), id, err)
		}
		contracts = append(contracts, contract)
	}
	return contracts, nil
}

func instructionDestination(selectedTarget target.Target, scope target.Scope, renderTo string) (string, error) {
	defaultPlacement, err := profile.Profile(selectedTarget).DefaultPlacement(entity.KindInstructions, scope)
	if err != nil {
		return "", err
	}
	if renderTo == "" {
		return defaultPlacement.Root(), nil
	}
	clean, err := cleanInstructionRenderPath(renderTo)
	if err != nil {
		return "", err
	}
	if scope == target.ScopeProject {
		return clean, nil
	}
	if scope == target.ScopeGlobal {
		return path.Join(path.Dir(defaultPlacement.Root()), clean), nil
	}
	return "", fmt.Errorf("unknown scope %q", scope)
}

func cleanInstructionRenderPath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("render_to is required")
	}
	if strings.Contains(trimmed, "\\") {
		return "", fmt.Errorf("render_to %q must use slash-separated relative paths", value)
	}
	if strings.HasPrefix(trimmed, "~") || path.IsAbs(trimmed) {
		return "", fmt.Errorf("render_to %q must be relative to the target scope root", value)
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("render_to %q must stay inside the target scope root", value)
	}
	if cleaned != trimmed {
		return "", fmt.Errorf("render_to %q must be a canonical slash-separated relative path", value)
	}
	return cleaned, nil
}

func instructionProjectionMode(mode instructions.RenderMode) (realization.PathProjectionMode, error) {
	switch mode {
	case instructions.RenderModeCopy:
		return realization.PathProjectionCopy, nil
	case instructions.RenderModeSymlink:
		return realization.PathProjectionSymlink, nil
	default:
		return "", fmt.Errorf("unknown render mode %q", mode)
	}
}
