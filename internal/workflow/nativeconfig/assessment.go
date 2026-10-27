package nativeconfig

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output"
	codexhostpath "github.com/isty2e/daem/internal/output/hostpath/codex"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func assessRetainedInputs(ctx context.Context, input readiness.Input, operation *AssessmentOperation) (result readiness.Assessment, resultErr error) {
	if input.PersistenceEpoch == nil {
		return readiness.Assessment{}, fmt.Errorf("CLI assessment requires its selected persistence epoch")
	}
	before, err := input.PersistenceEpoch.Snapshot()
	if err != nil {
		return readiness.Assessment{}, err
	}
	codecs, err := Codecs(ctx)
	if err != nil {
		return readiness.Assessment{}, err
	}
	input.Codecs = codecs
	if input.SourceEpoch == nil {
		epoch, err := ResolveSourceEpoch(ctx, input.Paths, input.Environment, input.Lockfile, input.Selection)
		if err != nil {
			return readiness.Assessment{}, err
		}
		input.SourceEpoch = &epoch
	}

	aliases := make(map[aggregate.DocumentAddress]struct{})
	add := func(address aggregate.DocumentAddress) {
		if input.Selection.Includes(address.Target()) {
			aliases[address] = struct{}{}
		}
	}
	for _, config := range input.Environment.HostConfigs() {
		if !input.Selection.Includes(config.Target()) {
			continue
		}
		if config.Ownership() != hostconfig.OwnershipKeys {
			return readiness.Assessment{}, fmt.Errorf("native document mode and ownership transitions require their containing operation")
		}
		placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{
			Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax(),
		})
		if err != nil {
			return readiness.Assessment{}, err
		}
		address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
		if err != nil {
			return readiness.Assessment{}, err
		}
		add(address)
	}
	for _, contract := range input.Lockfile.Locked.Subjects() {
		if placement, native := contract.HostConfigPlacement(); native && input.Selection.Includes(placement.Target()) {
			if placement.Ownership() != hostconfig.OwnershipKeys {
				return readiness.Assessment{}, fmt.Errorf("native document mode and ownership transitions require their containing operation")
			}
			address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
			if err != nil {
				return readiness.Assessment{}, err
			}
			add(address)
		}
	}
	for _, state := range before.ManagedPaths() {
		if aggregate.IsHostConfigDocumentSubject(state.Subject()) && slices.ContainsFunc(state.ConsumerTargets(), input.Selection.Includes) {
			return readiness.Assessment{}, fmt.Errorf("native whole-document history and ownership transitions require their containing operation")
		}
	}
	for _, state := range before.ManagedAggregates() {
		if _, native := aggregate.HostConfigSyntaxForCodec(state.Contribution().CodecContractID()); native {
			add(state.Contribution().Contract().Address().Document())
		}
	}
	if len(aliases) == 0 {
		if input.Lockfile.Version == lock.NativeVersion || before.ContainsHostConfig() {
			input.Native = &readiness.NativeAssessmentInput{}
		}
		if operation != nil {
			operation.native = input.Native
		}
		return readiness.Assess(ctx, input)
	}

	budget, err := input.PersistenceEpoch.PhysicalBudget()
	if err != nil {
		return readiness.Assessment{}, err
	}
	var roots journal.NativeDocumentRoots
	var retained []*rootedpath.CapturedRoot
	defer func() {
		if resultErr == nil && operation != nil {
			operation.roots = retained
			return
		}
		for _, root := range retained {
			resultErr = errors.Join(resultErr, root.Close())
		}
		if resultErr != nil {
			result = readiness.Assessment{}
		}
	}()
	addresses := make([]aggregate.DocumentAddress, 0, len(aliases))
	for alias := range aliases {
		addresses = append(addresses, alias)
	}
	slices.SortFunc(addresses, func(left, right aggregate.DocumentAddress) int {
		return strings.Compare(string(left.Target())+"\x00"+string(left.Scope())+"\x00"+left.AggregateRoot().String(), string(right.Target())+"\x00"+string(right.Scope())+"\x00"+right.AggregateRoot().String())
	})
	manifestRoot, err := rootedpath.CaptureCanonicalRootBounded(input.Paths.ManifestRoot, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return readiness.Assessment{}, err
	}
	retained = append(retained, manifestRoot)
	roots.Manifest = journal.NativeDocumentRoot{SelectedPath: input.Paths.ManifestRoot, Root: manifestRoot}
	for _, alias := range addresses {
		var selected *journal.NativeDocumentRoot
		var path string
		switch alias.AggregateRoot().RootRole() {
		case output.RootProject:
			selected, path = &roots.Manifest, input.Paths.ManifestRoot
		case output.RootPiAgent:
			selected = &roots.PiAgent
			path, err = pihostpath.ResolveAgentRoot(pihostpath.AgentRootInput{WorkDir: input.Paths.ManifestRoot})
		case output.RootCodexHome:
			selected = &roots.CodexHome
			path, err = codexhostpath.ResolveHomeRoot(input.Paths.ManifestRoot)
		default:
			return readiness.Assessment{}, fmt.Errorf("native document has no selected host root")
		}
		if err != nil {
			return readiness.Assessment{}, err
		}
		if selected.Root != nil {
			continue
		}
		path = filepath.Clean(path)
		selection, err := journal.CaptureNativeDocumentRoot(ctx, path, budget)
		if err != nil {
			return readiness.Assessment{}, fmt.Errorf("observe selected native root %q without creating it: %w", path, err)
		}
		retained = append(retained, selection.Root)
		*selected = selection
	}
	stateAuthority, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(input.Paths.StatefilePath, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return readiness.Assessment{}, err
	}
	owner, err := stateauthority.New(stateAuthority.Exact(), input.Paths.ManifestPath)
	if err != nil {
		return readiness.Assessment{}, err
	}
	reader, err := ownershipstore.NewRecoveryReader(input.Paths.OwnershipRegistryPath)
	if err != nil {
		return readiness.Assessment{}, err
	}
	registry := &observedRegistry{RegistryReader: reader}
	documents, err := journal.ObserveNativeDocuments(ctx, journal.NativeDocumentObservationInput{
		Owner: owner, Before: before, Aliases: addresses, Roots: roots,
		Filesystem: storagecommit.Adapter{}, Registry: registry, Limits: DocumentLimits(), Budget: budget,
	})
	if err != nil {
		return readiness.Assessment{}, err
	}
	input.Native = &readiness.NativeAssessmentInput{
		Documents: &documents, Registry: registry.actual, Owner: owner, Budget: budget,
	}
	if operation != nil {
		operation.native = input.Native
	}
	return readiness.Assess(ctx, input)
}

type observedRegistry struct {
	ownershipmutation.RegistryReader
	actual ownership.Registry
}

func (reader *observedRegistry) LoadForClaimRemovals(ctx context.Context, claims []ownership.Claim, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	registry, err := reader.RegistryReader.LoadForClaimRemovals(ctx, claims, depth, budget)
	if err == nil {
		reader.actual = registry
	}
	return registry, err
}

func (reader *observedRegistry) LoadForDocuments(ctx context.Context, owner stateauthority.Authority, missing []pathauthority.Provisional, depth int, budget rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	qualified, ok := reader.RegistryReader.(ownershipmutation.DocumentRegistryReader)
	if !ok {
		return ownership.Registry{}, fmt.Errorf("selected Registry reader cannot qualify missing native history")
	}
	registry, err := qualified.LoadForDocuments(ctx, owner, missing, depth, budget)
	if err == nil {
		reader.actual = registry
	}
	return registry, err
}
