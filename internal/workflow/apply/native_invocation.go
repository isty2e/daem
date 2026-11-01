package apply

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/codexplugin"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/supply/artifact"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
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
	rewrite    nativeconfig.InvocationRewrite
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

func prepareNativeInvocations(ctx context.Context, current commandPlan, leases *mutation.LeaseSet, requests []mutation.PhysicalAuthorityRequest, removalAdapter executehostroute.RemovalAdapter) (*nativeInvocationProgram, error) {
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
	commands, err := nativeInvocationCommands(current, removalAdapter)
	if err != nil {
		return nil, err
	}
	for _, command := range commands {
		rewrite, err := nativeInvocationRewrite(current, command.carrier)
		if err != nil {
			return nil, err
		}
		prepared := &nativePreparedInvocation{rewrite: rewrite}
		var correlation nativeInvocationWork
		if err := program.state.ReserveDestinationCapture(recovery.MaximumPhysicalPathDepth, &correlation); err != nil {
			return nil, err
		}
		// Correlation captures identity and then publishes: two rooted operations.
		for range 2 {
			if err := program.state.ReserveDestinationAccess(recovery.MaximumPhysicalPathDepth, &correlation); err != nil {
				return nil, err
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
			correlation, err := ownership.NewProtectionCorrelation(operationID, strings.TrimPrefix(command.command.RouteRequest().CanonicalRequestHash(), "sha256:"), selectionFingerprint)
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
		key, err := nativeInvocationCommandKey(command.command.AttemptRequest())
		if err != nil {
			return nil, err
		}
		if _, duplicate := program.commands[key]; duplicate {
			return nil, fmt.Errorf("native invocation schedule repeats one exact command")
		}
		program.commands[key] = prepared
	}
	return program, nil
}

func nativeInvocationRewrite(current commandPlan, key desiredextension.CarrierKey) (nativeconfig.InvocationRewrite, error) {
	switch key.Carrier() {
	case desiredextension.CarrierPiPackage:
		return nativePiInvocationRewrite(current, key)
	case desiredextension.CarrierCodexPlugin:
		wanted, err := codexplugin.ConfigDocumentAddress()
		if err != nil {
			return nativeconfig.InvocationRewrite{}, err
		}
		for _, group := range current.assessment.NativeApproval {
			for _, address := range group.Aliases {
				if address == wanted {
					return nativeconfig.InvocationRewrite{Aliases: append([]aggregate.DocumentAddress(nil), group.Aliases...), Preserve: func(ctx context.Context, before, after configdocument.Document) (bool, error) {
						return codexplugin.PreservesOutsidePluginRewrite(ctx, before, after, key.Source())
					}}, nil
				}
			}
		}
		return nativeconfig.InvocationRewrite{}, nil
	case desiredextension.CarrierClaudeCodePlugin, desiredextension.CarrierAntigravityCLIPlugin, desiredextension.CarrierOpenCodePlugin:
		return nativeconfig.InvocationRewrite{}, nil
	default:
		return nativeconfig.InvocationRewrite{}, fmt.Errorf("native invocation rewrite profile is unavailable")
	}
}

func nativePiInvocationRewrite(current commandPlan, key desiredextension.CarrierKey) (nativeconfig.InvocationRewrite, error) {
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: key.Target(), Scope: key.Scope(), Path: "settings.json", Ownership: hostconfig.OwnershipKeys})
	if err != nil {
		return nativeconfig.InvocationRewrite{}, err
	}
	wanted, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
	if err != nil {
		return nativeconfig.InvocationRewrite{}, err
	}
	physical, err := destinationResolver(current.context.Paths).Resolve(placement.Destination())
	if err != nil {
		return nativeconfig.InvocationRewrite{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nativeconfig.InvocationRewrite{}, err
	}
	sourceContext, err := extensiontopology.NewLocalSourceContext(filepath.Dir(physical), home)
	if err != nil {
		return nativeconfig.InvocationRewrite{}, err
	}
	selectedSource := key.Source().Ref()
	interpreted, err := extensiontopology.InterpretCarrierSource(key)
	if err != nil {
		return nativeconfig.InvocationRewrite{}, err
	}
	if interpreted.Class() == extensiontopology.CarrierSourceLocal {
		commandContext, err := extensiontopology.NewLocalSourceContext(current.context.Paths.ManifestRoot, home)
		if err != nil {
			return nativeconfig.InvocationRewrite{}, err
		}
		absolute, err := interpreted.ResolveLocal(commandContext)
		if err != nil {
			return nativeconfig.InvocationRewrite{}, err
		}
		selectedSource = absolute.Path()
	}
	for _, group := range current.assessment.NativeApproval {
		for _, address := range group.Aliases {
			if address == wanted {
				return nativeconfig.InvocationRewrite{Aliases: append([]aggregate.DocumentAddress(nil), group.Aliases...), Preserve: func(ctx context.Context, before, after configdocument.Document) (bool, error) {
					return pipackage.PreservesOutsidePackageRewrite(ctx, before, after, selectedSource, key.Scope(), sourceContext)
				}}, nil
			}
		}
	}
	return nativeconfig.InvocationRewrite{}, nil
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
	key, err := nativeInvocationCommandKey(request)
	if err != nil {
		return attempt, false, errors.Join(err, binding.Close())
	}
	prepared, present := program.commands[key]
	if !present || prepared.used {
		return attempt, false, errors.Join(fmt.Errorf("native command is not available in its reserved invocation schedule"), binding.Close())
	}
	prepared.used = true
	transferred := false
	defer func() {
		if !transferred {
			returnErr = errors.Join(returnErr, binding.Close())
		}
	}()
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
		if err := program.documents.Qualify(ctx, before, before, nil); err != nil {
			return err
		}
		preparedContent = true
		return correlation(ctx, budget)
	}
	if prepared.protection != nil {
		input, err := prepared.protection.AttachProtection(ctx)
		if err != nil {
			return attempt, false, err
		}
		result, err := protection.ExecuteInvocation(ctx, input, executor, request, func() (subprocess.WorkingDirectoryBinding, error) {
			transferred = true
			return binding, nil
		}, prepare)
		returnErr = err
		if process, present := result.Process(); present {
			attempt, observed = process.Attempt(), true
		}
	} else {
		if err := prepare(ctx, reads); err != nil {
			return attempt, false, err
		}
		attempt = executor.ExecuteInWorkingDirectory(ctx, request, func() (subprocess.WorkingDirectoryBinding, error) {
			transferred = true
			return binding, nil
		})
		observed = true
	}
	if observed && preparedContent {
		after, err := program.documents.Read(ctx, reads)
		if err == nil {
			err = program.documents.Qualify(ctx, before, after, []nativeconfig.InvocationRewrite{prepared.rewrite})
		}
		returnErr = errors.Join(returnErr, err)
	}
	return attempt, observed, returnErr
}
