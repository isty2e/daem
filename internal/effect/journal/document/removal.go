package document

import (
	"context"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

type removalRelationKey struct {
	scope       target.Scope
	destination output.Destination
}

func RemovalStates(before aggregate.Document, beforeMode *recovery.PermissionMode, expected aggregate.Document, expectedMode *recovery.PermissionMode) ([]recovery.RemovalState, error) {
	if before.Exists() == expected.Exists() {
		return nil, nil
	}
	if before.Exists() {
		if beforeMode == nil {
			return nil, fmt.Errorf("document removal before mode is required")
		}
		state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{Existed: true, PathExisted: true, ParentExisted: true, Kind: recovery.PathKindFile, PathMode: beforeMode, ContentHash: string(artifact.HashFileContentWithExecutable(before.Content(), os.FileMode(*beforeMode)&0o111 != 0))})
		return []recovery.RemovalState{state}, err
	}
	if expectedMode == nil {
		return nil, fmt.Errorf("document removal expected mode is required")
	}
	state, err := recovery.NewExpectedRemovalState(recovery.ExpectedPathState{Existed: true, PathExisted: true, Kind: recovery.PathKindFile, PathMode: expectedMode, ContentHash: string(artifact.HashFileContentWithExecutable(expected.Content(), os.FileMode(*expectedMode)&0o111 != 0))})
	return []recovery.RemovalState{state}, err
}

func (comparison Comparison) RemovalDemands(ctx context.Context, relations []aggregate.DocumentAddress) (recovery.RemovalDemandSet, error) {
	if ctx == nil {
		return recovery.RemovalDemandSet{}, fmt.Errorf("document removal context is required")
	}
	if err := ctx.Err(); err != nil {
		return recovery.RemovalDemandSet{}, err
	}
	if err := comparison.Validate(); err != nil {
		return recovery.RemovalDemandSet{}, err
	}
	var expectedMode *recovery.PermissionMode
	if mode, present := comparison.ExpectedState().RegularFileMode(); present {
		expectedMode = &mode
	}
	states, err := RemovalStates(comparison.BeforeDocument(), comparison.BeforeMode(), comparison.ExpectedDocument(), expectedMode)
	if err != nil || len(states) == 0 {
		return recovery.RemovalDemandSet{}, err
	}
	seen := make(map[removalRelationKey]struct{})
	demands := make([]recovery.RemovalDemand, 0, len(relations))
	for _, relation := range relations {
		if err := ctx.Err(); err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		key := removalRelationKey{scope: relation.Scope(), destination: relation.AggregateRoot()}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		demand, err := recovery.NewRemovalDemand(key.scope, key.destination, states)
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		demands = append(demands, demand)
	}
	if len(demands) == 0 {
		return recovery.RemovalDemandSet{}, fmt.Errorf("document root transition has no actual logical writer")
	}
	return recovery.NewRemovalDemandSet(demands)
}
