package authoring

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	declarationedit "github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/desired/entity"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

type agentConfigReleaseFile struct {
	role    journalrecord.NativeReleaseFileRole
	path    string
	content []byte
	after   []byte
	mode    os.FileMode
	exists  bool
	write   bool
}

type agentConfigReleaseCandidate struct {
	paths       daempaths.Paths
	request     UnmanageAgentConfigRequest
	files       []agentConfigReleaseFile
	before      durable.Snapshot
	after       durable.Snapshot
	codec       durable.SnapshotCodec
	owner       stateauthority.Authority
	management  *journaldocument.AgentConfigManagement
	destination output.Destination
	selected    target.Target
	scope       target.Scope
	namespace   journalrecord.NativeReleaseNamespace
	// Nil is admitted only for a proved declaration-only transition that has
	// neither a document namespace nor an own metadata or membership delta.
	registryScope   *ownership.DocumentScope
	exclusion       mutation.DirectoryEntryAuthorityObservation
	claims          []ownership.Claim
	binding         ownership.DocumentBindingValue
	manifestChanged bool
	registryChanged bool
}

func readAgentConfigReleaseFile(ctx context.Context, selected string, role journalrecord.NativeReleaseFileRole, budget *physicalwork.Budget) (agentConfigReleaseFile, error) {
	path, err := mutation.CanonicalDirectoryEntryPathBounded(selected, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return agentConfigReleaseFile{}, err
	}
	if role != journalrecord.NativeReleaseState {
		target, err := declarationartifact.CaptureMutationTarget(ctx, selected)
		if err != nil {
			return agentConfigReleaseFile{}, err
		}
		path = target.Path()
	}
	file := agentConfigReleaseFile{path: path, role: role}
	content, err := declarationartifact.Read(ctx, path)
	if errors.Is(err, os.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return agentConfigReleaseFile{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return agentConfigReleaseFile{}, err
	}
	if !info.Mode().IsRegular() {
		return agentConfigReleaseFile{}, fmt.Errorf("release control target is not a regular file")
	}
	if err := budget.AdmitPhysicalWork(0, 1, int64(len(content))); err != nil {
		return agentConfigReleaseFile{}, err
	}
	file.content, file.after, file.mode, file.exists = content, content, info.Mode().Perm(), true
	return file, nil
}

func buildAgentConfigRelease(ctx context.Context, paths daempaths.Paths, request UnmanageAgentConfigRequest, contributions aggregate.ContributionCatalog, budget *physicalwork.Budget) (agentConfigReleaseCandidate, error) {
	candidate := agentConfigReleaseCandidate{paths: paths, request: request}
	for index, path := range []string{paths.ManifestPath, paths.LockfilePath, paths.StatefilePath} {
		roles := []journalrecord.NativeReleaseFileRole{journalrecord.NativeReleaseManifest, journalrecord.NativeReleaseDefaultLock, journalrecord.NativeReleaseState}
		file, err := readAgentConfigReleaseFile(ctx, path, roles[index], budget)
		if err != nil {
			return agentConfigReleaseCandidate{}, err
		}
		candidate.files = append(candidate.files, file)
	}
	codec, err := statefile.NewNativeCodec(contributions)
	if err != nil {
		return agentConfigReleaseCandidate{}, err
	}
	candidate.codec = codec
	physical, err := statefile.NewPhysicalCodec(candidate.codec)
	if err != nil {
		return agentConfigReleaseCandidate{}, err
	}
	state, err := statefile.LoadOptionalPhysicalWithOptions(ctx, paths.StatefilePath, statefile.ReadOptions{Codec: physical, PhysicalPathBudget: budget})
	if err != nil {
		return agentConfigReleaseCandidate{}, err
	}
	candidate.before, candidate.after = state.Snapshot(), state.Snapshot()
	id, _ := entity.New(entity.KindAgentConfig, request.ID)
	identities, err := journaldocument.AgentConfigManagements(ctx, candidate.before, id)
	if err != nil {
		return agentConfigReleaseCandidate{}, err
	}
	var selected []journaldocument.AgentConfigManagement
	for _, identity := range identities {
		if (request.Target == "" || identity.Target() == request.Target) && (request.Scope == "" || identity.Scope() == request.Scope) {
			selected = append(selected, identity)
		}
	}
	if len(selected) > 1 {
		return agentConfigReleaseCandidate{}, fmt.Errorf("%w: use --target/--scope", ErrAgentConfigManagementAmbiguous)
	}
	if len(selected) == 1 {
		candidate.management = &selected[0]
		candidate.selected, candidate.scope, candidate.destination = selected[0].Target(), selected[0].Scope(), selected[0].Destination()
		candidate.after, err = selected[0].RemoveFrom(ctx, candidate.before)
		if err != nil {
			return agentConfigReleaseCandidate{}, err
		}
	} else if len(identities) != 0 {
		return agentConfigReleaseCandidate{}, ErrAgentConfigManagementNotFound
	}
	var declaration *agentconfig.Config
	if candidate.files[0].exists {
		environment, err := declarationmanifest.Decode(candidate.files[0].content)
		if err != nil {
			return agentConfigReleaseCandidate{}, err
		}
		for _, config := range environment.AgentConfigs() {
			if config.ID() != id {
				continue
			}
			if (request.Target != "" && config.Target() != request.Target) || (request.Scope != "" && config.Scope() != request.Scope) {
				return agentConfigReleaseCandidate{}, ErrAgentConfigManagementNotFound
			}
			placement, err := profile.ResolveAgentConfigPlacement(configuration.PlacementInput{Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax()})
			if err != nil {
				return agentConfigReleaseCandidate{}, err
			}
			if candidate.management != nil && (candidate.selected != config.Target() || candidate.scope != config.Scope() || candidate.destination != placement.Destination()) {
				return agentConfigReleaseCandidate{}, fmt.Errorf("agent configuration declaration conflicts with retained management identity")
			}
			candidate.selected, candidate.scope, candidate.destination = config.Target(), config.Scope(), placement.Destination()
			declaration = &config
		}
	}
	if declaration == nil && candidate.management == nil {
		return agentConfigReleaseCandidate{}, ErrAgentConfigManagementNotFound
	}
	if declaration != nil {
		blocks, err := declarationcodec.ScanAgentConfigBlocks(candidate.files[0].content)
		if err != nil {
			return agentConfigReleaseCandidate{}, err
		}
		removed := 0
		for _, block := range blocks {
			if block.AgentConfig.ID != request.ID {
				continue
			}
			candidate.files[0].after = declarationedit.RemoveDocumentRange(candidate.files[0].content, declarationedit.DocumentRange{Start: block.Start, End: block.End})
			removed++
			candidate.files[0].write = !bytes.Equal(candidate.files[0].content, candidate.files[0].after)
			candidate.manifestChanged = candidate.files[0].write
		}
		if removed != 1 {
			return agentConfigReleaseCandidate{}, fmt.Errorf("agent configuration declaration cannot be removed automatically: use [[host_config]] blocks or remove the declaration manually, then rerun unmanage agent-config")
		}
		remaining, err := declarationmanifest.Decode(candidate.files[0].after)
		if err != nil {
			return agentConfigReleaseCandidate{}, err
		}
		for _, config := range remaining.AgentConfigs() {
			if config.ID() == id {
				return agentConfigReleaseCandidate{}, fmt.Errorf("agent configuration declaration remains after its source edit")
			}
		}
	}
	if candidate.files[1].exists {
		codec, err := lockfile.NewCodec(contributions)
		if err != nil {
			return agentConfigReleaseCandidate{}, err
		}
		locked, err := codec.Decode(ctx, candidate.files[1].content)
		if err != nil {
			return agentConfigReleaseCandidate{}, err
		}
		if source, found := locked.Locked.ExactSupplySubject(id); found {
			placement, present := source.AgentConfigPlacement()
			if !present || placement.Target() != candidate.selected || placement.Scope() != candidate.scope || placement.Destination() != candidate.destination {
				return agentConfigReleaseCandidate{}, fmt.Errorf("lock entry conflicts with the selected agent configuration")
			}
		}
		next, changed, err := locked.WithoutAgentConfig(id)
		if err != nil {
			return agentConfigReleaseCandidate{}, err
		}
		if changed {
			candidate.files[1].after, err = codec.Marshal(ctx, next)
			if err != nil {
				return agentConfigReleaseCandidate{}, err
			}
			candidate.files[1].write = true
		}
	}
	if candidate.management != nil {
		candidate.files[2].after, err = state.Codec().EncodeState(candidate.after)
		if err != nil {
			return agentConfigReleaseCandidate{}, err
		}
		candidate.files[2].write = true
	}
	registry, err := ownershipstore.ReadProtectionRegistryBounded(ctx, paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return agentConfigReleaseCandidate{}, err
	}
	if err := candidate.qualifyMetadata(ctx, registry, budget); err != nil {
		return agentConfigReleaseCandidate{}, err
	}
	return candidate, ctx.Err()
}

func (candidate agentConfigReleaseCandidate) same(other agentConfigReleaseCandidate) bool {
	if (candidate.registryScope == nil) != (other.registryScope == nil) {
		return false
	}
	if candidate.registryScope != nil && !candidate.registryScope.Equal(*other.registryScope) {
		return false
	}
	if candidate.owner != other.owner || candidate.destination != other.destination || candidate.selected != other.selected || candidate.scope != other.scope || !candidate.before.Equal(other.before) || !candidate.after.Equal(other.after) || !candidate.binding.Equal(other.binding) || !slices.EqualFunc(candidate.claims, other.claims, ownership.Claim.Equal) {
		return false
	}
	return slices.EqualFunc(candidate.files, other.files, func(left, right agentConfigReleaseFile) bool {
		return left.role == right.role && left.path == right.path && left.mode == right.mode && left.exists == right.exists && left.write == right.write && bytes.Equal(left.content, right.content) && bytes.Equal(left.after, right.after)
	})
}
