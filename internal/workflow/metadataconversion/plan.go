package metadataconversion

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/target"
)

type compilerInput struct {
	Paths              daempaths.Paths
	References         []pathauthority.Reference
	PhysicalWorkBudget fileset.PhysicalWorkBudget
}

// Plan retains exact image facts and lazy materializers, not participant payloads.
type Plan struct {
	paths        daempaths.Paths
	selection    selection
	intent       fileset.PublicationIntent
	materialize  map[string]fileset.PublicationMaterializer
	participants []string
	budget       fileset.PhysicalWorkBudget
	changed      bool
	barrier      recoverygate.MetadataBarrier
	authority    recoverygate.EffectAuthority
}

func (plan Plan) Changed() bool { return plan.changed }

func (plan Plan) ParticipantCount() int { return len(plan.participants) }

type planCompiler struct {
	input            compilerInput
	read             sourceReader
	qualify          *referenceQualification
	owners           map[pathauthority.Exact]*ownerCandidates
	documents        map[pathauthority.Exact][]lookupCandidate
	bindings         []ownership.DocumentBinding
	claims           []ownership.Claim
	compiled         []compiledTarget
	participants     []string
	required         []pathauthority.Exact
	barrierDirs      []string
	selectedIdentity pathauthority.Exact
}

func compilePlan(ctx context.Context, input compilerInput, read sourceReader, retained *selection) (Plan, error) {
	if ctx == nil || input.PhysicalWorkBudget == nil || read == nil {
		return Plan{}, fmt.Errorf("metadata conversion requires context, source reader and shared physical work budget")
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	evidenceRoot, err := mutation.CanonicalDirectoryEntryPathBounded(input.Paths.GlobalMetadataDir(), mutationfs.MaximumPhysicalPathDepth, input.PhysicalWorkBudget)
	if err != nil {
		return Plan{}, err
	}
	input.Paths, err = input.Paths.WithDataDir(filepath.Dir(evidenceRoot))
	if err != nil {
		return Plan{}, err
	}
	statePath, err := mutation.CanonicalDirectoryEntryPathBounded(input.Paths.StatefilePath, mutationfs.MaximumPhysicalPathDepth, input.PhysicalWorkBudget)
	if err != nil {
		return Plan{}, err
	}
	if retained != nil && (retained.manifestPath != input.Paths.ManifestPath || retained.dataDir != input.Paths.DataDir || retained.statefilePath != statePath) {
		return Plan{}, fmt.Errorf("metadata recovery requires the original manifest, State namespace and DataDir selection")
	}
	outputPath, err := mutation.CanonicalDirectoryEntryPathBounded(input.Paths.OwnershipRegistryPath, mutationfs.MaximumPhysicalPathDepth, input.PhysicalWorkBudget)
	if err != nil {
		return Plan{}, err
	}
	carrierPath, err := mutation.CanonicalDirectoryEntryPathBounded(input.Paths.CarrierClaimRegistryPath, mutationfs.MaximumPhysicalPathDepth, input.PhysicalWorkBudget)
	if err != nil {
		return Plan{}, err
	}
	outputs, err := read(ctx, outputPath, ownershipSource)
	if err != nil {
		return Plan{}, fmt.Errorf("ownership registry %q: %w", outputPath, err)
	}
	outputFacts, outputReferences, err := ownershipFacts(outputs)
	if err != nil {
		return Plan{}, err
	}
	carriers, err := read(ctx, carrierPath, carrierSource)
	if err != nil {
		return Plan{}, fmt.Errorf("carrier registry %q: %w", carrierPath, err)
	}
	carrierFacts, carrierReferences, err := carrierFacts(carriers)
	if err != nil {
		return Plan{}, err
	}
	if err := requireSettledRegistryFacts(outputFacts, carrierFacts); err != nil {
		return Plan{}, err
	}
	recorded := append(outputReferences, carrierReferences...)
	if retained != nil {
		recorded = append(recorded, retained.references.References()...)
	}
	qualify, err := newReferenceQualification(input.PhysicalWorkBudget, recorded, input.References)
	if err != nil {
		return Plan{}, err
	}
	compiler := &planCompiler{
		input: input, read: read, qualify: qualify, owners: registryOwners(outputFacts, carrierFacts),
		documents: registryDocuments(outputFacts), bindings: outputFacts.DocumentBindings(), claims: outputFacts.Claims(),
	}
	compiler.required = append(ownershipstore.RequiredReferenceIdentities(outputFacts), carrierclaim.RequiredReferenceIdentities(carrierFacts)...)
	compiler.barrierDirs = append(compiler.barrierDirs, input.Paths.StateDir)
	if input.Paths.LegacyUserStateDir != "" {
		compiler.barrierDirs = append(compiler.barrierDirs, input.Paths.LegacyUserStateDir)
	}
	if err := compiler.addSelected(ctx, statePath); err != nil {
		return Plan{}, err
	}
	identities := make([]pathauthority.Exact, 0, len(compiler.owners))
	for identity := range compiler.owners {
		identities = append(identities, identity)
	}
	slices.SortFunc(identities, pathauthority.Exact.Compare)
	for _, identity := range identities {
		owner := compiler.owners[identity]
		if len(owner.paths) == 0 {
			owner.discover(input.Paths)
		}
		reference, err := qualify.require(ctx, identity, owner.paths)
		if err != nil {
			return Plan{}, fmt.Errorf("metadata participant %q: %w", owner.owners[0].ManifestPath(), err)
		}
		if err := owner.qualifyContexts(ctx, qualify, reference); err != nil {
			return Plan{}, err
		}
		compiler.barrierDirs = append(compiler.barrierDirs, owner.barrierDirs...)
		if identity.Equal(compiler.selectedIdentity) {
			owner.contexts = []daempaths.Paths{input.Paths}
		}
		namespace, err := mutation.CanonicalDirectoryEntryPathBounded(reference.Locator(), mutationfs.MaximumPhysicalPathDepth, input.PhysicalWorkBudget)
		if err != nil {
			return Plan{}, err
		}
		if err := compiler.addState(ctx, namespace, reference, owner.contexts); err != nil {
			return Plan{}, fmt.Errorf("metadata participant %q at %q: %w", owner.owners[0].ManifestPath(), reference.Locator(), err)
		}
	}
	for _, binding := range compiler.bindings {
		if qualify.hasReference(binding.PathAuthority()) {
			continue
		}
		owner := compiler.owners[binding.Owner().StatefileAuthority()]
		if owner == nil {
			return Plan{}, fmt.Errorf("document binding lacks a participating owner")
		}
		for _, paths := range owner.contexts {
			candidates, err := destinationCandidates(paths, binding.Destination(), binding.PathAuthority(), input.PhysicalWorkBudget)
			if err != nil {
				return Plan{}, err
			}
			compiler.documents[binding.PathAuthority()] = append(compiler.documents[binding.PathAuthority()], candidates...)
		}
	}
	for _, identity := range compiler.required {
		if _, err := qualify.require(ctx, identity, compiler.documents[identity]); err != nil {
			return Plan{}, err
		}
	}
	references := make([]pathauthority.Reference, 0, len(qualify.found))
	for _, reference := range qualify.found {
		references = append(references, reference)
	}
	table, err := pathauthority.NewReferenceTable(compiler.required, references)
	if err != nil {
		return Plan{}, err
	}
	for identity := range qualify.fixed {
		if _, present := table.Reference(identity); !present {
			return Plan{}, fmt.Errorf("supplied reference %q is outside the participating closure", identity.Key())
		}
	}
	slices.Sort(compiler.barrierDirs)
	compiler.barrierDirs = slices.Compact(compiler.barrierDirs)
	selected := selection{manifestPath: input.Paths.ManifestPath, statefilePath: statePath, dataDir: input.Paths.DataDir, references: table, barrierDirs: compiler.barrierDirs}
	if retained != nil {
		before, err := retained.encode()
		if err != nil {
			return Plan{}, err
		}
		after, err := selected.encode()
		if err != nil || string(before) != string(after) {
			return Plan{}, fmt.Errorf("metadata recovery selection membership changed")
		}
	}
	for _, source := range []struct {
		path  string
		kind  sourceKind
		entry sourceEntry
	}{{outputPath, ownershipSource, outputs}, {carrierPath, carrierSource, carriers}} {
		compiled, err := compileRegistry(ctx, source.path, source.kind, source.entry, table)
		if err != nil {
			return Plan{}, err
		}
		compiler.compiled = append(compiler.compiled, compiled)
	}
	if err := compiler.requireMetadataDocumentSeparation(ctx, table, evidenceRoot); err != nil {
		return Plan{}, err
	}
	return compiler.finish(selected)
}

func (compiler *planCompiler) addSelected(ctx context.Context, path string) error {
	observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(path, mutationfs.MaximumPhysicalPathDepth, compiler.input.PhysicalWorkBudget)
	if err != nil {
		return err
	}
	if identity, exact := observation.Exact(); exact {
		candidates := compiler.owners[identity]
		if candidates != nil {
			candidates.discover(compiler.input.Paths)
			if _, err := compiler.qualify.require(ctx, identity, candidates.paths); err != nil {
				return fmt.Errorf("selected namespace collides with an unqualified recorded owner: %w", err)
			}
		}
		reference, err := compiler.qualify.selected(ctx, path)
		if err != nil {
			return err
		}
		if !reference.Identity().Equal(identity) {
			return mutation.StaleSnapshotError{}
		}
		owner, err := stateauthority.New(identity, compiler.input.Paths.ManifestPath)
		if err != nil {
			return err
		}
		if candidates == nil {
			candidates = &ownerCandidates{paths: []lookupCandidate{{path: path, origin: lookupRecordedRoot}}}
		}
		candidates.owners = append(candidates.owners, owner)
		candidates.contexts = append(candidates.contexts, compiler.input.Paths)
		compiler.owners[identity] = candidates
		compiler.selectedIdentity = identity
		compiler.required = append(compiler.required, reference.Identity())
		return nil
	}
	entry, err := compiler.read(ctx, path, stateSource)
	if err != nil {
		return err
	}
	if entry.image.Exists() {
		return fmt.Errorf("existing selected State lacks exact namespace authority")
	}
	compiled, err := compileState(ctx, path, entry, pathauthority.Reference{})
	if err != nil {
		return err
	}
	compiler.compiled = append(compiler.compiled, compiled)
	compiler.participants = append(compiler.participants, path)
	compiler.barrierDirs = append(compiler.barrierDirs, filepath.Dir(path))
	return nil
}

func (compiler *planCompiler) addState(ctx context.Context, path string, reference pathauthority.Reference, contexts []daempaths.Paths) error {
	entry, err := compiler.read(ctx, path, stateSource)
	if err != nil {
		return err
	}
	var snapshot durable.Snapshot
	if entry.image.Exists() {
		var recorded pathauthority.Reference
		snapshot, recorded, err = stateFacts(ctx, entry.content)
		if err != nil {
			return err
		}
		if !recorded.IsZero() && recorded.Identity().Equal(reference.Identity()) {
			reference, err = compiler.qualify.retainRecorded(ctx, recorded)
			if err != nil {
				return err
			}
		}
	}
	compiled, err := compileState(ctx, path, entry, reference)
	if err != nil {
		return err
	}
	compiler.compiled = append(compiler.compiled, compiled)
	compiler.participants = append(compiler.participants, path)
	compiler.barrierDirs = append(compiler.barrierDirs, filepath.Dir(path))
	compiler.required = append(compiler.required, reference.Identity())
	if !entry.image.Exists() {
		return nil
	}
	var destinations []output.Destination
	for _, state := range snapshot.ManagedPaths() {
		if state.Scope() == target.ScopeGlobal {
			destinations = append(destinations, state.Destination())
		}
	}
	for _, state := range snapshot.ManagedAggregates() {
		if state.Contribution().Scope() == target.ScopeGlobal {
			destinations = append(destinations, state.Contribution().AggregateRoot())
		}
	}
	for _, paths := range contexts {
		for _, destination := range destinations {
			for _, claim := range compiler.claims {
				if claim.Owner().StatefileAuthority().Equal(reference.Identity()) {
					identity := claim.Address().PathAuthority()
					if compiler.qualify.hasReference(identity) {
						continue
					}
					candidates, err := destinationCandidates(paths, destination, identity, compiler.input.PhysicalWorkBudget)
					if err != nil {
						return err
					}
					compiler.documents[identity] = append(compiler.documents[identity], candidates...)
				}
			}
		}
	}
	return nil
}

func (compiler *planCompiler) finish(selected selection) (Plan, error) {
	content, err := selected.encode()
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{
		paths: compiler.input.Paths, selection: selected, participants: slices.Clone(compiler.participants),
		materialize: make(map[string]fileset.PublicationMaterializer), budget: compiler.input.PhysicalWorkBudget,
	}
	targets := make([]fileset.PublicationTarget, 0, len(compiler.compiled))
	for _, compiled := range compiler.compiled {
		targets = append(targets, compiled.target)
		if compiled.target.Writes() {
			plan.changed = true
			plan.materialize[compiled.target.Path()] = compiled.materialize
		}
	}
	plan.intent, err = fileset.NewPublicationIntent(content, targets)
	return plan, err
}

func requireSettledRegistryFacts(outputs ownership.Registry, carriers durablecarrier.GlobalCarrierClaims) error {
	if len(outputs.PendingProtections()) != 0 {
		return fmt.Errorf("pending document protection requires its original operation recovery before metadata conversion")
	}
	for _, claim := range outputs.Claims() {
		if claim.State() == ownership.ClaimReserved {
			return fmt.Errorf("reserved ownership for %q requires original operation %q recovery", claim.Owner().ManifestPath(), claim.OperationID())
		}
	}
	for _, claim := range carriers.Claims() {
		if err := claim.RequireStable(); err != nil {
			return fmt.Errorf("carrier owner %q: %w", claim.Owner().ManifestPath(), err)
		}
	}
	return nil
}

func registryOwners(outputs ownership.Registry, carriers durablecarrier.GlobalCarrierClaims) map[pathauthority.Exact]*ownerCandidates {
	owners := make(map[pathauthority.Exact]*ownerCandidates)
	add := func(authority stateauthority.Authority) {
		identity := authority.StatefileAuthority()
		owner := owners[identity]
		if owner == nil {
			owner = &ownerCandidates{}
			owners[identity] = owner
		}
		owner.owners = append(owner.owners, authority)
	}
	for _, claim := range outputs.Claims() {
		add(claim.Owner())
	}
	for _, binding := range outputs.DocumentBindings() {
		add(binding.Owner())
	}
	for _, protection := range outputs.PendingProtections() {
		add(protection.Owner())
		for _, document := range protection.Documents() {
			for _, binding := range document.Bindings() {
				add(binding.Owner())
			}
		}
	}
	for _, claim := range carriers.Claims() {
		add(claim.Owner())
	}
	return owners
}
