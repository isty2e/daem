package apply

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	configurationinvocation "github.com/isty2e/daem/internal/workflow/nativeconfig/invocation"
	protection "github.com/isty2e/daem/internal/workflow/protection/invocation"
)

type nativeInvocationProgram struct {
	documents nativeconfig.InvocationDocuments
	state     rootedpath.PathLayout
	commands  map[string]*configurationinvocation.Invocation
}

type nativeInvocationWork struct {
	paths   []int
	entries int
	bytes   int64
}

func (work *nativeInvocationWork) AdmitPathComponents(count int) error {
	work.paths = append(work.paths, count)
	return nil
}

func (work *nativeInvocationWork) AdmitPhysicalWork(paths, entries int, bytes int64) error {
	work.paths = append(work.paths, paths)
	work.entries += entries
	work.bytes += bytes
	return nil
}

func (work nativeInvocationWork) compile() (physicalwork.InvocationWork, error) {
	content, err := workmeasure.NewArtifactWork(work.entries, work.bytes)
	if err != nil {
		return physicalwork.InvocationWork{}, err
	}
	return physicalwork.NewInvocationWork(work.paths, content)
}

func nativeInvocationCommandKey(request subprocess.CommandAttemptRequest) (string, error) {
	encoded, err := json.Marshal(request)
	return string(encoded), err
}

