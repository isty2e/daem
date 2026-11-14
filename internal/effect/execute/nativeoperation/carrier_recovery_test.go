package nativeoperation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

type carrierRecoveryFixture struct {
	journal                  journalrecord.Native
	records                  journalrecord.LoadedDocuments
	read                     journalqualification.ReadOptions
	profile                  journal.Profile
	registryPath             string
	recoveryDir              string
	statePath                string
	source, target           string
	sourceIndex, targetIndex int
	beforeRegistry, prepared ownership.Registry
	comparisons              []journaldocument.Comparison
}

type carrierRecoveryShape uint8

const (
	carrierTransfer carrierRecoveryShape = iota
	carrierRetainedSource
	carrierSourceRetirement
	carrierCoalesced
)

func newCarrierRecoveryFixture(t *testing.T, bound, reverse bool, shape carrierRecoveryShape) *carrierRecoveryFixture {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest := carrierRecoveryRoot(t, base)
	const sourceDirectory = "source-Cafe\u0301"
	for _, name := range []string{sourceDirectory, "target"} {
		if err := os.Mkdir(filepath.Join(base, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f := &carrierRecoveryFixture{source: filepath.Join(base, sourceDirectory, ".claude.json"), target: filepath.Join(base, "target", ".config.json"), statePath: filepath.Join(base, "state.json"), registryPath: filepath.Join(base, "ownership.json"), recoveryDir: filepath.Join(base, "recovery")}
	if err := os.Mkdir(f.recoveryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	originals := []aggregate.Document{aggregate.ExistingDocument([]byte(`{"preserved":1,"mcpServers":{"search":{"type":"stdio","command":"npx"}}}`)), aggregate.ExistingDocument([]byte(`{"peer":2}`))}
	if shape == carrierRetainedSource || shape == carrierCoalesced {
		originals[0] = aggregate.ExistingDocument([]byte(`{"preserved":1,"mcpServers":{"search":{"type":"stdio","command":"npx"},"remove":{"type":"stdio","command":"npx"}}}`))
	}
	for index, path := range []string{f.source, f.target} {
		if err := os.WriteFile(path, originals[index].Content(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if shape == carrierCoalesced {
		if err := os.Remove(f.target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(f.source, f.target); err != nil {
			t.Skip("symlink creation is unavailable", err)
		}
		originals[1] = originals[0]
	}
	budget := carrierRecoveryBudget(t)
	stateObservation, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(f.statePath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(stateObservation.Exact(), filepath.Join(manifest.SelectedPath, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	placement, _ := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, target.ScopeGlobal)
	contract, err := placement.ProjectionContract("search")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	codecs := aggregatecodec.Catalog()
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	physicalCodec, err := statefile.NewPhysicalCodec(codec)
	if err != nil {
		t.Fatal(err)
	}
	projectionCodec, _ := codecs.Lookup(contract.CodecContractID())
	row, _ := carrierMCPRow(t, originals[0], "search", codecs)
	subject := row.Subject()
	beforeRows := []durable.ManagedAggregateState{row}
	sourceSubjects, sourceSelection := []topology.SubjectID{subject}, selection
	if shape == carrierRetainedSource || shape == carrierCoalesced {
		removed, selected := carrierMCPRow(t, originals[0], "remove", codecs)
		beforeRows = append(beforeRows, removed)
		if shape == carrierRetainedSource {
			sourceSubjects, sourceSelection = []topology.SubjectID{removed.Subject()}, selected
		} else {
			sourceSubjects = append(sourceSubjects, removed.Subject())
			sourceSelection, err = aggregate.NewSelection(append(selection.Contracts(), selected.Contracts()...))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: beforeRows})
	if err != nil {
		t.Fatal(err)
	}
	after, err := before.AdvanceNativeDocumentCommit()
	afterRow := row
	switch shape {
	case carrierRetainedSource:
		after, err = durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: []durable.ManagedAggregateState{row}})
	case carrierSourceRetirement:
		after, err = durable.NewSnapshot(durable.SnapshotInput{})
	case carrierCoalesced:
		afterRow, _ = carrierMCPRow(t, aggregate.ExistingDocument([]byte(`{"mcpServers":{"search":{"type":"stdio","command":"node"}}}`)), "search", codecs)
		after, err = durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: []durable.ManagedAggregateState{afterRow}})
	}
	if err != nil {
		t.Fatal(err)
	}
	alias := contract.Address().Document()
	input := configuration.AliasInput{Target: alias.Target(), Scope: alias.Scope(), Destination: alias.AggregateRoot()}
	sourceBinding, err := configuration.OwnedMCPSourceBinding(input)
	if err != nil {
		t.Fatal(err)
	}
	currentBinding, err := configuration.RootBindingForAlias(input)
	if err != nil {
		t.Fatal(err)
	}
	sourceObservation, err := mutation.ObserveDirectoryEntryAuthorityBounded(f.source, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	sourceExact, _ := sourceObservation.Exact()
	sourceRoot, err := journalnamespace.CaptureOwnedEndpointRoot(t.Context(), f.source, sourceExact, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sourceRoot.Close() })
	targetRoot, err := journalnamespace.CaptureEndpointRoot(t.Context(), [2]string{f.target, f.target + ".fallback"}, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = targetRoot.Close() })
	manifestReference, err := output.NewRootReference(configuration.ProjectRootIdentity, "selected-project-v1")
	if err != nil {
		t.Fatal(err)
	}
	roots, err := (journalnamespace.Roots{}).With(manifestReference, manifest)
	if err != nil {
		t.Fatal(err)
	}
	for reference, root := range map[output.RootReference]journalnamespace.RootSelection{sourceBinding.RootReference(): sourceRoot, currentBinding.RootReference(): targetRoot} {
		roots, err = roots.With(reference, root)
		if err != nil {
			t.Fatal(err)
		}
	}
	grants, err := roots.Qualify(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	sourceGrant, _ := grants.Selection(sourceBinding.RootReference())
	pin, err := journalnamespace.NewBoundWriterPin(sourceBinding.RootReference(), sourceGrant.Provenance(), sourceBinding.Coordinate())
	if err != nil {
		t.Fatal(err)
	}
	sourceAliases := []journalnamespace.RecordedAlias{{Address: alias, Binding: sourceBinding}}
	if shape == carrierCoalesced {
		sourceAliases = append(sourceAliases, journalnamespace.RecordedAlias{Address: alias, Binding: currentBinding})
	}
	sourceNamespace, err := journalnamespace.Reobserve(t.Context(), sourceAliases, roots, namespacetest.Reservations(t), testFilesystem(), budget, pin)
	if err != nil {
		t.Fatal(err)
	}
	targetNamespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, namespacetest.Reservations(t), testFilesystem(), budget)
	if err != nil {
		t.Fatal(err)
	}
	targetExact, _ := targetNamespace.Observation().Exact()
	claims := make([]ownership.Claim, 2)
	for index, exact := range []pathauthority.Exact{sourceExact, targetExact} {
		address, err := ownership.NewManagedAddress(exact, contract.Address().Footprint())
		if err != nil {
			t.Fatal(err)
		}
		claims[index], err = ownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatal(err)
		}
	}
	retained, err := ownership.NewDocumentBinding(owner, alias.AggregateRoot(), sourceExact)
	if err != nil {
		t.Fatal(err)
	}
	beforeBinding := ownership.NoDocumentBinding()
	var bindings []ownership.DocumentBinding
	if bound {
		beforeBinding, _ = ownership.PresentDocumentBinding(retained)
		bindings = append(bindings, retained)
	}
	targetBinding, err := ownership.NewDocumentBinding(owner, alias.AggregateRoot(), targetExact)
	if err != nil {
		t.Fatal(err)
	}
	targetValue, _ := ownership.PresentDocumentBinding(targetBinding)
	var intentsForCarrier []ownership.DocumentBindingIntent
	if shape == carrierTransfer || shape == carrierCoalesced || bound {
		if shape == carrierRetainedSource {
			targetValue = beforeBinding
		} else if shape == carrierSourceRetirement {
			targetValue = ownership.NoDocumentBinding()
		}
		intent, err := ownership.NewExactDocumentBindingIntent(owner, alias.AggregateRoot(), beforeBinding, targetValue)
		if bound && shape == carrierTransfer {
			intent, err = ownership.NewExactDocumentBindingTransferIntent(retained, targetExact)
		}
		if err != nil {
			t.Fatal(err)
		}
		intentsForCarrier = append(intentsForCarrier, intent)
	}
	sourceClaims := claims[:1]
	if shape == carrierRetainedSource || shape == carrierCoalesced {
		address, err := ownership.NewManagedAddress(sourceExact, sourceSelection.Contracts()[0].Address().Footprint())
		if shape == carrierCoalesced {
			removed, _ := carrierMCPRow(t, originals[0], "remove", codecs)
			address, err = ownership.NewManagedAddress(sourceExact, removed.Contribution().Contract().Address().Footprint())
		}
		if err != nil {
			t.Fatal(err)
		}
		removed, err := ownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatal(err)
		}
		sourceClaims = append(slices.Clone(sourceClaims), removed)
	}
	f.beforeRegistry, err = ownership.NewRegistry(sourceClaims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	operationID := "carrier-recovery"
	member, _ := aggregate.NewSubjectContribution(subject, afterRow.Contribution())
	values, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{member})
	if err != nil {
		t.Fatal(err)
	}
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	manifestGrant, _ := grants.Manifest()
	var captured []journalrecord.RootCapture
	references := []output.RootReference{manifestReference, sourceBinding.RootReference()}
	if shape == carrierTransfer || shape == carrierCoalesced {
		references = append(references, currentBinding.RootReference())
	}
	for _, reference := range references {
		grant, _ := grants.Selection(reference)
		alternative, _ := grant.PointAlternative()
		root, err := journalrecord.NewRootCapture(reference, grant.Provenance(), alternative)
		if err != nil {
			t.Fatal(err)
		}
		captured = append(captured, root)
	}
	order := []int{0, 1}
	if shape != carrierTransfer {
		order = []int{0}
	}
	if reverse {
		slices.Reverse(order)
	}
	var groups []journalrecord.SealedDocumentGroup
	var preparedClaims []ownership.Claim
	for recordIndex, endpoint := range order {
		namespace := []journalnamespace.DocumentNamespace{sourceNamespace, targetNamespace}[endpoint]
		binding := []configuration.AliasBinding{sourceBinding, currentBinding}[endpoint]
		var desired *aggregate.ContributionSet
		var beforeClaims, afterClaims []ownership.Claim
		var intents []ownership.DocumentBindingIntent
		selected, selectedSubjects := selection, []topology.SubjectID{subject}
		if endpoint == 0 {
			beforeClaims, f.sourceIndex = sourceClaims, recordIndex
			selected, selectedSubjects = sourceSelection, sourceSubjects
			if shape != carrierTransfer {
				intents = intentsForCarrier
			}
			if shape == carrierRetainedSource || shape == carrierCoalesced {
				afterClaims = claims[:1]
			}
			if shape == carrierCoalesced {
				desired = &values
			}
		} else {
			desired, afterClaims, intents, f.targetIndex = &values, claims[1:], intentsForCarrier, recordIndex
		}
		transition, err := ownershipmutation.NewDocumentClaimTransition(beforeClaims, afterClaims, operationID)
		if err != nil {
			t.Fatal(err)
		}
		preparedClaims = append(preparedClaims, transition.Prepared()...)
		baseline, failure := projectionCodec.Read(originals[endpoint], selected)
		if failure != nil {
			t.Fatal(failure)
		}
		var projectionIntents []aggregate.ProjectionIntent
		for _, projection := range baseline.States() {
			var targetValues *aggregate.ContributionSet
			if projection.Contract().Equal(row.Contribution().Contract()) {
				targetValues = desired
			}
			intent, err := aggregate.NewProjectionIntent(projection, targetValues)
			if err != nil {
				t.Fatal(err)
			}
			projectionIntents = append(projectionIntents, intent)
		}
		plan, err := aggregate.NewPlan(baseline, projectionIntents)
		if err != nil {
			t.Fatal(err)
		}
		participant, err := journaldocument.Participant(selected, codecs)
		if err != nil {
			t.Fatal(err)
		}
		edit, err := compose.NewProjectionEdit(projectionCodec, plan, participant.Syntax(), participant.RewriteAreas())
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := journaldocument.PrepareSelected(t.Context(), journaldocument.SelectedPreparationInput{Before: originals[endpoint], BeforeMode: recovery.NewPermissionMode(0o600), ExpectedMode: recovery.NewPermissionMode(0o600), Limits: limits}, []compose.ProjectionEdit{edit}, nil)
		if err != nil {
			t.Fatal(err)
		}
		draft, err := journalrecord.BeginDocumentGroup(owner, operationID, intents, &transition, nil)
		if err != nil {
			t.Fatal(err)
		}
		aliases := []journalrecord.DocumentGroupAlias{journalrecord.NewBoundDocumentGroupAlias(alias, binding, namespace.Aliases()[0].Root().Provenance(), namespace.Observation())}
		if shape == carrierCoalesced {
			aliases = nil
			for _, entry := range namespace.Aliases() {
				binding, _ := entry.Binding()
				aliases = append(aliases, journalrecord.NewBoundDocumentGroupAlias(entry.Address(), binding, entry.Root().Provenance(), namespace.Observation()))
			}
		}
		spelling, err := namespace.WriterPath()
		if err != nil {
			t.Fatal(err)
		}
		draft, err = draft.WithBoundNamespace(t.Context(), namespace.Observation(), namespace.WriterPin(), spelling, aliases, manifestGrant.Provenance())
		if err != nil {
			t.Fatal(err)
		}
		groupCandidate, err := draft.WithComparison(candidate.Comparison(), recordIndex, selectedSubjects)
		if err != nil {
			t.Fatal(err)
		}
		group, comparison, err := groupCandidate.Seal(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		groups, f.comparisons = append(groups, group), append(f.comparisons, comparison)
	}
	f.prepared, err = ownership.NewRegistry(preparedClaims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	capturedJournal, err := journalrecord.NewNative17Capture(t.Context(), operationID, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), owner, manifestReference, captured, before, after, groups, codec)
	if err != nil {
		t.Fatal(err)
	}
	f.journal, f.read.Artifacts = publishCarrierRecovery(t, filepath.Join(f.recoveryDir, operationID), capturedJournal, f.comparisons, journalpublication.StageOptions{StateCodec: codec, Codecs: codecs, Limits: limits, Budget: budget})
	f.read.Protection, f.read.Rules, f.read.Filesystem, f.read.Budget, f.read.Limits, f.read.Codecs = namespacetest.Reservations(t), journalqualification.RecordedRuleExpectations(), testFilesystem(), carrierRecoveryBudget(t), limits, codecs
	f.records, err = journalqualification.LoadDocuments(f.journal, t.Context(), f.read)
	if err != nil {
		t.Fatal(err)
	}
	f.profile = journal.Profile{
		StateCodec: codec, StatefileCodec: physicalCodec, Codecs: codecs, Limits: limits, Manifest: manifest.SelectedPath, ManifestPath: owner.ManifestPath(), Budget: f.read.Budget, Protection: f.read.Protection,
		ResolvePoint: func(reference output.RootReference, alternative configuration.PointAlternative) (string, error) {
			return f.target, nil
		},
		StateReader: func(ctx context.Context) (durable.Snapshot, error) {
			return statefile.LoadOptionalWithOptions(ctx, f.statePath, statefile.ReadOptions{Codec: physicalCodec, PhysicalPathBudget: f.read.Budget})
		},
		RawRegistry: func(ctx context.Context) (ownership.Registry, error) {
			return ownershipstore.ReadProtectionRegistryBounded(ctx, f.registryPath, physicalwork.MaximumPhysicalPathDepth, f.read.Budget)
		},
	}
	f.persist(t, before, f.beforeRegistry)
	return f
}

func (f *carrierRecoveryFixture) persist(t *testing.T, state durable.Snapshot, registry ownership.Registry) {
	t.Helper()
	stateBytes, err := f.profile.StateCodec.Encode(state)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string][]byte{f.statePath: stateBytes, f.registryPath: registryBytes} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *carrierRecoveryFixture) paths() journalpublication.Paths {
	return journalpublication.Paths{RecoveryDir: f.recoveryDir, StatefilePath: f.statePath, ManifestRoot: f.profile.Manifest, DataDir: filepath.Dir(f.statePath)}
}

func (f *carrierRecoveryFixture) loadOptions(t *testing.T) journal.PlanLoadOptions {
	t.Helper()
	reader, err := ownershipstore.NewRecoveryReader(f.registryPath)
	if err != nil {
		t.Fatal(err)
	}
	return journal.PlanLoadOptions{Filesystem: testFilesystem(), StateCodec: f.profile.StateCodec, StateReader: f.profile.StateReader, OwnershipRegistry: reader, NativeDocuments: &f.profile, PhysicalPathBudget: f.read.Budget, Codecs: f.profile.Codecs, Resolver: func(output.Destination) (string, error) { return f.source, nil }}
}

func (f *carrierRecoveryFixture) plan(t *testing.T) (journal.NativeRecoveryPlan, journal.PlanLoadOptions) {
	t.Helper()
	load := f.loadOptions(t)
	disclosed, err := journal.LoadRecoverablePlanWithOptions(t.Context(), f.paths(), load)
	if err != nil {
		t.Fatal(err)
	}
	plan, native := disclosed.(journal.NativeRecoveryPlan)
	if !native || plan.Blocked() {
		t.Fatal("inventory did not disclose the selected native document roots")
	}
	return plan, load
}

func (f *carrierRecoveryFixture) assess(t *testing.T, demand journalqualification.RecoveryRootDemand) journalqualification.NativeRecoveryAssessment {
	t.Helper()
	plan, load := f.plan(t)
	operation, err := journal.OpenNativeRecovery(t.Context(), f.paths(), plan, load)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = operation.Close() })
	assessment := operation.InitialAssessment()
	if !assessment.Facts().RootDemand().SameRoots(demand) {
		t.Fatal("inventory changed its selected root demand")
	}
	return assessment
}

func carrierAuthorityRequests(t *testing.T, plan journal.NativeRecoveryPlan) [][]mutation.PhysicalAuthorityRequest {
	t.Helper()
	aliases, err := plan.DocumentAliases()
	if err != nil {
		t.Fatal(err)
	}
	requests := make([][]mutation.PhysicalAuthorityRequest, len(aliases))
	for index := range aliases {
		requests[index], err = plan.DocumentAuthorityRequests(index)
		if err != nil {
			t.Fatal(err)
		}
	}
	return requests
}

func TestCarrierRecoveryKeepsSourceBeforeButNeedsOnlyTargetAfterCommit(t *testing.T) {
	for _, bound := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("bound-%t/reverse-%t", bound, reverse), func(t *testing.T) {
				f := newCarrierRecoveryFixture(t, bound, reverse, carrierTransfer)
				demand, err := journalqualification.DetermineRecoveryRootDemand(t.Context(), f.journal, f.records, f.journal.Before(), f.beforeRegistry)
				if err != nil || len(demand.LiveIndexes()) != 2 {
					t.Fatal("Before omitted the historical source", err)
				}
				before := f.assess(t, demand)
				plan, _ := f.plan(t)
				requests := carrierAuthorityRequests(t, plan)
				if len(requests) != 2 || len(requests[f.sourceIndex]) != 1 || requests[f.sourceIndex][0].Path != f.source || len(requests[f.targetIndex]) != 1 || requests[f.targetIndex][0].Path != f.target {
					t.Fatal("shared logical alias lost a physical authority route", requests)
				}
				writes, err := before.Facts().DocumentWriteIndexes()
				if err != nil || !slices.Equal(writes, []int{0, 1}) {
					t.Fatal("physical claim transfers lost a write group", writes, err)
				}
				if err := os.WriteFile(f.target, f.comparisons[f.targetIndex].ExpectedDocument().Content(), 0o600); err != nil {
					t.Fatal(err)
				}
				f.persist(t, f.journal.After(), f.prepared)
				if err := os.Remove(f.source); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Dir(f.source)); err != nil {
					t.Fatal(err)
				}
				afterDemand, err := journalqualification.DetermineRecoveryRootDemand(t.Context(), f.journal, f.records, f.journal.After(), f.prepared)
				if err != nil || !slices.Equal(afterDemand.LiveIndexes(), []int{f.targetIndex}) {
					t.Fatal("committed source still required a live root", err)
				}
				after := f.assess(t, afterDemand)
				if after.Facts().Classification() != recovery.ClassificationNeedsFinalize || after.Documents().Len() != 1 || after.Documents().Records().Len() != 2 {
					t.Fatal("rootless source was mistaken for a host stage or dropped from metadata")
				}
				if index, present := after.Documents().RecordIndex(0); !present || index != f.targetIndex {
					t.Fatal("live target was confused with the full-record source index")
				}
				plan, _ = f.plan(t)
				requests = carrierAuthorityRequests(t, plan)
				if len(requests) != 1 || len(requests[0]) != 1 || requests[0][0].Path != f.target {
					t.Fatal("rootless source retained a host authority request", requests)
				}
				aliases, err := plan.DocumentAliases()
				if err != nil || len(aliases) != 1 {
					t.Fatal("plan's consumer index space still included the historical source", err)
				}
				writes, err = plan.DocumentWriteIndexes()
				if err != nil || !slices.Equal(writes, []int{0}) {
					t.Fatal("write classification used full-record indexes for live requests", writes, err)
				}
				settled, _, err := after.Facts().FinalizeClaims().Apply(f.prepared)
				if err != nil {
					t.Fatal(err)
				}
				settled, _, err = after.Facts().FinalizeBindings().Apply(settled)
				if err != nil {
					t.Fatal(err)
				}
				f.persist(t, f.journal.After(), settled)
				replayed := f.assess(t, afterDemand)
				if replayed.Facts().Classification() != recovery.ClassificationCleanAfter {
					t.Fatal("applied source retirement did not replay cleanly")
				}
				if _, err := os.Lstat(filepath.Dir(f.source)); !os.IsNotExist(err) {
					t.Fatal("rootless recovery recreated the source directory", err)
				}
				if bound && !reverse {
					checkRetiredCarrierForeignReplacement(t, f, afterDemand, replayed, settled)
				}
			})
		}
	}
}

