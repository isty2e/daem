package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestSelectedGroupCaptureDerivesActualSubjectFrontierAndPreservesForeignData(t *testing.T) {
	fixture := nativeJournalFixture(t)
	selected := fixture.after.ManagedAggregates()[0]
	outlying := membershipNativeRow(t, fixture.claims.alias, "native-json-v1", "outside")
	outlying = selectionNativeRow(t, outlying.Contribution().Contract(), nativeRecordCanonical(t, "1"))
	fixture.before = selectionState(t, fixture.before.ManagedAggregates()[0], outlying)
	fixture.after = selectionState(t, selected, outlying)
	original := aggregate.ExistingDocument([]byte(`{"model":true,"outside":1,"foreign":0}`))
	edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
	capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil {
		t.Fatal(err)
	}
	if len(group.SelectedSubjects) != 1 || capture.reader.imageReads != 1 {
		t.Fatal("selected references expanded to all membership or original was not read once")
	}
	subject, err := group.SelectedSubjects[0].canonical()
	if err != nil || subject != selected.Subject() {
		t.Fatalf("capture lost full actual subject identity: %v", err)
	}
	if group.Original.Mode == nil || *group.Original.Mode != 0o644 || group.Expected.Mode == nil || *group.Expected.Mode != 0o644 {
		t.Fatal("selected writer changed the existing document mode")
	}
	capture.requireCompleteStage(t, group, comparison)
	current := aggregate.ExistingDocument([]byte(`{"model":"fast","outside":1,"foreign":99}`))
	restored, err := comparison.RestoreBefore(t.Context(), current, recovery.NewPermissionMode(0o644))
	if err != nil {
		t.Fatal(err)
	}
	var values struct {
		Model            bool
		Outside, Foreign int
	}
	if err := json.Unmarshal(restored.Document().Content(), &values); err != nil || !values.Model || values.Outside != 1 || values.Foreign != 99 {
		t.Fatalf("selected recovery changed unselected data: %+v, %v", values, err)
	}
}

func TestSelectedGroupCaptureCreatesMissingDocumentWithPrivateMode(t *testing.T) {
	fixture := nativeJournalFixture(t)
	fixture.before = durable.EmptySnapshot()
	selected := fixture.after.ManagedAggregates()[0]
	original := aggregate.AbsentDocument()
	edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
	capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil {
		t.Fatal(err)
	}
	if group.Original.Kind != "absent" || group.Expected.Mode == nil || *group.Expected.Mode != 0o600 {
		t.Fatal("new selected root lost absence or private creation mode")
	}
	capture.requireCompleteStage(t, group, comparison)
}

func TestSelectedGroupCaptureCombinesActualNativeAndMCPPlans(t *testing.T) {
	fixture := nativeJournalFixture(t)
	placement, found := aggregate.MCPPlacementForID(aggregate.MCPPlacementCodexProject)
	if !found {
		t.Fatal("Codex MCP placement missing")
	}
	subject, err := topologymcp.ProjectionSubject(target.TargetCodex, target.ScopeProject, "search")
	if err != nil {
		t.Fatal(err)
	}
	var mcpRows []durable.ManagedAggregateState
	for _, command := range []string{"node", "python"} {
		canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{ServerID: "search", Command: command, AdapterContract: aggregate.CodexProjectMCPStdioCommandV1})
		if err != nil {
			t.Fatal(err)
		}
		contribution, err := placement.Contribution("search", string(canonical))
		if err != nil {
			t.Fatal(err)
		}
		row, err := durable.NewManagedAggregateState(subject, contribution)
		if err != nil {
			t.Fatal(err)
		}
		mcpRows = append(mcpRows, row)
	}
	fixture.claims.alias = mcpRows[0].Contribution().Contract().Address().Document()
	before := membershipNativeRow(t, fixture.claims.alias, "native-toml-v1", "model")
	before = selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, "true"))
	after := selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, `"fast"`))
	fixture.before, fixture.after = selectionState(t, before, mcpRows[0]), selectionState(t, after, mcpRows[1])
	fixture.read.codecs, err = aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{after.Contribution().CodecContractID(), mcpRows[1].Contribution().CodecContractID()}, fixture.read.limits, fixture.read.limits)
	if err != nil {
		t.Fatal(err)
	}
	fixture.codec, err = statefile.NewNativeCodec(fixture.read.codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	original := aggregate.ExistingDocument([]byte("model=true\n[mcp_servers.search]\ncommand='node'\n[mcp_servers.foreign]\ncommand='foreign' # retained\n"))
	edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{after.Contribution().Contract()}, []durable.ManagedAggregateState{after})
	edits = append(edits, selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{mcpRows[1].Contribution().Contract()}, mcpRows[1:])...)
	capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil || len(group.SelectedSubjects) != 2 {
		t.Fatalf("actual native/MCP plans did not form one group: %v", err)
	}
	foreign, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{ServerID: "foreign", Command: "foreign", AdapterContract: aggregate.CodexProjectMCPStdioCommandV1})
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution("foreign", string(foreign))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		t.Fatal(err)
	}
	codec, available := fixture.read.codecs.Lookup(selection.CodecContractID())
	if !available {
		t.Fatal("foreign MCP verification codec missing")
	}
	observed, failure := codec.Read(comparison.ExpectedDocument(), selection)
	if failure != nil || observed.States()[0].CanonicalProjection() != contribution.CanonicalContribution() || !bytes.Contains(comparison.ExpectedDocument().Content(), []byte("# retained")) {
		t.Fatalf("MCP rewrite lost the foreign value or existing comment text: %v", failure)
	}
	capture.requireCompleteStage(t, group, comparison)
}