func prepareNativeInvocations(ctx context.Context, current commandPlan, leases *mutation.LeaseSet, requests []mutation.PhysicalAuthorityRequest, removalAdapter executehostroute.RemovalAdapter, executor delegate.Executor) (*nativeInvocationProgram, error) {
	if current.nativePlan == nil || !current.nativePlan.opaque {
		return nil, nil
	}
	input, err := nativePrefixApplyInputFor(current)
	if err != nil {
		return nil, err
	}
	program := &nativeInvocationProgram{commands: make(map[string]*configurationinvocation.Invocation)}
	native, present := current.nativeOperation.NativeInput()
	if !present {
		return nil, fmt.Errorf("native invocation requires its retained document operation")
	}
	program.documents, err = nativeconfig.PrepareInvocationDocuments(ctx, current.context.Paths, native, current.assessment.NativeSources)
	if err != nil {
		return nil, err
	}
	program.state, err = rootedpath.CaptureDirectoryEntryLayoutBounded(current.context.Paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, input.Budget)
	if err != nil {
		return nil, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	operationID := hex.EncodeToString(random[:])
	_, members, err := input.Documents.CaptureMembers(input.Preparation.AfterState(), operationID, input.Preparation.SequenceEdits()...)
	if err != nil {
		return nil, err
	}
	members, err = nativeProtectionMembers(ctx, input.Documents, input.Preparation, members)
	if err != nil {
		return nil, err
	}
	selection, err := json.Marshal(aggregateFingerprintRows(current.assessment.Reconciliation.Aggregates()))
	if err != nil {
		return nil, err
	}
	selectionFingerprint := strings.TrimPrefix(string(artifact.HashFileContent(selection)), "sha256:")
	commands, err := nativeInvocationCommands(current, removalAdapter, executor)
	if err != nil {
		return nil, err
	}
	for _, command := range commands {
		var protected *protection.InvocationLayout
		var correlation nativeInvocationWork
		if command.correlation {
			if err := program.state.ReserveDestinationCapture(physicalwork.MaximumPhysicalPathDepth, &correlation); err != nil {
				return nil, err
			}
			// Correlation captures identity and then publishes: two rooted operations.
			for range 2 {
				if err := program.state.ReserveDestinationAccess(physicalwork.MaximumPhysicalPathDepth, &correlation); err != nil {
					return nil, err
				}
			}
		}
		correlationWork, err := correlation.compile()
		if err != nil {
			return nil, err
		}
		if len(members) != 0 {
			if _, err := rand.Read(random[:]); err != nil {
				return nil, err
			}
			invocation, err := executionidentity.NewInvocation(hex.EncodeToString(random[:]))
			if err != nil {
				return nil, err
			}
			correlation, err := ownership.NewProtectionCorrelation(operationID, command.fingerprint, selectionFingerprint)
			if err != nil {
				return nil, err
			}
			layout, err := protection.PrepareInvocationLayout(ctx, protection.InvocationLayoutInput{
				Paths: current.context.Paths, Owner: current.assessment.Owner, Invocation: invocation,
				Correlation: correlation, Leases: leases, Requests: requests, Members: members,
			}, input.Budget)
			if err != nil {
				return nil, err
			}
			if err := layout.ReserveWork(input.Budget, correlationWork); err != nil {
				return nil, err
			}
			protected = &layout
			correlationWork = physicalwork.InvocationWork{}
		}
		reads, err := program.documents.ReserveReads(input.Budget, correlationWork)
		if err != nil {
			return nil, err
		}
		if _, duplicate := program.commands[command.key]; duplicate {
			return nil, fmt.Errorf("native invocation schedule repeats one exact command")
		}
		program.commands[command.key] = configurationinvocation.New(program.documents, reads, command.rewrite, protected, configurationinvocation.CallerContext())
	}
	return program, nil
}

func nativeInvocationRewrite(current commandPlan, key desiredextension.CarrierKey, operation lock.OperationKind) (relationhost.NativeSettingsRewrites, error) {
	profile, err := relationhost.NativeSettingsProfileFor(key, operation)
	if err != nil {
		return relationhost.NativeSettingsRewrites{}, err
	}
	var groups [][]aggregate.DocumentAddress
	for _, group := range current.assessment.NativeApproval {
		groups = append(groups, group.Aliases)
	}
	return profile.BindInvocationRewrites(current.context.Paths, groups)
}

func (program *nativeInvocationProgram) commitCorrelation(ctx context.Context, budget *physicalwork.Budget, commit func(*rootedpath.EntryAuthority) (durable.Snapshot, error)) (state durable.Snapshot, returnErr error) {
	root, destination, err := program.state.CaptureDestinationBounded(physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return state, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	entry, err := rootedpath.BindCapturedEntryAuthorityBounded(root, destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return state, err
	}
	defer func() { returnErr = errors.Join(returnErr, entry.Close()) }()
	return commit(entry)
}

func (program *nativeInvocationProgram) execute(ctx context.Context, executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, binding subprocess.WorkingDirectoryBinding, correlation func(context.Context, *physicalwork.Budget) error) (attempt subprocess.CommandAttemptResult, observed bool, returnErr error) {
	transferred := false
	defer func() {
		if !transferred {
			returnErr = errors.Join(returnErr, binding.Close())
		}
	}()
	key, err := nativeInvocationCommandKey(request)
	if err != nil {
		return attempt, false, err
	}
	return program.executeCommand(ctx, key, executor, request, func() (subprocess.WorkingDirectoryBinding, error) {
		transferred = true
		return binding, nil
	}, correlation)
}

func (program *nativeInvocationProgram) executeDelegate(ctx context.Context, action reconcile.DelegateAction, executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, bind subprocess.WorkingDirectoryBinder) (subprocess.CommandAttemptResult, bool, error) {
	key, err := nativeDelegateInvocationKey(action, request)
	if err != nil {
		return subprocess.CommandAttemptResult{}, false, err
	}
	return program.executeCommand(ctx, key, executor, request, bind, nil)
}

func (program *nativeInvocationProgram) executeCommand(ctx context.Context, key string, executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, bind subprocess.WorkingDirectoryBinder, correlation func(context.Context, *physicalwork.Budget) error) (attempt subprocess.CommandAttemptResult, observed bool, returnErr error) {
	prepared, present := program.commands[key]
	if !present {
		return attempt, false, configurationinvocation.ErrUnavailable
	}
	return prepared.Execute(ctx, executor, request, bind, correlation)
}