func checkRetiredCarrierForeignReplacement(t *testing.T, f *carrierRecoveryFixture, demand journalqualification.RecoveryRootDemand, settled journalqualification.NativeRecoveryAssessment, registry ownership.Registry) {
	t.Helper()
	if err := os.Mkdir(filepath.Dir(f.source), 0o700); err != nil {
		t.Fatal(err)
	}
	const foreignContent = `{"mcpServers":{"search":{"type":"stdio","command":"foreign"}},"foreign":true}`
	if err := os.WriteFile(f.source, []byte(foreignContent), 0o600); err != nil {
		t.Fatal(err)
	}
	source, _ := f.records.At(f.sourceIndex)
	oldClaim := source.Claims().Before()[0]
	state, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(f.statePath+".foreign", physicalwork.MaximumPhysicalPathDepth, f.read.Budget)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(state.Exact(), filepath.Join(filepath.Dir(f.statePath), "foreign.toml"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := ownership.NewActiveClaim(oldClaim.Address(), owner)
	if err != nil {
		t.Fatal(err)
	}
	withForeign, err := ownership.NewRegistry(append(registry.Claims(), foreign), registry.DocumentBindings()...)
	if err != nil {
		t.Fatal(err)
	}
	f.persist(t, f.journal.After(), withForeign)
	scope, err := settled.RegistryScope()
	if err != nil {
		t.Fatal(err)
	}
	if same, err := scope.Matches(registry, withForeign); err != nil || !same {
		t.Fatal("completed retirement retained a comparison fence over a new foreign document", err)
	}
	replayed := f.assess(t, demand)
	actual, changed, err := replayed.Facts().FinalizeClaims().Apply(withForeign)
	if err != nil || changed || !actual.Equal(withForeign) || replayed.Facts().Classification() != recovery.ClassificationCleanAfter {
		t.Fatal("already-applied source retirement replayed over a new foreign claim", err)
	}
	if content, err := os.ReadFile(f.source); err != nil || string(content) != foreignContent {
		t.Fatal("rootless recovery touched the replacement source document", err)
	}

	changedRegistry, err := ownership.NewRegistry(append(registry.Claims(), oldClaim), registry.DocumentBindings()...)
	if err != nil {
		t.Fatal(err)
	}
	load := f.loadOptions(t)
	load.OwnershipRegistry = conflictingQualifiedRegistry{RegistryReader: load.OwnershipRegistry, registry: changedRegistry}
	value, err := journal.LoadRecoverablePlanWithOptions(t.Context(), f.paths(), load)
	if err == nil || value != nil {
		t.Fatal("qualified Registry changed the retirement decision but returned usable document facts", err)
	}
	f.execute(t)
	actual, err = f.profile.RawRegistry(t.Context())
	if err != nil || !actual.Equal(withForeign) {
		t.Fatal("executor reclaimed a new foreign source claim during journal retirement", err)
	}
	if content, err := os.ReadFile(f.source); err != nil || string(content) != foreignContent {
		t.Fatal("executor rewrote the rootless source replacement", err)
	}
}

type conflictingQualifiedRegistry struct {
	ownershipmutation.RegistryReader
	registry ownership.Registry
}

func (reader conflictingQualifiedRegistry) LoadForClaimRemovals(context.Context, []ownership.Claim, int, rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	return reader.registry, nil
}

func TestCarrierRecoveryRefusesSourceRetargetedToLeafSymlink(t *testing.T) {
	f := newCarrierRecoveryFixture(t, true, false, carrierTransfer)
	if err := os.Remove(f.source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.target, f.source); err != nil {
		t.Skip("symlink creation is unavailable", err)
	}
	value, err := journal.LoadRecoverablePlanWithOptions(t.Context(), f.paths(), f.loadOptions(t))
	if err == nil || value != nil {
		t.Fatal("retargeted source supplied live owned roots", err)
	}
	if content, err := os.ReadFile(f.target); err != nil || string(content) != `{"peer":2}` {
		t.Fatal("refused source retarget changed its referent", err)
	}
}

func TestExecuteCarrierRecoveryRetiresWithoutOpeningMissingSource(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(fmt.Sprintf("bound-%t", bound), func(t *testing.T) {
			f := newCarrierRecoveryFixture(t, bound, false, carrierTransfer)
			if err := os.WriteFile(f.target, f.comparisons[f.targetIndex].ExpectedDocument().Content(), 0o600); err != nil {
				t.Fatal(err)
			}
			f.persist(t, f.journal.After(), f.prepared)
			if err := os.Remove(f.source); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Dir(f.source)); err != nil {
				t.Fatal(err)
			}
			demand, err := journalqualification.DetermineRecoveryRootDemand(t.Context(), f.journal, f.records, f.journal.After(), f.prepared)
			if err != nil {
				t.Fatal(err)
			}
			assessment := f.assess(t, demand)
			expected, _, err := assessment.Facts().FinalizeClaims().Apply(f.prepared)
			if err != nil {
				t.Fatal(err)
			}
			expected, _, err = assessment.Facts().FinalizeBindings().Apply(expected)
			if err != nil {
				t.Fatal(err)
			}
			f.execute(t)
			actual, err := f.profile.RawRegistry(t.Context())
			if err != nil || !actual.Equal(expected) {
				t.Fatal("bound store did not commit the exact metadata convergence", err)
			}
			if _, err := os.Lstat(filepath.Dir(f.source)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("executor reopened or recreated the retired source parent", err)
			}
			if entries, err := os.ReadDir(f.recoveryDir); err != nil || len(entries) != 0 {
				t.Fatal("metadata settlement did not retire its journal", err)
			}
			if plan, err := journal.LoadRecoverablePlanWithOptions(t.Context(), f.paths(), f.loadOptions(t)); !errors.Is(err, journal.ErrNoRecoverableJournal) || plan != nil {
				t.Fatal("second recovery did not observe the completed no-op", err)
			}
		})
	}
}