func TestSelectedGroupCaptureSupportsReleaseWithoutInventedSelectedSubjects(t *testing.T) {
	fixture := nativeJournalFixture(t)
	fixture.after = durable.EmptySnapshot()
	original := aggregate.ExistingDocument([]byte(`{"model":true,"foreign":99}`))
	capture := selectedCaptureFixture(t, fixture, original, nil, nil, journalnamespace.Roots{})
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil || len(group.SelectedSubjects) != 0 || !comparison.ExpectedDocument().Equal(original) {
		t.Fatalf("release changed host bytes or invented a frontier: %v", err)
	}
	capture.requireCompleteStage(t, group, comparison)
}

func TestSelectedGroupCaptureRetainsRealPiContextAndOrderOnlyWork(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			loaded, order := piEvidenceFixture(t, scope)
			command, known := loaded.Relations[0].SourceContext()
			if !known {
				t.Fatal("actual Pi relation has no command context")
			}
			roots := journalnamespace.Roots{Manifest: nativeNamespaceRoot(t, command.BaseRoot())}
			if scope == target.ScopeGlobal {
				roots.PiAgent = nativeNamespaceRoot(t, loaded.SettingsContext.BaseRoot())
			}
			fixture := nativeJournalFixture(t)
			fixture.claims.alias, _ = piOrderDocumentAlias(scope)
			fixture.before, fixture.after = durable.EmptySnapshot(), durable.EmptySnapshot()
			capture := selectedCaptureFixture(t, fixture, loaded.Document, nil, &order, roots)
			group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
			if err != nil || group.PiOrder == nil || len(group.SelectedSubjects) != 0 || !comparison.Changed() {
				t.Fatalf("real Pi-only group failed: %v", err)
			}
			if *group.Original.Mode != 0o644 || *group.Expected.Mode != 0o644 {
				t.Fatal("Pi-only work imposed an aggregate writer mode")
			}
			capture.requireCompleteStage(t, group, comparison)
			loaded.Document = comparison.ExpectedDocument()
			unchanged, err := pipackage.ReconstructOrder(loaded)
			if err != nil || unchanged.Changed() {
				t.Fatalf("actual converged order was not unchanged: %v", err)
			}
			capture = selectedCaptureFixture(t, fixture, loaded.Document, nil, &unchanged, roots)
			group, comparison, err = prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
			if err != nil || group.PiOrder != nil || comparison.Changed() || !comparison.ExpectedDocument().Equal(loaded.Document) {
				t.Fatalf("unchanged order acquired recovery ownership: %v", err)
			}
		})
	}
}

func TestSelectedGroupCaptureCombinesActualNativeAndPiOrder(t *testing.T) {
	loaded, order := piEvidenceFixture(t, target.ScopeGlobal)
	command, known := loaded.Relations[0].SourceContext()
	if !known {
		t.Fatal("actual Pi relation lacks source context")
	}
	roots := journalnamespace.Roots{Manifest: nativeNamespaceRoot(t, command.BaseRoot()), PiAgent: nativeNamespaceRoot(t, loaded.SettingsContext.BaseRoot())}
	fixture := nativeJournalFixture(t)
	fixture.claims.alias, _ = piOrderDocumentAlias(target.ScopeGlobal)
	before := membershipNativeRow(t, fixture.claims.alias, "native-json-v1", "theme")
	before = selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, `"day"`))
	after := selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, `"night"`))
	fixture.before, fixture.after = selectionState(t, before), selectionState(t, after)
	edits := selectedCaptureEdits(t, fixture, loaded.Document, []aggregate.ProjectionContract{after.Contribution().Contract()}, []durable.ManagedAggregateState{after})
	capture := selectedCaptureFixture(t, fixture, loaded.Document, edits, &order, roots)
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil || len(group.SelectedSubjects) != 1 || group.PiOrder == nil || !comparison.Changed() {
		t.Fatalf("actual native/Pi plans did not form one group: %v", err)
	}
	capture.requireCompleteStage(t, group, comparison)
}

