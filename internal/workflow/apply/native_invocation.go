package apply

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/supply/artifact"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/internal/workflow/protection"
)

type nativeInvocationProgram struct {
	keys      []nativeInvocationKey
	documents []nativeInvocationRead
	state     rootedpath.PathLayout
	commands  map[string]*nativePreparedInvocation
}

type nativeInvocationRead struct {
	layout  rootedpath.PathLayout
	aliases []aggregate.DocumentAddress
	syntax  configdocument.Syntax
}

type nativePreparedInvocation struct {
	protection *protection.InvocationLayout
	reads      recovery.InvocationWorkReservation
	rewrite    nativeInvocationRewrite
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

func prepareNativeInvocations(ctx context.Context, current commandPlan, leases *mutation.LeaseSet, requests []mutation.PhysicalAuthorityRequest) (*nativeInvocationProgram, error) {
	if current.nativePlan == nil || !current.nativePlan.opaque {
		return nil, nil
	}
	input, err := nativeApplyInputFor(current)
	if err != nil {
		return nil, err
	}
	program := &nativeInvocationProgram{commands: make(map[string]*nativePreparedInvocation)}
	program.keys, err = nativeInvocationKeys(ctx, current.assessment.NativeSources)
	if err != nil {
		return nil, err
	}
	program.state, err = rootedpath.CaptureDirectoryEntryLayoutBounded(current.context.Paths.StatefilePath, recovery.MaximumPhysicalPathDepth, input.Budget)
	if err != nil {
		return nil, err
	}
	for _, namespace := range input.Documents.Namespaces() {
		aliases := namespace.Addresses()
		path, err := destinationResolver(current.context.Paths).Resolve(aliases[0].AggregateRoot())
		if err != nil {
			return nil, err
		}
		layout, err := rootedpath.CaptureDirectoryEntryLayoutBounded(path, recovery.MaximumPhysicalPathDepth, input.Budget)
		if err != nil {
			return nil, err
		}
		syntax, err := nativeInvocationSyntax(current, aliases)
		if err != nil {
			return nil, err
		}
		program.documents = append(program.documents, nativeInvocationRead{layout: layout, aliases: aliases, syntax: syntax})
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
	for _, action := range current.assessment.Reconciliation.Relations() {
		if !action.InvokesHostRoute() {
			continue
		}
		command, err := executehostroute.BuildCommand(executehostroute.BuildInput{Action: action, Lockfile: current.context.Lockfile, WorkDir: current.context.Paths.ManifestRoot})
		if err != nil {
			return nil, err
		}
		rewrite, err := nativePiInvocationRewrite(current, action)
		if err != nil {
			return nil, err
		}
		prepared := &nativePreparedInvocation{rewrite: rewrite}
		var reads, correlation nativeInvocationWork
		for range 2 {
			for _, document := range program.documents {
				if err := document.layout.ReserveDestinationCapture(recovery.MaximumPhysicalPathDepth, &reads); err != nil {
					return nil, err
				}
				if err := document.layout.ReserveDestinationAccess(recovery.MaximumPhysicalPathDepth, &reads); err != nil {
					return nil, err
				}
				reads.bytes += nativeconfig.DocumentLimits().MaximumBytes
			}
		}
		if err := program.state.ReserveDestinationCapture(recovery.MaximumPhysicalPathDepth, &correlation); err != nil {
			return nil, err
		}
		if err := program.state.ReserveDestinationAccess(recovery.MaximumPhysicalPathDepth, &correlation); err != nil {
			return nil, err
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
			correlation, err := ownership.NewProtectionCorrelation(operationID, strings.TrimPrefix(action.RouteRequest().CanonicalRequestHash(), "sha256:"), selectionFingerprint)
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
		} else {
			reads.paths = append(reads.paths, correlation.paths...)
			reads.entries += correlation.entries
			reads.bytes += correlation.bytes
		}
		readWork, err := reads.compile()
		if err != nil {
			return nil, err
		}
		prepared.reads, err = input.Budget.ReserveInvocation(readWork, recovery.InvocationWork{})
		if err != nil {
			return nil, err
		}
		key, err := nativeInvocationCommandKey(command.AttemptRequest())
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

func nativeInvocationSyntax(current commandPlan, aliases []aggregate.DocumentAddress) (configdocument.Syntax, error) {
	for _, source := range current.assessment.NativeSources {
		placement, _ := source.SourceContract().HostConfigPlacement()
		for _, alias := range aliases {
			if alias.Target() == placement.Target() && alias.Scope() == placement.Scope() && alias.AggregateRoot() == placement.Destination() {
				return configdocument.ParseSyntax(string(placement.Grammar()))
			}
		}
	}
	for _, state := range current.assessment.CurrentState.ManagedAggregates() {
		name, native := aggregate.HostConfigSyntaxForCodec(state.Contribution().CodecContractID())
		if native {
			for _, alias := range aliases {
				if alias == state.Contribution().Contract().Address().Document() {
					return configdocument.ParseSyntax(name)
				}
			}
		}
	}
	return 0, fmt.Errorf("native invocation document has no selected syntax")
}

func nativePiInvocationRewrite(current commandPlan, action reconcile.RelationAction) (nativeInvocationRewrite, error) {
	key := action.CarrierIdentity().Carrier().Key()
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: action.Target(), Scope: action.Scope(), Path: "settings.json", Ownership: hostconfig.OwnershipKeys})
	if err != nil {
		return nativeInvocationRewrite{}, err
	}
	wanted, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
	if err != nil {
		return nativeInvocationRewrite{}, err
	}
	physical, err := destinationResolver(current.context.Paths).Resolve(placement.Destination())
	if err != nil {
		return nativeInvocationRewrite{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nativeInvocationRewrite{}, err
	}
	sourceContext, err := extensiontopology.NewLocalSourceContext(filepath.Dir(physical), home)
	if err != nil {
		return nativeInvocationRewrite{}, err
	}
	selectedSource := key.Source().Ref()
	interpreted, err := extensiontopology.InterpretCarrierSource(key)
	if err != nil {
		return nativeInvocationRewrite{}, err
	}
	if interpreted.Class() == extensiontopology.CarrierSourceLocal {
		commandContext, err := extensiontopology.NewLocalSourceContext(current.context.Paths.ManifestRoot, home)
		if err != nil {
			return nativeInvocationRewrite{}, err
		}
		absolute, err := interpreted.ResolveLocal(commandContext)
		if err != nil {
			return nativeInvocationRewrite{}, err
		}
		selectedSource = absolute.Path()
	}
	for _, group := range current.assessment.NativeApproval {
		for _, address := range group.Aliases {
			if address == wanted {
				return nativeInvocationRewrite{aliases: append([]aggregate.DocumentAddress(nil), group.Aliases...), preserve: func(ctx context.Context, before, after configdocument.Document) (bool, error) {
					return pipackage.PreservesOutsidePackageRewrite(ctx, before, after, selectedSource, key.Scope(), sourceContext)
				}}, nil
			}
		}
	}
	return nativeInvocationRewrite{}, nil
}

func (program *nativeInvocationProgram) read(ctx context.Context, budget *recovery.PhysicalWorkBudget) (map[aggregate.DocumentAddress]configdocument.Document, error) {
	documents := make(map[aggregate.DocumentAddress]configdocument.Document)
	for _, selected := range program.documents {
		document, err := selected.read(ctx, budget)
		if err != nil {
			return nil, err
		}
		for _, alias := range selected.aliases {
			documents[alias] = document
		}
	}
	return documents, nil
}

func (selected nativeInvocationRead) read(ctx context.Context, budget *recovery.PhysicalWorkBudget) (document configdocument.Document, returnErr error) {
	root, destination, err := selected.layout.CaptureDestinationBounded(recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return document, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	capability, err := root.AcquireBounded(destination, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return document, err
	}
	content, _, _, readErr := storagecommit.ReadRootedRegularFileUpTo(ctx, capability, nativeconfig.DocumentLimits().MaximumBytes)
	closeErr := capability.Close()
	charged := int64(len(content))
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		charged = nativeconfig.DocumentLimits().MaximumBytes
	}
	work, err := recovery.NewArtifactWork(0, charged)
	if err == nil {
		err = budget.AdmitTree(work)
	}
	if err := errors.Join(closeErr, err, ctx.Err()); err != nil {
		return document, err
	}
	if errors.Is(readErr, fs.ErrNotExist) {
		return configdocument.Absent(selected.syntax, nativeconfig.DocumentLimits())
	}
	if readErr != nil {
		return document, readErr
	}
	return configdocument.Parse(ctx, content, selected.syntax, nativeconfig.DocumentLimits())
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
	var before map[aggregate.DocumentAddress]configdocument.Document
	prepare := func(ctx context.Context, budget *recovery.PhysicalWorkBudget) error {
		var err error
		before, err = program.read(ctx, reads)
		if err != nil {
			return err
		}
		if err := qualifyNativeInvocationDocuments(ctx, program.keys, before, before, nativeInvocationRewrite{}); err != nil {
			return err
		}
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
	if observed && before != nil {
		after, err := program.read(ctx, reads)
		if err == nil {
			err = qualifyNativeInvocationDocuments(ctx, program.keys, before, after, prepared.rewrite)
		}
		returnErr = errors.Join(returnErr, err)
	}
	return attempt, observed, returnErr
}