func TestExecuteUnpairedCarrierSourcesUseRecordedPhase(t *testing.T) {
	for _, shape := range []carrierRecoveryShape{carrierRetainedSource, carrierSourceRetirement} {
		for _, bound := range []bool{false, true} {
			t.Run(fmt.Sprintf("shape-%d/bound-%t", shape, bound), func(t *testing.T) {
				f := newCarrierRecoveryFixture(t, bound, false, shape)
				f.profile.ResolvePoint = func(output.RootReference, configuration.PointAlternative) (string, error) {
					return "", errors.New("unpaired source has no current selector")
				}
				beforeDemand, err := journalqualification.DetermineRecoveryRootDemand(t.Context(), f.journal, f.records, f.journal.Before(), f.beforeRegistry)
				if err != nil {
					t.Fatal(err)
				}
				before := f.assess(t, beforeDemand)
				if before.Documents().Len() != 1 {
					t.Fatal("unpaired Before source lost its recorded witness")
				}
				if err := os.WriteFile(f.source, f.comparisons[0].ExpectedDocument().Content(), 0o600); err != nil {
					t.Fatal(err)
				}
				f.persist(t, f.journal.After(), f.prepared)
				if shape == carrierSourceRetirement {
					if err := os.Remove(f.source); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(filepath.Dir(f.source)); err != nil {
						t.Fatal(err)
					}
				}
				afterDemand, err := journalqualification.DetermineRecoveryRootDemand(t.Context(), f.journal, f.records, f.journal.After(), f.prepared)
				if err != nil {
					t.Fatal(err)
				}
				after := f.assess(t, afterDemand)
				if (after.Documents().Len() != 0) != (shape == carrierRetainedSource) {
					t.Fatal("actual After membership did not select the live source")
				}
				if shape == carrierRetainedSource {
					checkPendingCarrierSourceInterference(t, f)
				}
				f.execute(t)
				registry, err := f.profile.RawRegistry(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				source, _ := f.records.At(0)
				destination := source.Group().Aliases()[0].AggregateRoot()
				binding, present := registry.DocumentBinding(f.journal.Owner().Key(), destination)
				if present != (bound && shape == carrierRetainedSource) {
					t.Fatal("source binding did not follow the last After member")
				}
				if present {
					beforeBinding, _ := f.beforeRegistry.DocumentBinding(f.journal.Owner().Key(), destination)
					if !binding.Equal(beforeBinding) {
						t.Fatal("retained source rebound through a current selector")
					}
				}
				if shape == carrierSourceRetirement {
					if len(registry.Claims()) != 0 {
						t.Fatal("source retirement left exact own claims")
					}
					if _, err := os.Lstat(filepath.Dir(f.source)); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("source-only retirement recreated the old parent", err)
					}
				}
				if entries, err := os.ReadDir(f.recoveryDir); err != nil || len(entries) != 0 {
					t.Fatal("unpaired source recovery did not retire its journal", err)
				}
			})
		}
	}
}

