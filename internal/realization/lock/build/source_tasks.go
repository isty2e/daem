package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hookasset"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

type sourceTaskID string

func newSourceTaskID(stage EventStage, ordinal int) sourceTaskID {
	return sourceTaskID(fmt.Sprintf("%s:%06d", stage, ordinal))
}

func (id sourceTaskID) requestID() acquisition.RequestID {
	return acquisition.RequestID(id)
}

type sourceTask struct {
	stage          EventStage
	ordinal        int
	operation      acquisition.Operation
	sourceSpec     source.Source
	entityID       entity.ID
	groupIndex     *int
	startedEvent   EventKind
	succeededEvent EventKind
	failedEvent    EventKind
	errorPrefix    string
}

func newSkillGroupListTask(index int, set skill.SkillSet) sourceTask {
	return sourceTask{
		stage:        EventStageSkillGroupRoot,
		ordinal:      index,
		operation:    acquisition.OperationListRoot,
		sourceSpec:   set.Source(),
		groupIndex:   cloneIntPointer(&index),
		startedEvent: EventSkillGroupListStarted,
		failedEvent:  EventSkillGroupListFailed,
		errorPrefix:  fmt.Sprintf("list skill_group[%d] source", index),
	}
}

func newSkillResolveTask(ordinal int, lockable lockableSkill) sourceTask {
	entityID := lockable.Resource.ID()
	return sourceTask{
		stage:          EventStageSkill,
		ordinal:        ordinal,
		operation:      acquisition.OperationResolve,
		sourceSpec:     lockable.Resource.Source(),
		entityID:       entityID,
		startedEvent:   EventResourceResolveStarted,
		succeededEvent: EventResourceResolved,
		failedEvent:    EventResourceResolveFailed,
		errorPrefix:    fmt.Sprintf("resolve skill %q", entityID.Name()),
	}
}

func newHookAssetResolveTask(ordinal int, asset hookasset.HookAsset) sourceTask {
	entityID := asset.ID()
	return sourceTask{
		stage:          EventStageHookAsset,
		ordinal:        ordinal,
		operation:      acquisition.OperationResolve,
		sourceSpec:     asset.Source(),
		entityID:       entityID,
		startedEvent:   EventResourceResolveStarted,
		succeededEvent: EventResourceResolved,
		failedEvent:    EventResourceResolveFailed,
		errorPrefix:    fmt.Sprintf("resolve hook_asset %q source", entityID.Name()),
	}
}

func newInstructionsResolveTask(ordinal int, instruction instructions.Instructions) sourceTask {
	entityID := instruction.ID()
	return sourceTask{
		stage:          EventStageInstructions,
		ordinal:        ordinal,
		operation:      acquisition.OperationResolve,
		sourceSpec:     instruction.Source(),
		entityID:       entityID,
		startedEvent:   EventResourceResolveStarted,
		succeededEvent: EventResourceResolved,
		failedEvent:    EventResourceResolveFailed,
		errorPrefix:    fmt.Sprintf("resolve instructions %q source", entityID.Name()),
	}
}

func (task sourceTask) id() sourceTaskID {
	return newSourceTaskID(task.stage, task.ordinal)
}

func (task sourceTask) request() (acquisition.Request, error) {
	return acquisition.NewRequest(task.id().requestID(), task.ordinal, task.operation, task.sourceSpec)
}

func (task sourceTask) sourceID() artifact.SourceID {
	sourceID, err := source.SourceIDFor(task.sourceSpec)
	if err != nil {
		return ""
	}

	return sourceID
}

func (task sourceTask) skillGroupIndex() *int {
	return cloneIntPointer(task.groupIndex)
}

func (task sourceTask) event(kind EventKind, err error) Event {
	return Event{
		Kind:            kind,
		TaskID:          task.id().requestID(),
		Stage:           task.stage,
		Ordinal:         task.ordinal,
		EntityID:        task.entityID,
		SourceID:        task.sourceID(),
		SkillGroupIndex: task.skillGroupIndex(),
		Err:             err,
	}
}

func (task sourceTask) resolveStartedEventKind() EventKind {
	return task.startedEvent
}

func (task sourceTask) resolveSucceededEventKind() EventKind {
	return task.succeededEvent
}

func (task sourceTask) resolveFailedEventKind() EventKind {
	return task.failedEvent
}

func (task sourceTask) wrapSourceError(err error) error {
	if err == nil {
		return nil
	}

	if task.errorPrefix == "" {
		return fmt.Errorf("resolve source task %q: %w", task.id(), err)
	}
	return fmt.Errorf("%s: %w", task.errorPrefix, err)
}

type sourceTaskResult struct {
	task       sourceTask
	resolution acquisition.Resolution
	listing    source.RootListing
	err        error
}

