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
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/internal/workflow/protection"
)

type nativeInvocationProgram struct {
	documents nativeconfig.InvocationDocuments
	state     rootedpath.PathLayout
	commands  map[string]*nativePreparedInvocation
}

type nativePreparedInvocation struct {
	protection *protection.InvocationLayout
	reads      recovery.InvocationWorkReservation
	rewrite    relationhost.NativeSettingsRewrites
	used       bool
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

func (work nativeInvocationWork) compile() (recovery.InvocationWork, error) {
	content, err := recovery.NewArtifactWork(work.entries, work.bytes)
	if err != nil {
		return recovery.InvocationWork{}, err
	}
	return recovery.NewInvocationWork(work.paths, content)
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
	program := &nativeInvocationProgram{commands: make(map[string]*nativePreparedInvocation)}
	native, present := current.nativeOperation.NativeInput()
	if !present {
		return nil, fmt.Errorf("native invocation requires its retained document operation")
	}
	program.documents, err = nativeconfig.PrepareInvocationDocuments(ctx, current.context.Paths, native, current.assessment.NativeSources)
	if err != nil {
		return nil, err
	}
	program.state, err = rootedpath.CaptureDirectoryEntryLayoutBounded(current.context.Paths.StatefilePath, recovery.MaximumPhysicalPathDepth, input.Budget)
	if err != nil {
		return nil, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	operationID := hex.EncodeToString(random[:])
	_, members, err := input.Documents.CaptureMembers(input.Preparation.AfterState(), operationID)
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
		prepared := &nativePreparedInvocation{rewrite: command.rewrite}
		var correlation nativeInvocationWork
		if command.correlation {
			if err := program.state.ReserveDestinationCapture(recovery.MaximumPhysicalPathDepth, &correlation); err != nil {
				return nil, err
			}
			// Correlation captures identity and then publishes: two rooted operations.
			for range 2 {
				if err := program.state.ReserveDestinationAccess(recovery.MaximumPhysicalPathDepth, &correlation); err != nil {
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
			prepared.protection = &layout
			correlationWork = recovery.InvocationWork{}
		}
		prepared.reads, err = program.documents.ReserveReads(input.Budget, correlationWork)
		if err != nil {
			return nil, err
		}
		if _, duplicate := program.commands[command.key]; duplicate {
			return nil, fmt.Errorf("native invocation schedule repeats one exact command")
		}
		program.commands[command.key] = prepared
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

func (program *nativeInvocationProgram) commitCorrelation(ctx context.Context, budget *recovery.PhysicalWorkBudget, commit func(*rootedpath.EntryAuthority) (durable.Snapshot, error)) (state durable.Snapshot, returnErr error) {
	root, destination, err := program.state.CaptureDestinationBounded(recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return state, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	entry, err := rootedpath.BindCapturedEntryAuthorityBounded(root, destination, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return state, err
	}
	defer func() { returnErr = errors.Join(returnErr, entry.Close()) }()
	return commit(entry)
}

func (program *nativeInvocationProgram) execute(ctx context.Context, executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, binding subprocess.WorkingDirectoryBinding, correlation func(context.Context, *recovery.PhysicalWorkBudget) error) (attempt subprocess.CommandAttemptResult, observed bool, returnErr error) {
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

func (program *nativeInvocationProgram) executeCommand(ctx context.Context, key string, executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, bind subprocess.WorkingDirectoryBinder, correlation func(context.Context, *recovery.PhysicalWorkBudget) error) (attempt subprocess.CommandAttemptResult, observed bool, returnErr error) {
	prepared, present := program.commands[key]
	if !present || prepared.used {
		return attempt, false, fmt.Errorf("native command is not available in its reserved invocation schedule")
	}
	prepared.used = true
	reads, _, err := prepared.reads.Take()
	if err != nil {
		return attempt, false, err
	}
	var before nativeconfig.InvocationBaseline
	preparedContent := false
	prepare := func(ctx context.Context, budget *recovery.PhysicalWorkBudget) error {
		var err error
		before, err = program.documents.Read(ctx, reads)
		if err != nil {
			return err
		}
		if err := program.documents.Qualify(ctx, before, before, relationhost.NativeSettingsRewrites{}); err != nil {
			return err
		}
		preparedContent = true
		if correlation != nil {
			return correlation(ctx, budget)
		}
		return nil
	}
	if prepared.protection != nil {
		input, err := prepared.protection.AttachProtection(ctx)
		if err != nil {
			return attempt, false, err
		}
		result, err := protection.ExecuteInvocation(ctx, input, executor, request, bind, prepare)
		returnErr = err
		if process, present := result.Process(); present {
			attempt, observed = process.Attempt(), true
		}
	} else {
		if err := prepare(ctx, reads); err != nil {
			return attempt, false, err
		}
		attempt = executor.ExecuteInWorkingDirectory(ctx, request, bind)
		observed = true
	}
	if observed && preparedContent {
		after, err := program.documents.Read(ctx, reads)
		if err == nil {
			err = program.documents.Qualify(ctx, before, after, prepared.rewrite)
		}
		returnErr = errors.Join(returnErr, err)
	}
	return attempt, observed, returnErr
}
