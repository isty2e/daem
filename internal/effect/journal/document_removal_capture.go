package journal

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func documentRemovalStates(before aggregate.Document, beforeMode *recovery.PermissionMode, expected aggregate.Document, expectedMode *recovery.PermissionMode) ([]recovery.RemovalState, error) {
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

func (comparison DocumentComparison) removalDemands(ctx context.Context, relations []aggregate.DocumentAddress) (recovery.RemovalDemandSet, error) {
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
	if mode, present := comparison.expectedState.RegularFileMode(); present {
		expectedMode = &mode
	}
	states, err := documentRemovalStates(comparison.before, comparison.beforeMode(), comparison.expected, expectedMode)
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

func captureDocumentRemoval(ctx context.Context, namespace NativeDocumentNamespace, comparison DocumentComparison, relations []aggregate.DocumentAddress, budget *recovery.PhysicalWorkBudget) (*recoveryDocumentRemoval, error) {
	demands, err := comparison.removalDemands(ctx, relations)
	if err != nil || demands.Len() == 0 {
		return nil, err
	}
	aliases, err := documentAliasSet(ctx, namespace.Addresses())
	if err != nil {
		return nil, err
	}
	for _, relation := range relations {
		if _, present := aliases[relation]; !present {
			return nil, fmt.Errorf("actual removal writer has no qualified document alias")
		}
	}
	if err := budget.AdmitObservation(); err != nil {
		return nil, err
	}
	writer, err := namespace.writer.LexicalPath()
	if err != nil {
		return nil, err
	}
	cleanup, physical, err := captureRemovalNamespace(ctx, writer, make(map[string]struct{}), budget)
	if err != nil {
		return nil, err
	}
	if physical != writer {
		return nil, fmt.Errorf("document removal namespace changed its physical writer")
	}
	capability, err := namespace.AcquireReferent(ctx, budget)
	if err != nil {
		return nil, err
	}
	if err := capability.Close(); err != nil {
		return nil, err
	}
	observed, err := ObserveRemovalNamespace(ctx, namespace.writerRoot, namespace.writer, cleanup, budget)
	if err != nil || observed.Status() != recovery.RemovalNamespaceMatched {
		return nil, errors.Join(err, fmt.Errorf("document removal namespace no longer matches the writer"))
	}
	if err := reserveRemovalIntentNamespacePathCapacity(budget, writer, cleanup); err != nil {
		return nil, err
	}
	removal, err := recovery.NewDocumentRemoval(demands, cleanup)
	if err != nil {
		return nil, err
	}
	record, err := persistedDocumentRemoval(removal)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &record, nil
}

func (record recoveryDocumentGroup) validateRemovalImages(images loadedDocumentImages) error {
	if err := record.validateRemoval(); err != nil || record.Removal == nil {
		return err
	}
	states, err := documentRemovalStates(images.original.document, images.original.mode, images.expected.document, images.expected.mode)
	if err != nil {
		return err
	}
	removal, err := record.Removal.canonical()
	if err != nil {
		return err
	}
	for _, demand := range removal.Demands().Demands() {
		actual := demand.States()
		if len(actual) != len(states) {
			return fmt.Errorf("document removal states do not match its complete root transition")
		}
		for index, state := range states {
			if !state.Equal(actual[index]) {
				return fmt.Errorf("document removal states do not match actual image bytes and modes")
			}
		}
	}
	return nil
}