func TestSelectedGroupCaptureKeepsWholeMembershipSeparateFromNarrowedFrontier(t *testing.T) {
	fixture := nativeJournalFixture(t)
	selected := fixture.after.ManagedAggregates()[0]
	id, _, found := topologyprojection.HostConfigKeyIdentity(selected.Subject())
	if !found {
		t.Fatal("selected subject has no native key identity")
	}
	wholeSubject, err := aggregate.HostConfigDocumentSubject(id, fixture.claims.alias.Target(), fixture.claims.alias.Scope())
	if err != nil {
		t.Fatal(err)
	}
	original := aggregate.ExistingDocument([]byte(`{"model":true,"foreign":99}`))
	whole, err := durable.NewManagedPathState(wholeSubject, []target.Target{fixture.claims.alias.Target()}, fixture.claims.alias.Scope(), fixture.claims.alias.AggregateRoot(), artifact.HashFileContent(original.Content()), realization.PathProjectionFile, realization.PathPermissionsExact, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fixture.before, err = durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{whole}})
	if err != nil {
		t.Fatal(err)
	}
	edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
	capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil || len(group.SelectedSubjects) != 1 {
		t.Fatalf("whole-to-key capture failed: %v", err)
	}
	ref, err := group.SelectedSubjects[0].canonical()
	if err != nil || ref == wholeSubject || ref != selected.Subject() {
		t.Fatal("whole membership leaked into the selected frontier")
	}
	capture.requireCompleteStage(t, group, comparison)
}

func TestSelectedGroupCaptureRejectsWrongPiCommandRootBeforeRead(t *testing.T) {
	loaded, order := piEvidenceFixture(t, target.ScopeGlobal)
	roots := journalnamespace.Roots{Manifest: nativeNamespaceRoot(t, t.TempDir()), PiAgent: nativeNamespaceRoot(t, loaded.SettingsContext.BaseRoot())}
	fixture := nativeJournalFixture(t)
	var err error
	fixture.claims.alias, err = piOrderDocumentAlias(target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	fixture.before, fixture.after = durable.EmptySnapshot(), durable.EmptySnapshot()
	capture := selectedCaptureFixture(t, fixture, loaded.Document, nil, &order, roots)
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err == nil || len(group.Aliases) != 0 || comparison.Validate() == nil || capture.reader.imageReads != 0 {
		t.Fatalf("wrong Pi command root was admitted or read host bytes: %v", err)
	}
}

func TestSelectedGroupCaptureRefusesStaticContractAndMetadataMisjoinsBeforeRead(t *testing.T) {
	for _, mismatch := range []string{"missing index", "wrong alias", "zero edit", "wrong owner", "wrong Pi alias"} {
		t.Run(mismatch, func(t *testing.T) {
			fixture := nativeJournalFixture(t)
			original := aggregate.ExistingDocument([]byte(`{"model":true,"outside":1}`))
			selected := fixture.after.ManagedAggregates()[0]
			edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
			capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
			switch mismatch {
			case "missing index":
				capture.subjects = nil
			case "wrong alias":
				other := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/other.json")
				var err error
				capture.namespace, err = journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{other}, capture.roots, capture.reader, capture.input.budget)
				if err != nil {
					t.Fatal(err)
				}
			case "zero edit":
				capture.input.edits = []compose.ProjectionEdit{{}}
			case "wrong owner":
				capture.input.owner = documentClaimsForTest(t).owner
			case "wrong Pi alias":
				_, order := piEvidenceFixture(t, target.ScopeGlobal)
				capture.input.order = &order
			}
			group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
			if err == nil || len(group.Aliases) != 0 || comparison.Validate() == nil || capture.reader.imageReads != 0 {
				t.Fatalf("static misjoin escaped or read host bytes: %v", err)
			}
		})
	}
}

