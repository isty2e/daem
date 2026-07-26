package aggregate

import (
	"fmt"

	"github.com/isty2e/daem/internal/target"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

type HookPlacementID string

const (
	HookPlacementCodexProject  HookPlacementID = topologyhook.CodexProjectProjectionNamespace
	HookPlacementCodexGlobal   HookPlacementID = topologyhook.CodexGlobalProjectionNamespace
	HookPlacementClaudeProject HookPlacementID = topologyhook.ClaudeProjectProjectionNamespace
	HookPlacementClaudeGlobal  HookPlacementID = topologyhook.ClaudeGlobalProjectionNamespace

	HookCodecCodexProject  CodecContractID = "codex-project-hook-json-v1"
	HookCodecCodexGlobal   CodecContractID = "codex-global-hook-json-v1"
	HookCodecClaudeProject CodecContractID = "claude-project-hook-json-v1"
	HookCodecClaudeGlobal  CodecContractID = "claude-global-hook-json-v1"

	HookMergeUnit    MergeUnit = "hook-set"
	HooksContentPath           = "/hooks"
)

var hookComparedFields = []string{"event", "handler", "matcher"}

// HookPlacement is one static native command-hook aggregate row.
type HookPlacement struct {
	id              HookPlacementID
	target          target.Target
	scope           target.Scope
	aggregateRoot   string
	codecContractID CodecContractID
}

var implementedHookPlacements = []HookPlacement{
	{
		id: HookPlacementCodexProject, target: target.TargetCodex, scope: target.ScopeProject,
		aggregateRoot: ".codex/hooks.json", codecContractID: HookCodecCodexProject,
	},
	{
		id: HookPlacementCodexGlobal, target: target.TargetCodex, scope: target.ScopeGlobal,
		aggregateRoot: "~/.codex/hooks.json", codecContractID: HookCodecCodexGlobal,
	},
	{
		id: HookPlacementClaudeProject, target: target.TargetClaudeCode, scope: target.ScopeProject,
		aggregateRoot: ".claude/settings.json", codecContractID: HookCodecClaudeProject,
	},
	{
		id: HookPlacementClaudeGlobal, target: target.TargetClaudeCode, scope: target.ScopeGlobal,
		aggregateRoot: "~/.claude/settings.json", codecContractID: HookCodecClaudeGlobal,
	},
}

func init() {
	if err := validateHookPlacements(implementedHookPlacements); err != nil {
		panic(err)
	}
}

// ImplementedHookPlacements returns implemented Hook placements in stable order.
func ImplementedHookPlacements() []HookPlacement {
	return append([]HookPlacement(nil), implementedHookPlacements...)
}

// HookPlacementFor returns the exact native Hook row for target and scope.
func HookPlacementFor(selectedTarget target.Target, scope target.Scope) (HookPlacement, bool) {
	for _, placement := range implementedHookPlacements {
		if placement.target == selectedTarget && placement.scope == scope {
			return placement, true
		}
	}
	return HookPlacement{}, false
}

// HookPlacementForCodec returns the row owning one Hook codec contract.
func HookPlacementForCodec(contractID CodecContractID) (HookPlacement, bool) {
	for _, placement := range implementedHookPlacements {
		if placement.codecContractID == contractID {
			return placement, true
		}
	}
	return HookPlacement{}, false
}

// Contribution constructs one shared-set Hook realization body.
func (placement HookPlacement) Contribution(canonical string) (ManagedContribution, error) {
	if err := placement.Validate(); err != nil {
		return ManagedContribution{}, err
	}
	return NewManagedContribution(ManagedContributionInput{
		PlacementID:           string(placement.id),
		Target:                placement.target,
		Scope:                 placement.scope,
		AggregateRoot:         placement.aggregateRoot,
		ContentPath:           HooksContentPath,
		MergeUnit:             HookMergeUnit,
		Cardinality:           ContributionSharedSet,
		SiblingRetention:      PreserveUnmanagedSiblings,
		SiblingPreservation:   PreserveSiblingsSemantic,
		Equivalence:           EquivalenceCanonicalSemantic,
		CanonicalContribution: canonical,
		CodecContractID:       placement.codecContractID,
		ComparedFields:        hookComparedFields,
	})
}

// Validate rejects a row that differs from the closed catalog.
func (placement HookPlacement) Validate() error {
	if _, err := target.ParseTarget(string(placement.target)); err != nil {
		return err
	}
	if _, err := target.ParseScope(string(placement.scope)); err != nil {
		return err
	}
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "Hook placement id", value: string(placement.id)},
		{label: "Hook codec contract id", value: string(placement.codecContractID)},
	} {
		if err := validateToken(field.label, field.value); err != nil {
			return err
		}
	}
	if _, err := newProjectionAddress(
		string(placement.id), placement.target, placement.scope, placement.aggregateRoot,
		HookMergeUnit, HooksContentPath,
	); err != nil {
		return err
	}
	return nil
}

func validateHookPlacements(placements []HookPlacement) error {
	ids := make(map[HookPlacementID]struct{}, len(placements))
	coordinates := make(map[string]struct{}, len(placements))
	codecs := make(map[CodecContractID]struct{}, len(placements))
	for _, placement := range placements {
		if err := placement.Validate(); err != nil {
			return err
		}
		coordinate := string(placement.target) + "\x00" + string(placement.scope)
		if _, duplicate := ids[placement.id]; duplicate {
			return fmt.Errorf("Hook placements repeat id %q", placement.id)
		}
		if _, duplicate := coordinates[coordinate]; duplicate {
			return fmt.Errorf("Hook placements repeat target/scope %q", coordinate)
		}
		if _, duplicate := codecs[placement.codecContractID]; duplicate {
			return fmt.Errorf("Hook placements repeat codec %q", placement.codecContractID)
		}
		ids[placement.id] = struct{}{}
		coordinates[coordinate] = struct{}{}
		codecs[placement.codecContractID] = struct{}{}
	}
	return nil
}

func (placement HookPlacement) ID() HookPlacementID              { return placement.id }
func (placement HookPlacement) Target() target.Target            { return placement.target }
func (placement HookPlacement) Scope() target.Scope              { return placement.scope }
func (placement HookPlacement) AggregateRoot() string            { return placement.aggregateRoot }
func (placement HookPlacement) CodecContractID() CodecContractID { return placement.codecContractID }