func checkPendingCarrierSourceInterference(t *testing.T, f *carrierRecoveryFixture) {
	t.Helper()
	if err := os.WriteFile(f.source, f.comparisons[0].BeforeDocument().Content(), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := journal.LoadRecoverablePlanWithOptions(t.Context(), f.paths(), f.loadOptions(t))
	if plan, native := value.(journal.NativeRecoveryPlan); err == nil && (!native || !plan.Blocked()) {
		t.Fatal("pending source retired a projection that reappeared before settlement")
	}
	actual, err := f.profile.RawRegistry(t.Context())
	if err != nil || !actual.Equal(f.prepared) {
		t.Fatal("refused interference changed source metadata", err)
	}
	if err := os.WriteFile(f.source, f.comparisons[0].ExpectedDocument().Content(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteCoalescedCarrierRecoveryKeepsPhaseWitnesses(t *testing.T) {
	for _, bound := range []bool{false, true} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("bound-%t/after-%t", bound, committed), func(t *testing.T) {
				f := newCarrierRecoveryFixture(t, bound, false, carrierCoalesced)
				before, _ := f.plan(t)
				requests := carrierAuthorityRequests(t, before)
				if len(requests) != 1 || len(requests[0]) != 2 || !slices.ContainsFunc(requests[0], func(request mutation.PhysicalAuthorityRequest) bool { return request.Path == f.source }) || !slices.ContainsFunc(requests[0], func(request mutation.PhysicalAuthorityRequest) bool { return request.Path == f.target }) {
					t.Fatal("coalesced Before lost a distinct selector entry", requests)
				}
				if err := os.WriteFile(f.source, f.comparisons[0].ExpectedDocument().Content(), 0o600); err != nil {
					t.Fatal(err)
				}
				state := f.journal.Before()
				if committed {
					state = f.journal.After()
				}
				f.persist(t, state, f.prepared)
				wantedRegistry := f.beforeRegistry
				if committed {
					after, _ := f.plan(t)
					requests = carrierAuthorityRequests(t, after)
					if len(requests) != 1 || len(requests[0]) != 1 || requests[0][0].Path != f.target {
						t.Fatal("coalesced After did not select its current witness", requests)
					}
					demand, err := journalqualification.DetermineRecoveryRootDemand(t.Context(), f.journal, f.records, state, f.prepared)
					if err != nil {
						t.Fatal(err)
					}
					assessment := f.assess(t, demand)
					wantedRegistry, _, err = assessment.Facts().FinalizeClaims().Apply(f.prepared)
					if err != nil {
						t.Fatal(err)
					}
					wantedRegistry, _, err = assessment.Facts().FinalizeBindings().Apply(wantedRegistry)
					if err != nil {
						t.Fatal(err)
					}
				}
				f.execute(t)
				actualRegistry, err := f.profile.RawRegistry(t.Context())
				if err != nil || !actualRegistry.Equal(wantedRegistry) {
					t.Fatal("coalesced recovery did not preserve the selected claim and binding phase", err)
				}
				content, err := os.ReadFile(f.source)
				if err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(f.source)
				if err != nil {
					t.Fatal(err)
				}
				match, err := f.comparisons[0].Match(t.Context(), aggregate.ExistingDocument(content), recovery.NewPermissionMode(info.Mode().Perm()))
				if err != nil || committed && !match.MatchesExpected() || !committed && !match.MatchesBefore() {
					t.Fatal("coalesced recovery selected the wrong projection phase", err)
				}
				var fringe struct {
					Preserved int `json:"preserved"`
				}
				if err := json.Unmarshal(content, &fringe); err != nil || fringe.Preserved != 1 {
					t.Fatal("coalesced recovery lost the disjoint foreign field", err)
				}
				if _, err := os.Readlink(f.target); err != nil {
					t.Fatal("coalesced recovery replaced its current selector symlink", err)
				}
				if entries, err := os.ReadDir(f.recoveryDir); err != nil || len(entries) != 0 {
					t.Fatal("coalesced recovery did not retire", err)
				}
			})
		}
	}
}

func TestUnpairedSourceBindingHistoryDoesNotFallBack(t *testing.T) {
	for _, recordedBound := range []bool{false, true} {
		t.Run(fmt.Sprintf("recorded-bound-%t", recordedBound), func(t *testing.T) {
			f := newCarrierRecoveryFixture(t, recordedBound, false, carrierRetainedSource)
			_, _ = f.plan(t)
			var bindings []ownership.DocumentBinding
			if !recordedBound {
				source, _ := f.records.At(0)
				binding, err := ownership.NewDocumentBinding(f.journal.Owner(), source.Group().Aliases()[0].AggregateRoot(), source.Claims().Before()[0].Address().PathAuthority())
				if err != nil {
					t.Fatal(err)
				}
				bindings = append(bindings, binding)
			}
			changed, err := ownership.NewRegistry(f.beforeRegistry.Claims(), bindings...)
			if err != nil {
				t.Fatal(err)
			}
			f.persist(t, f.journal.Before(), changed)
			load := f.loadOptions(t)
			calls := 0
			load.Resolver = func(output.Destination) (string, error) {
				calls++
				return f.source, nil
			}
			if plan, err := journal.LoadRecoverablePlanWithOptions(t.Context(), f.paths(), load); err == nil || plan != nil || calls != 0 {
				t.Fatal("source history fell through between bound C and coincident literal X", err)
			}
			if content, err := os.ReadFile(f.source); err != nil || string(content) != string(f.comparisons[0].BeforeDocument().Content()) {
				t.Fatal("refused source-basis substitution changed the file", err)
			}
		})
	}
}

func TestCommittedUnpairedSourceRefusesLostBinding(t *testing.T) {
	f := newCarrierRecoveryFixture(t, true, false, carrierRetainedSource)
	content := f.comparisons[0].ExpectedDocument().Content()
	if err := os.WriteFile(f.source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	f.persist(t, f.journal.After(), f.prepared)
	_, _ = f.plan(t)
	missingBinding, err := ownership.NewRegistry(f.prepared.Claims())
	if err != nil {
		t.Fatal(err)
	}
	f.persist(t, f.journal.After(), missingBinding)
	load := f.loadOptions(t)
	calls := 0
	load.Resolver = func(output.Destination) (string, error) {
		calls++
		return f.source, nil
	}
	if plan, err := journal.LoadRecoverablePlanWithOptions(t.Context(), f.paths(), load); err == nil || plan != nil || calls != 0 {
		t.Fatal("committed retained source fell through to legacy after losing its bound fact", err)
	}
	actual, err := f.profile.RawRegistry(t.Context())
	if err != nil || !actual.Equal(missingBinding) {
		t.Fatal("refused committed binding loss changed the Registry", err)
	}
	if actual, err := os.ReadFile(f.source); err != nil || string(actual) != string(content) {
		t.Fatal("refused committed binding loss changed the source", err)
	}
}

func TestExecuteSettledLiveSourcePreservesForeignBeforeOnlyReplacement(t *testing.T) {
	for _, shape := range []carrierRecoveryShape{carrierRetainedSource, carrierCoalesced} {
		for _, bound := range []bool{false, true} {
			t.Run(fmt.Sprintf("shape-%d/bound-%t", shape, bound), func(t *testing.T) {
				f := newCarrierRecoveryFixture(t, bound, false, shape)
				if err := os.WriteFile(f.source, f.comparisons[0].ExpectedDocument().Content(), 0o600); err != nil {
					t.Fatal(err)
				}
				f.persist(t, f.journal.After(), f.prepared)
				demand, err := journalqualification.DetermineRecoveryRootDemand(t.Context(), f.journal, f.records, f.journal.After(), f.prepared)
				if err != nil {
					t.Fatal(err)
				}
				after := f.assess(t, demand)
				expected, _, err := after.Facts().FinalizeClaims().Apply(f.prepared)
				if err != nil {
					t.Fatal(err)
				}
				expected, _, err = after.Facts().FinalizeBindings().Apply(expected)
				if err != nil {
					t.Fatal(err)
				}
				stop := errors.New("interrupt after actual Registry settlement")
				stopped := false
				err = f.executeWithCheckpoint(t, func(ctx context.Context) error {
					current, err := f.profile.RawRegistry(ctx)
					if err != nil {
						return err
					}
					if current.Equal(expected) {
						stopped = true
						return stop
					}
					return nil
				})
				if !stopped || !errors.Is(err, stop) {
					t.Fatal("recovery did not retain the actual post-settlement crash window", err)
				}
				source, _ := f.records.At(0)
				oldClaims := source.Claims().Before()
				index := slices.IndexFunc(oldClaims, func(claim ownership.Claim) bool { return !slices.ContainsFunc(source.Claims().After(), claim.Equal) })
				if index < 0 {
					t.Fatal("fixture has no exact Before-only selected claim")
				}
				foreign := carrierForeignClaim(t, f, oldClaims[index])
				withForeign, err := ownership.NewRegistry(append(expected.Claims(), foreign), expected.DocumentBindings()...)
				if err != nil {
					t.Fatal(err)
				}
				command := "npx"
				if shape == carrierCoalesced {
					command = "node"
				}
				content := fmt.Sprintf(`{"preserved":1,"mcpServers":{"search":{"type":"stdio","command":%q},"remove":{"type":"stdio","command":"foreign"}}}`, command)
				if err := os.WriteFile(f.source, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				f.persist(t, f.journal.After(), withForeign)
				if shape == carrierCoalesced {
					checkSettledCarrierSurvivorInterference(t, f, withForeign)
					if err := os.WriteFile(f.source, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				f.execute(t)
				actual, err := f.profile.RawRegistry(t.Context())
				if err != nil || !actual.Equal(withForeign) {
					t.Fatal("retired source replayed its exact CAS over the new foreign claim", err)
				}
				if actual, err := os.ReadFile(f.source); err != nil || string(actual) != content {
					t.Fatal("retired source replayed a Before-only restoration over foreign content", err)
				}
				if entries, err := os.ReadDir(f.recoveryDir); err != nil || len(entries) != 0 {
					t.Fatal("retired live source could not finish journal retirement", err)
				}
			})
		}
	}
}

func checkSettledCarrierSurvivorInterference(t *testing.T, f *carrierRecoveryFixture, registry ownership.Registry) {
	t.Helper()
	const changed = `{"preserved":1,"mcpServers":{"search":{"type":"stdio","command":"interference"},"remove":{"type":"stdio","command":"foreign"}}}`
	if err := os.WriteFile(f.source, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := journal.LoadRecoverablePlanWithOptions(t.Context(), f.paths(), f.loadOptions(t))
	if plan, native := value.(journal.NativeRecoveryPlan); err == nil && (!native || !plan.Blocked()) {
		t.Fatal("completed source phase discarded a surviving selected After projection")
	}
	actual, err := f.profile.RawRegistry(t.Context())
	if err != nil || !actual.Equal(registry) {
		t.Fatal("refused After interference changed source metadata", err)
	}
	if content, err := os.ReadFile(f.source); err != nil || string(content) != changed {
		t.Fatal("refused After interference changed the selected document", err)
	}
	if _, err := os.Stat(filepath.Join(f.recoveryDir, f.journal.OperationID(), journalrecord.FileName)); err != nil {
		t.Fatal("refused After interference retired its journal", err)
	}
}

func carrierForeignClaim(t *testing.T, f *carrierRecoveryFixture, previous ownership.Claim) ownership.Claim {
	t.Helper()
	state, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(f.statePath+".foreign", physicalwork.MaximumPhysicalPathDepth, f.read.Budget)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(state.Exact(), filepath.Join(filepath.Dir(f.statePath), "foreign.toml"))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(previous.Address(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func (f *carrierRecoveryFixture) execute(t *testing.T) {
	t.Helper()
	if err := f.executeWithCheckpoint(t, nil); err != nil {
		t.Fatal(err)
	}
}

func (f *carrierRecoveryFixture) executeWithCheckpoint(t *testing.T, checkpoint func(context.Context) error) error {
	t.Helper()
	f.read.Budget = carrierRecoveryBudget(t)
	f.profile.Budget = f.read.Budget
	plan, load := f.plan(t)
	store, err := mutation.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	control, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: f.statePath, Access: mutation.AccessExclusive, Effect: mutation.PathEffectDirectoryEntry})
	if err != nil {
		t.Fatal(err)
	}
	domains := []mutation.Domain{control}
	for _, group := range carrierAuthorityRequests(t, plan) {
		for _, request := range group {
			for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
				domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{Path: request.Path, Access: mutation.AccessExclusive, Effect: effect, Target: request.Target, Scope: request.Scope})
				if err != nil {
					t.Fatal(err)
				}
				domains = append(domains, domain)
			}
		}
	}
	leases, err := store.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := leases.Release(); err != nil {
			t.Error(err)
		}
	}()
	options := execute.RecoveryOptions{
		Filesystem: testFilesystem(), StateCodec: f.profile.StateCodec, Codecs: f.profile.Codecs, OwnershipRegistryBinder: testOwnershipRegistryBinder(),
		Resolver: func(output.Destination, rootedpath.PhysicalTraversalBudget) (string, error) { return f.source, nil },
		ValidateBeforeEffects: func(_ context.Context, selected mutation.PhysicalAuthoritySet) error {
			covered, err := leases.CoversPhysicalAuthority(selected)
			if err != nil || !covered {
				return errors.Join(errors.New("carrier recovery lacks selected physical leases"), err)
			}
			return nil
		},
		ValidateVisibilityAuthority: func(ctx context.Context) error {
			matches, err := leases.VisibilityAuthorityMatchesCurrent(ctx)
			if err != nil || !matches {
				return errors.Join(errors.New("carrier recovery visibility changed"), err)
			}
			return nil
		},
		AcceptVisibilityChanges: func(ctx context.Context) error {
			accepted, err := leases.AcceptVisibilityChanges(ctx)
			if err != nil || !accepted {
				return errors.Join(errors.New("carrier recovery visibility acceptance failed"), err)
			}
			if checkpoint != nil {
				return checkpoint(ctx)
			}
			return nil
		},
	}
	paths := execute.Paths{RecoveryDir: f.recoveryDir, StatefilePath: f.statePath, ManifestRoot: f.profile.Manifest, DataDir: filepath.Dir(f.statePath), OwnershipRegistryPath: f.registryPath}
	return ExecuteNativeRecovery(t.Context(), plan, paths, load, options)
}

func carrierMCPRow(t *testing.T, document aggregate.Document, name string, codecs aggregate.CodecCatalog) (durable.ManagedAggregateState, aggregate.Selection) {
	t.Helper()
	placement, _ := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, target.ScopeGlobal)
	contract, err := placement.ProjectionContract(name)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := codecs.Lookup(contract.CodecContractID())
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	projection, _ := snapshot.State(contract)
	contribution, err := placement.Contribution(name, projection.CanonicalProjection())
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), name)
	if err != nil {
		t.Fatal(err)
	}
	row, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	return row, selection
}

func carrierRecoveryBudget(t *testing.T) *physicalwork.Budget {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func carrierRecoveryRoot(t *testing.T, path string) journalnamespace.RootSelection {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := rootedpath.CaptureRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return journalnamespace.RootSelection{SelectedPath: path, Root: root}
}

func publishCarrierRecovery(t *testing.T, destination string, value journalrecord.Native, comparisons []journaldocument.Comparison, options journalpublication.StageOptions) (journalrecord.Native, *rootedpath.CapturedRoot) {
	t.Helper()
	root, bound, err := rootedpath.CaptureDestination(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	capability, err := root.Acquire(bound)
	if err != nil {
		t.Fatal(err)
	}
	defer capability.Close()
	prepared, err := testFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		_, err := journalpublication.StageDocuments(value, t.Context(), comparisons, writer, options)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	artifacts, err := rootedpath.CaptureRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = artifacts.Close() })
	snapshot, err := journalqualification.ReadJournalSnapshot(t.Context(), artifacts, testFilesystem(), options.Budget)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := journalrecord.DecodeNativeSnapshot(t.Context(), snapshot, journalrecord.FileName, options.StateCodec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, present := loaded.Native()
	if !present {
		t.Fatal("published carrier journal decoded as another family")
	}
	return decoded, artifacts
}
