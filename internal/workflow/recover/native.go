package recover

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/journal/removalnamespace"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/output"
	codexhostpath "github.com/isty2e/daem/internal/output/hostpath/codex"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

func recoveryLoadOptions(ctx context.Context, paths daempaths.Paths, filesystem mutationfs.Reader, physicalBudget rootedpath.PhysicalTraversalBudget) (journal.PlanLoadOptions, error) {
	registry, err := ownershipstore.NewRecoveryReader(paths.OwnershipRegistryPath)
	if err != nil {
		return journal.PlanLoadOptions{}, err
	}
	codecs, err := nativeconfig.Codecs(ctx)
	if err != nil {
		return journal.PlanLoadOptions{}, err
	}
	stateCodec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		return journal.PlanLoadOptions{}, err
	}
	budget, _ := physicalBudget.(*physicalwork.Budget)
	profile := &journal.Profile{
		StateCodec: stateCodec, Codecs: codecs, Limits: nativeconfig.DocumentLimits(),
		Manifest: paths.ManifestRoot, ManifestPath: paths.ManifestPath, Budget: budget,
		ResolveRoot: func(role output.RootRole) (string, error) {
			switch role {
			case output.RootPiAgent:
				return pihostpath.ResolveAgentRoot(pihostpath.AgentRootInput{WorkDir: paths.ManifestRoot})
			case output.RootCodexHome:
				return codexhostpath.ResolveHomeRoot(paths.ManifestRoot)
			default:
				return "", fmt.Errorf("native recovery root role is not selected")
			}
		},
		StateReader: func(ctx context.Context) (durable.Snapshot, error) {
			return statefile.LoadOptionalWithOptions(ctx, paths.StatefilePath, statefile.ReadOptions{Codec: stateCodec, PhysicalPathBudget: budget})
		},
	}
	return journal.PlanLoadOptions{
		Filesystem: filesystem, PhysicalPathBudget: physicalBudget, Resolver: destinationResolver(paths).Resolve,
		OwnershipRegistry: registry, Codecs: aggregatecodec.Catalog(), StateCodec: statefile.Codec{}, StateReader: stateReaderForPath(paths.StatefilePath), NativeDocuments: profile,
	}, nil
}

func recoveryInventoryProfile(ctx context.Context) (journal.RecoveryInventoryProfile, error) {
	codecs, err := nativeconfig.Codecs(ctx)
	if err != nil {
		return journal.RecoveryInventoryProfile{}, err
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		return journal.RecoveryInventoryProfile{}, err
	}
	return journal.NewNativeRecoveryInventoryProfile(statefile.Codec{}, codec)
}

func nativeRecoveryOperationFingerprint(paths daempaths.Paths, plan journal.NativeRecoveryPlan, stateDir recoverygate.StateDirAuthority) (mutation.OperationFingerprint, error) {
	stateFingerprint, err := stateDir.IdentityFingerprint()
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	return mutation.NewOperationFingerprint([]byte(paths.ManifestRoot + "\x00" + paths.StatefilePath + "\x00" + paths.RecoveryDir + "\x00" + plan.OperationID() + "\x00" + plan.ObservationFingerprint() + "\x00" + stateFingerprint)), nil
}

func buildNativeRecoveryAuthorityEvidence(paths daempaths.Paths, plan journal.NativeRecoveryPlan, stateDir recoverygate.StateDirAuthority) (recoveryAuthorityEvidence, error) {
	builder := operationplan.NewBuilder(operationplan.RevisionsOff, nil, 0)
	stateFingerprint, err := stateDir.IdentityFingerprint()
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	if err := builder.AddFingerprintOnly(operationplan.FactStateDirIdentity, paths.StateDir, mutation.AccessShared, mutation.PathEffectDirectoryEntry, stateFingerprint); err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	for _, path := range []string{paths.RecoveryDir, plan.OperationDir(), paths.StatefilePath, paths.OwnershipRegistryPath} {
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			if err := builder.AddLogical(path, mutation.AccessExclusive, effect); err != nil {
				return recoveryAuthorityEvidence{}, err
			}
		}
	}
	for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
		if err := builder.AddLogical(paths.StateDir, mutation.AccessShared, effect); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
	}
	resolver := destinationResolver(paths)
	writeAliases, err := plan.DocumentWriteAliases()
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	writes := make(map[aggregate.DocumentAddress]struct{})
	for _, group := range writeAliases {
		for _, alias := range group {
			writes[alias] = struct{}{}
		}
	}
	aliases, err := plan.DocumentAliases()
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	for _, group := range aliases {
		for _, alias := range group {
			path, err := resolver.Resolve(alias.AggregateRoot())
			if err != nil {
				return recoveryAuthorityEvidence{}, err
			}
			if _, write := writes[alias]; write {
				if err := builder.AddDocumentWritePair(path, string(alias.Target()), string(alias.Scope())); err != nil {
					return recoveryAuthorityEvidence{}, err
				}
			} else if err := builder.AddPhysicalPair(path, mutation.AccessExclusive, string(alias.Target()), string(alias.Scope())); err != nil {
				return recoveryAuthorityEvidence{}, err
			}
		}
	}
	for _, action := range plan.GuardedOrdinaryActions() {
		destination, err := output.Parse(action.Destination)
		if err != nil {
			return recoveryAuthorityEvidence{}, err
		}
		path, err := resolver.Resolve(destination)
		if err != nil {
			return recoveryAuthorityEvidence{}, err
		}
		targets := action.ConsumerTargets
		if action.Target != "" {
			targets = []target.Target{action.Target}
		}
		for _, selectedTarget := range targets {
			if err := builder.AddDocumentWritePair(path, string(selectedTarget), string(action.Scope)); err != nil {
				return recoveryAuthorityEvidence{}, err
			}
		}
	}
	intents, err := plan.RemovalIntents()
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	for _, intent := range intents {
		residue, cleanup, err := removalnamespace.Paths(intent.Namespace())
		if err != nil {
			return recoveryAuthorityEvidence{}, err
		}
		for _, path := range []string{residue, cleanup} {
			for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
				if err := builder.AddRemovalContinuation(path, effect); err != nil {
					return recoveryAuthorityEvidence{}, err
				}
			}
		}
		if err := builder.AddFingerprintOnly(operationplan.FactRemovalParentIdentity, filepath.Dir(residue), mutation.AccessExclusive, mutation.PathEffectDirectoryEntry, ""); err != nil {
			return recoveryAuthorityEvidence{}, err
		}
	}
	compiled := builder.Compile()
	documentWrites, err := mutation.NewPhysicalAuthoritySet(compiled.DocumentWriteRequests()...)
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	domains, err := lowerRecoveryAuthorityDomainSteps(compiled.DomainSteps())
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	fingerprint, err := operationplan.RecoverAuthorityFingerprint(compiled)
	if err != nil {
		return recoveryAuthorityEvidence{}, err
	}
	return recoveryAuthorityEvidence{domains: domains, documentWrites: documentWrites, authorityFingerprint: fingerprint}, nil
}