func TestSelectedGroupCaptureCancellationAndObservedDriftExposeNoFacts(t *testing.T) {
	for _, failure := range []string{"cancel", "changed selected value", "read limit"} {
		t.Run(failure, func(t *testing.T) {
			fixture := nativeJournalFixture(t)
			original := aggregate.ExistingDocument([]byte(`{"model":true,"outside":1}`))
			selected := fixture.after.ManagedAggregates()[0]
			edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
			capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
			ctx := t.Context()
			switch failure {
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "changed selected value":
				if err := os.WriteFile(capture.path, []byte(`{"model":123,"outside":1}`), 0o644); err != nil {
					t.Fatal(err)
				}
			case "read limit":
				capture.input.limits.MaximumBytes = 2
			}
			group, comparison, err := prepareSelectedDocumentGroup(ctx, capture.namespace, capture.input, capture.subjects)
			if err == nil || len(group.Aliases) != 0 || comparison.Validate() == nil {
				t.Fatalf("failed selected capture exposed facts: %v", err)
			}
			if failure == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

func TestDocumentCaptureSubjectIndexKeepsFullRetargetedIdentity(t *testing.T) {
	fixture := nativeJournalFixture(t)
	before := fixture.before.ManagedAggregates()[0]
	nextAlias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/retargeted.json")
	nextContract, err := aggregate.NewHostConfigProjectionContract(nextAlias.Target(), nextAlias.Scope(), nextAlias.AggregateRoot(), before.Contribution().Footprint(), before.Contribution().CodecContractID())
	if err != nil {
		t.Fatal(err)
	}
	nextContribution, err := aggregate.NewManagedContribution(nextContract, fixture.after.ManagedAggregates()[0].Contribution().CanonicalContribution())
	if err != nil {
		t.Fatal(err)
	}
	after, err := durable.NewManagedAggregateState(before.Subject(), nextContribution)
	if err != nil {
		t.Fatal(err)
	}
	index, err := indexDocumentCaptureSubjects(t.Context(), fixture.before, selectionState(t, after))
	if err != nil {
		t.Fatal(err)
	}
	oldEdits := selectedCaptureEdits(t, fixture, aggregate.ExistingDocument([]byte(`{"model":true}`)), []aggregate.ProjectionContract{before.Contribution().Contract()}, nil)
	nextEdits := selectedCaptureEdits(t, fixture, aggregate.AbsentDocument(), []aggregate.ProjectionContract{nextContract}, []durable.ManagedAggregateState{after})
	for side, edits := range [][]compose.ProjectionEdit{oldEdits, nextEdits} {
		alias := []aggregate.DocumentAddress{fixture.claims.alias, nextAlias}[side]
		refs, err := index.selectedSubjects(t.Context(), edits, []aggregate.DocumentAddress{alias})
		if err != nil || len(refs) != 1 {
			t.Fatalf("retargeted side lost state-backed identity: %v", err)
		}
		subject, err := refs[0].canonical()
		if err != nil || subject != before.Subject() {
			t.Fatal("retargeting changed the canonical full subject identity")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if refs, err := index.selectedSubjects(ctx, oldEdits, []aggregate.DocumentAddress{fixture.claims.alias}); !errors.Is(err, context.Canceled) || refs != nil {
		t.Fatalf("canceled lookup returned partial references: %v", err)
	}
}

type selectedGroupCaptureFixture struct {
	roots     journalnamespace.Roots
	fixture   nativeRecordFixture
	namespace journalnamespace.DocumentNamespace
	input     selectedDocumentCaptureInput
	subjects  documentCaptureSubjects
	reader    *nativeNamespaceReader
	path      string
	original  aggregate.Document
}

func selectedCaptureFixture(t *testing.T, fixture nativeRecordFixture, original aggregate.Document, edits []compose.ProjectionEdit, order *pipackage.OrderObservation, roots journalnamespace.Roots) selectedGroupCaptureFixture {
	t.Helper()
	alias := fixture.claims.alias
	if roots.Manifest.Root == nil {
		roots.Manifest = nativeNamespaceRoot(t, t.TempDir())
	}
	root := roots.Manifest
	if alias.Scope() == target.ScopeGlobal {
		if alias.Target() == target.TargetPi {
			if roots.PiAgent.Root == nil {
				roots.PiAgent = nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "pi"))
			}
			root = roots.PiAgent
		} else {
			if roots.CodexHome.Root == nil {
				roots.CodexHome = nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "codex"))
			}
			root = roots.CodexHome
		}
	}
	path := filepath.Join(root.SelectedPath, filepath.FromSlash(alias.AggregateRoot().RelativePath()))
	if original.Exists() {
		requireWholeGroupWrite(t, path, original.Content(), 0o644)
	}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	budget, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, reader, budget)
	if err != nil {
		t.Fatal(err)
	}
	exact, known := namespace.Observation().Exact()
	if !known {
		t.Fatal("fixture path requires normalization-stable exact authority")
	}
	input := selectedDocumentCaptureInput{edits: edits, order: order, nativeDocumentCaptureInput: nativeDocumentCaptureInput{owner: fixture.claims.owner, operationID: fixture.record.OperationID, groupIndex: 2, limits: fixture.read.limits, budget: budget}}
	indexes, err := journaldocument.IndexSnapshots(t.Context(), fixture.before, fixture.after)
	if err != nil {
		t.Fatal(err)
	}
	var claims [2][]ownership.Claim
	var bindings [2]ownership.DocumentBindingValue
	for side, index := range []journaldocument.SnapshotIndex{indexes.Before(), indexes.After()} {
		footprints, err := index.Footprints(t.Context(), namespace.Addresses())
		if err != nil {
			t.Fatal(err)
		}
		for footprint := range footprints {
			claims[side] = append(claims[side], documentActiveClaim(t, documentClaimFixture{owner: input.owner, exact: exact}, footprint))
		}
		destinations, err := index.NativeDestinations(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, present := destinations[alias.AggregateRoot()]; present {
			binding, err := ownership.NewDocumentBinding(input.owner, alias.AggregateRoot(), exact)
			if err != nil {
				t.Fatal(err)
			}
			bindings[side], err = ownership.PresentDocumentBinding(binding)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(claims[0])+len(claims[1]) != 0 {
		transition, err := ownershipmutation.NewDocumentClaimTransition(claims[0], claims[1], input.operationID)
		if err != nil {
			t.Fatal(err)
		}
		input.claims = &transition
	}
	if !bindings[0].Equal(ownership.NoDocumentBinding()) || !bindings[1].Equal(ownership.NoDocumentBinding()) {
		intent, err := ownership.NewExactDocumentBindingIntent(input.owner, alias.AggregateRoot(), bindings[0], bindings[1])
		if err != nil {
			t.Fatal(err)
		}
		input.bindings = []ownership.DocumentBindingIntent{intent}
	}
	subjects, err := indexDocumentCaptureSubjects(t.Context(), fixture.before, fixture.after)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := roots.Manifest.Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	fixture.record.ManifestRootProvenance = persistedNativeRoot(provenance)
	return selectedGroupCaptureFixture{roots: roots, fixture: fixture, namespace: namespace, input: input, subjects: subjects, reader: reader, path: path, original: original}
}

func selectedCaptureEdits(t *testing.T, fixture nativeRecordFixture, original aggregate.Document, contracts []aggregate.ProjectionContract, desired []durable.ManagedAggregateState) []compose.ProjectionEdit {
	t.Helper()
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	codec, found := fixture.read.codecs.Lookup(selection.CodecContractID())
	if !found {
		t.Fatal("selected fixture codec missing")
	}
	baseline, failure := codec.Read(original, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	var intents []aggregate.ProjectionIntent
	for _, state := range baseline.States() {
		var values []aggregate.SubjectContribution
		for _, row := range desired {
			if row.Contribution().Contract().Equal(state.Contract()) {
				value, err := aggregate.NewSubjectContribution(row.Subject(), row.Contribution())
				if err != nil {
					t.Fatal(err)
				}
				values = append(values, value)
			}
		}
		var set *aggregate.ContributionSet
		if len(values) != 0 {
			value, err := aggregate.NewContributionSet(values)
			if err != nil {
				t.Fatal(err)
			}
			set = &value
		}
		intent, err := aggregate.NewProjectionIntent(state, set)
		if err != nil {
			t.Fatal(err)
		}
		intents = append(intents, intent)
	}
	plan, err := aggregate.NewPlan(baseline, intents)
	if err != nil {
		t.Fatal(err)
	}
	participant, err := journaldocument.Participant(selection, fixture.read.codecs)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := compose.NewProjectionEdit(codec, plan, participant.Syntax(), participant.RewriteAreas())
	if err != nil {
		t.Fatal(err)
	}
	return []compose.ProjectionEdit{edit}
}

func (capture selectedGroupCaptureFixture) requireCompleteStage(t *testing.T, group recoveryDocumentGroup, comparison journaldocument.Comparison) {
	t.Helper()
	fixture := capture.fixture
	fixture.record.DocumentGroups = []recoveryDocumentGroup{group}
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decoded.prepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.read.limits, fixture.read.codecs); err != nil {
		t.Fatal(err)
	}
	requireStagedDocumentJournal(t, fixture, decoded, []journaldocument.Comparison{comparison})
	actual, err := os.ReadFile(capture.path)
	if !capture.original.Exists() {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("selected capture/staging created host file: %v", err)
		}
		return
	}
	if err != nil || !slices.Equal(actual, capture.original.Content()) {
		t.Fatalf("selected capture/staging changed host file: %v", err)
	}
}
