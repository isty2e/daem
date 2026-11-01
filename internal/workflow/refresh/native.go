package refresh

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe/codexplugin"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/executionidentity"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	"github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/internal/workflow/protection"
)

type nativeRefreshInputs struct {
	environment         desired.Environment
	requests            []mutation.PhysicalAuthorityRequest
	registryIdentity    string
	approvalFingerprint string
}

type nativeRefreshObservation struct {
	operation *nativeconfig.DocumentObservation
	documents nativeconfig.InvocationDocuments
	inputs    nativeRefreshInputs
}

func observeNativeRefresh(ctx context.Context, paths daempaths.Paths, environment desired.Environment, locked lock.File, before durable.Snapshot) (result *nativeRefreshObservation, returnErr error) {
	if len(environment.HostConfigs()) == 0 && !locked.ContainsHostConfig() && !before.ContainsHostConfig() {
		return nil, nil
	}
	selection, err := targetselection.ForDiagnostics(nil)
	if err != nil {
		return nil, err
	}
	nativeOnly, err := environment.HostConfigsOnly()
	if err != nil {
		return nil, err
	}
	epoch, err := nativeconfig.ResolveSourceEpoch(ctx, paths, nativeOnly, locked, selection)
	if err != nil {
		return nil, err
	}
	var sources []lockobserve.NativeSourceObservation
	for _, config := range nativeOnly.HostConfigs() {
		source, err := epoch.NativeSource(ctx, config)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	persistence, err := nativeconfig.LoadPersistenceEpoch(ctx, paths)
	if err != nil {
		return nil, err
	}
	actual, err := persistence.Snapshot()
	if err != nil || !actual.Equal(before) {
		return nil, errors.Join(mutation.StaleSnapshotError{}, err)
	}
	operation, err := nativeconfig.ObserveDocuments(ctx, nativeconfig.DocumentObservationInput{Paths: paths, Environment: nativeOnly, Lockfile: locked, Selection: selection, PersistenceEpoch: &persistence})
	if err != nil {
		return nil, err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, operation.Close())
			result = nil
		}
	}()
	input, present := operation.NativeInput()
	if !present {
		return nil, operation.Close()
	}
	documents, err := nativeconfig.PrepareInvocationDocuments(ctx, paths, input, sources)
	if err != nil {
		return nil, err
	}
	if err := documents.RequireManagedState(before); err != nil {
		return nil, err
	}
	baseline, err := documents.Read(ctx, input.Budget)
	if err != nil {
		return nil, err
	}
	if err := documents.Qualify(ctx, baseline, baseline, nil); err != nil {
		return nil, err
	}
	inputs := nativeRefreshInputs{environment: nativeOnly}
	for _, namespace := range input.Documents.Namespaces() {
		for _, alias := range namespace.Addresses() {
			path, err := managedhostpath.Resolver(paths).Resolve(alias.AggregateRoot())
			if err != nil {
				return nil, err
			}
			inputs.requests = append(inputs.requests, mutation.PhysicalAuthorityRequest{Path: path, Target: string(alias.Target()), Scope: string(alias.Scope())})
		}
	}
	physical, err := mutation.NewPhysicalAuthoritySet(inputs.requests...)
	if err != nil {
		return nil, err
	}
	scope, err := recoverygate.DocumentScope(physical)
	if err != nil {
		return nil, err
	}
	view, err := input.Registry.Documents(scope)
	if err != nil {
		return nil, err
	}
	if err := view.RequireProtectionClear(); err != nil {
		return nil, err
	}
	inputs.registryIdentity, err = view.Fingerprint()
	if err != nil {
		return nil, err
	}
	approval, err := input.Documents.ApprovalEvidence()
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Alias      string
		Protection string
	}
	for _, group := range approval {
		for _, alias := range group.Aliases {
			rows = append(rows, struct {
				Alias      string
				Protection string
			}{string(alias.Target()) + "\x00" + string(alias.Scope()) + "\x00" + alias.AggregateRoot().String(), group.Protection})
		}
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	inputs.approvalFingerprint = hex.EncodeToString(digest[:])
	return &nativeRefreshObservation{operation: operation, documents: documents, inputs: inputs}, nil
}