func sourceTaskResults(
	ctx context.Context,
	resolver acquisition.Resolver,
	tasks []sourceTask,
	options Options,
) ([]sourceTaskResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	batchResolver, ok := resolver.(acquisition.BatchResolver)
	if ok && options.MaxParallelSourceOps > 0 {
		return batchSourceTaskResults(ctx, batchResolver, tasks, options)
	}

	return sequentialSourceTaskResults(ctx, resolver, tasks, options.Events)
}

func batchSourceTaskResults(
	ctx context.Context,
	resolver acquisition.BatchResolver,
	tasks []sourceTask,
	options Options,
) ([]sourceTaskResult, error) {
	requests := make([]acquisition.Request, 0, len(tasks))
	for _, task := range tasks {
		options.Events.Emit(task.event(task.resolveStartedEventKind(), nil))
		request, err := task.request()
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}

	results, err := resolver.ResolveBatch(
		ctx,
		requests,
		acquisition.NewBatchOptions(options.MaxParallelSourceOps, options.SourceEvents),
	)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(results) != len(tasks) {
		return nil, fmt.Errorf("source batch returned %d results for %d requests", len(results), len(tasks))
	}

	interpreted := make([]sourceTaskResult, 0, len(tasks))
	for index, result := range results {
		expected := requests[index]
		if err := result.Validate(); err != nil {
			return nil, fmt.Errorf("source batch result[%d]: %w", index, err)
		}
		if !result.Request().Equal(expected) {
			return nil, fmt.Errorf("source batch result[%d] echoed request %q, want %q", index, result.Request().ID(), expected.ID())
		}

		task := tasks[index]
		interpretedResult := sourceTaskResult{
			task: task,
			err:  task.wrapSourceError(result.Err()),
		}
		if interpretedResult.err == nil {
			switch task.operation {
			case acquisition.OperationResolve:
				resolution, ok := result.Resolution()
				if !ok {
					return nil, fmt.Errorf("source batch result[%d] omitted resolution", index)
				}
				interpretedResult.resolution = resolution
			case acquisition.OperationListRoot:
				listing, ok := result.Listing()
				if !ok {
					return nil, fmt.Errorf("source batch result[%d] omitted root listing", index)
				}
				interpretedResult.listing = listing
			default:
				return nil, fmt.Errorf("source batch result[%d] has unknown operation %q", index, task.operation)
			}
		}
		emitSourceTaskResultEvent(options.Events, interpretedResult)
		interpreted = append(interpreted, interpretedResult)
	}

	return interpreted, nil
}

func sequentialSourceTaskResults(
	ctx context.Context,
	resolver acquisition.Resolver,
	tasks []sourceTask,
	events EventSink,
) ([]sourceTaskResult, error) {
	results := make([]sourceTaskResult, 0, len(tasks))
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		events.Emit(task.event(task.resolveStartedEventKind(), nil))

		result := sourceTaskResult{task: task}
		switch task.operation {
		case acquisition.OperationResolve:
			resolvedArtifact, err := resolver.Resolve(ctx, task.sourceSpec, acquisition.OperationOptions{})
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, ctxErr
				}
				result.err = task.wrapSourceError(err)
				emitSourceTaskResultEvent(events, result)
				results = append(results, result)
				return results, nil
			}
			result.resolution = resolvedArtifact
		case acquisition.OperationListRoot:
			lister, ok := resolver.(acquisition.RootLister)
			if !ok {
				result.err = task.wrapSourceError(fmt.Errorf("source root listing is unsupported by resolver"))
				emitSourceTaskResultEvent(events, result)
				results = append(results, result)
				return results, nil
			}
			listing, err := lister.ListSourceRoot(ctx, task.sourceSpec, acquisition.OperationOptions{})
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, ctxErr
				}
				result.err = task.wrapSourceError(err)
				emitSourceTaskResultEvent(events, result)
				results = append(results, result)
				return results, nil
			}
			result.listing = listing
		default:
			result.err = task.wrapSourceError(fmt.Errorf("unknown source operation %q", task.operation))
			emitSourceTaskResultEvent(events, result)
			results = append(results, result)
			return results, nil
		}

		emitSourceTaskResultEvent(events, result)
		results = append(results, result)
	}

	return results, nil
}

func emitSourceTaskResultEvent(events EventSink, result sourceTaskResult) {
	if result.err != nil {
		events.Emit(result.task.event(result.task.resolveFailedEventKind(), result.err))
		return
	}

	kind := result.task.resolveSucceededEventKind()
	if kind == "" {
		return
	}
	events.Emit(result.task.event(kind, nil))
}

func firstSourceTaskError(ctx context.Context, results []sourceTaskResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, result := range results {
		if result.err != nil {
			return result.err
		}
	}

	return nil
}