func planNativeRefresh(ctx context.Context, paths daempaths.Paths, environment desired.Environment, locked lock.File, before durable.Snapshot, selected target.Target, scope target.Scope) (*nativeRefreshInputs, error) {
	observed, err := observeNativeRefresh(ctx, paths, environment, locked, before)
	if err != nil || observed == nil {
		return nil, err
	}
	namespaces, handled, err := relationhost.OpaquePluginNamespaces(paths, selected, scope)
	if err == nil && handled {
		input, _ := observed.operation.NativeInput()
		var native []mutation.DirectoryEntryAuthorityObservation
		for _, namespace := range input.Documents.Namespaces() {
			native = append(native, namespace.Observation())
		}
		err = nativeconfig.RequireDisjointOpaqueNamespaces(ctx, native, namespaces, input.Budget)
	}
	err = errors.Join(err, observed.operation.Close())
	if err != nil {
		return nil, err
	}
	return &observed.inputs, nil
}

type nativeRefreshInvocation struct {
	observation *nativeRefreshObservation
	reads       recovery.InvocationWorkReservation
	protection  *protection.InvocationLayout
	rewrites    []nativeconfig.InvocationRewrite
}

func prepareNativeRefreshInvocation(ctx context.Context, current plan, leases *mutation.LeaseSet) (result *nativeRefreshInvocation, returnErr error) {
	if current.native == nil {
		return nil, nil
	}
	observed, err := observeNativeRefresh(ctx, current.paths, current.native.environment, current.lockfile, current.currentState)
	if err != nil {
		return nil, err
	}
	if observed == nil {
		return nil, fmt.Errorf("native refresh observation disappeared")
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, observed.operation.Close())
			result = nil
		}
	}()
	if observed.inputs.registryIdentity != current.native.registryIdentity || observed.inputs.approvalFingerprint != current.native.approvalFingerprint {
		return nil, mutation.StaleSnapshotError{}
	}
	result = &nativeRefreshInvocation{observation: observed}
	input, present := observed.operation.NativeInput()
	if !present {
		return nil, fmt.Errorf("native refresh observation was closed")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	operationID := hex.EncodeToString(random[:])
	_, members, err := input.Documents.CaptureMembers(current.currentState, operationID)
	if err != nil {
		return nil, err
	}
	result.reads, err = observed.documents.ReserveReads(input.Budget, recovery.InvocationWork{})
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
		encoded, err := statefile.Marshal(current.currentState)
		if err != nil {
			return nil, err
		}
		selection := sha256.Sum256(encoded)
		correlation, err := ownership.NewProtectionCorrelation(operationID, strings.TrimPrefix(current.routeRequest.CanonicalRequestHash(), "sha256:"), hex.EncodeToString(selection[:]))
		if err != nil {
			return nil, err
		}
		layout, err := protection.PrepareInvocationLayout(ctx, protection.InvocationLayoutInput{Paths: current.paths, Owner: input.Owner, Invocation: invocation, Correlation: correlation, Leases: leases, Requests: current.native.requests, Members: members}, input.Budget)
		if err != nil {
			return nil, err
		}
		if err := layout.ReserveWork(input.Budget, recovery.InvocationWork{}); err != nil {
			return nil, err
		}
		result.protection = &layout
	}
	result.rewrites, err = nativeRefreshRewrites(ctx, current, input.Documents, input.Budget)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func nativeRefreshRewrites(ctx context.Context, current plan, documents *journal.NativeDocumentObservationBatch, budget *recovery.PhysicalWorkBudget) ([]nativeconfig.InvocationRewrite, error) {
	namespaces, handled, err := relationhost.OpaquePluginNamespaces(current.paths, current.result.Selection.Target, current.result.Selection.Scope)
	if err != nil {
		return nil, err
	}
	if handled {
		var native []mutation.DirectoryEntryAuthorityObservation
		for _, namespace := range documents.Namespaces() {
			native = append(native, namespace.Observation())
		}
		return nil, nativeconfig.RequireDisjointOpaqueNamespaces(ctx, native, namespaces, budget)
	}
	if current.result.Selection.Target != target.TargetPi && current.result.Selection.Target != target.TargetCodex {
		return nil, fmt.Errorf("native settings protection for this refresh host is not connected")
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(current.contract)
	if err != nil || !admitted {
		return nil, errors.Join(fmt.Errorf("refresh carrier identity is unavailable"), err)
	}
	key := identity.Carrier().Key()
	if current.result.Selection.Target == target.TargetCodex {
		wanted, err := codexplugin.ConfigDocumentAddress()
		if err != nil {
			return nil, err
		}
		for _, namespace := range documents.Namespaces() {
			for _, alias := range namespace.Addresses() {
				if alias == wanted {
					return []nativeconfig.InvocationRewrite{{Aliases: namespace.Addresses(), Preserve: func(ctx context.Context, before, after configdocument.Document) (bool, error) {
						return codexplugin.PreservesOutsideMarketplaceRewrite(ctx, before, after, key.Source())
					}}}, nil
				}
			}
		}
		return nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	selectedSource := key.Source().Ref()
	interpreted, err := extensiontopology.InterpretCarrierSource(key)
	if err != nil {
		return nil, err
	}
	if interpreted.Class() == extensiontopology.CarrierSourceLocal {
		commandContext, err := extensiontopology.NewLocalSourceContext(current.paths.ManifestRoot, home)
		if err != nil {
			return nil, err
		}
		absolute, err := interpreted.ResolveLocal(commandContext)
		if err != nil {
			return nil, err
		}
		selectedSource = absolute.Path()
	}
	var rewrites []nativeconfig.InvocationRewrite
	for _, namespace := range documents.Namespaces() {
		for _, alias := range namespace.Addresses() {
			if alias.Target() != target.TargetPi {
				continue
			}
			wanted, err := pipackage.SettingsDocumentAddress(alias.Scope())
			if err != nil {
				return nil, err
			}
			if alias != wanted {
				continue
			}
			path, err := managedhostpath.Resolver(current.paths).Resolve(alias.AggregateRoot())
			if err != nil {
				return nil, err
			}
			sourceContext, err := extensiontopology.NewLocalSourceContext(filepath.Dir(path), home)
			if err != nil {
				return nil, err
			}
			rewrites = append(rewrites, nativeconfig.InvocationRewrite{Aliases: []aggregate.DocumentAddress{alias}, Preserve: func(ctx context.Context, before, after configdocument.Document) (bool, error) {
				return pipackage.PreservesOutsidePackageRewrite(ctx, before, after, selectedSource, alias.Scope(), sourceContext)
			}})
		}
	}
	return rewrites, nil
}

func (invocation *nativeRefreshInvocation) execute(ctx context.Context, executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, bind subprocess.WorkingDirectoryBinder) (attempt subprocess.CommandAttemptResult, observed bool, returnErr error) {
	reads, _, err := invocation.reads.Take()
	if err != nil {
		return attempt, false, err
	}
	var before nativeconfig.InvocationBaseline
	prepared := false
	prepare := func(ctx context.Context, _ *recovery.PhysicalWorkBudget) error {
		var err error
		before, err = invocation.observation.documents.Read(ctx, reads)
		if err != nil {
			return err
		}
		if err := invocation.observation.documents.Qualify(ctx, before, before, nil); err != nil {
			return err
		}
		prepared = true
		return nil
	}
	if invocation.protection != nil {
		input, err := invocation.protection.AttachProtection(ctx)
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
		attempt, observed = executor.ExecuteInWorkingDirectory(ctx, request, bind), true
	}
	if observed && prepared {
		postCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), attemptPersistenceTimeout)
		defer cancel()
		after, err := invocation.observation.documents.Read(postCtx, reads)
		if err == nil {
			err = invocation.observation.documents.Qualify(postCtx, before, after, invocation.rewrites)
		}
		returnErr = errors.Join(returnErr, err)
	}
	return attempt, observed, returnErr
}
