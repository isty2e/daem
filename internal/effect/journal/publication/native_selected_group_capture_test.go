package publication

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	"github.com/isty2e/daem/internal/output"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
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
	subjects, err := capture.recordedGroup(t, group).SelectedSubjects(t.Context())
	if err != nil || len(subjects) != 1 || capture.reader.imageReads != 1 {
		t.Fatal("selected references expanded to all membership or original was not read once")
	}
	if subjects[0] != selected.Subject() {
		t.Fatal("capture lost full actual subject identity")
	}
	expectedMode, present := comparison.ExpectedState().RegularFileMode()
	if comparison.BeforeMode() == nil || *comparison.BeforeMode() != 0o644 || !present || expectedMode != 0o644 {
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
	expectedMode, present := comparison.ExpectedState().RegularFileMode()
	if comparison.BeforeDocument().Exists() || !present || expectedMode != 0o600 {
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
	fixture.read.Codecs, err = aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{after.Contribution().CodecContractID(), mcpRows[1].Contribution().CodecContractID()}, fixture.read.Limits, fixture.read.Limits)
	if err != nil {
		t.Fatal(err)
	}
	fixture.codec, err = statefile.NewNativeCodec(fixture.read.Codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	original := aggregate.ExistingDocument([]byte("model=true\n[mcp_servers.search]\ncommand='node'\n[mcp_servers.foreign]\ncommand='foreign' # retained\n"))
	edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{after.Contribution().Contract()}, []durable.ManagedAggregateState{after})
	edits = append(edits, selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{mcpRows[1].Contribution().Contract()}, mcpRows[1:])...)
	capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil {
		t.Fatal(err)
	}
	subjects, err := capture.recordedGroup(t, group).SelectedSubjects(t.Context())
	if err != nil || len(subjects) != 2 {
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
	codec, available := fixture.read.Codecs.Lookup(selection.CodecContractID())
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
	if err != nil {
		t.Fatal(err)
	}
	subjects, err := capture.recordedGroup(t, group).SelectedSubjects(t.Context())
	if err != nil || len(subjects) != 0 || !comparison.ExpectedDocument().Equal(original) {
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
			roots := namespacetest.LegacyRoots(t, map[output.RootRole]journalnamespace.RootSelection{output.RootProject: nativeNamespaceRoot(t, command.BaseRoot())})
			if scope == target.ScopeGlobal {
				roots = namespacetest.WithLegacyRoot(t, roots, output.RootPiAgent, nativeNamespaceRoot(t, loaded.SettingsContext.BaseRoot()))
			}
			fixture := nativeJournalFixture(t)
			fixture.claims.alias, _ = journalrecord.PiOrderDocumentAlias(scope)
			fixture.before, fixture.after = durable.EmptySnapshot(), durable.EmptySnapshot()
			capture := selectedCaptureFixture(t, fixture, loaded.Document, nil, &order, roots)
			group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
			if err != nil {
				t.Fatal(err)
			}
			recorded := capture.recordedGroup(t, group)
			_, hasOrder, err := recorded.PiQualification()
			if err != nil || !hasOrder || !comparison.Changed() {
				t.Fatalf("real Pi-only group failed: %v", err)
			}
			subjects, err := recorded.SelectedSubjects(t.Context())
			if err != nil || len(subjects) != 0 {
				t.Fatal("Pi-only group invented selected subjects", err)
			}
			expectedMode, present := comparison.ExpectedState().RegularFileMode()
			if comparison.BeforeMode() == nil || *comparison.BeforeMode() != 0o644 || !present || expectedMode != 0o644 {
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
			if err != nil {
				t.Fatal(err)
			}
			_, hasOrder, err = capture.recordedGroup(t, group).PiQualification()
			if err != nil || hasOrder || comparison.Changed() || !comparison.ExpectedDocument().Equal(loaded.Document) {
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
	roots := namespacetest.LegacyRoots(t, map[output.RootRole]journalnamespace.RootSelection{output.RootProject: nativeNamespaceRoot(t, command.BaseRoot()), output.RootPiAgent: nativeNamespaceRoot(t, loaded.SettingsContext.BaseRoot())})
	fixture := nativeJournalFixture(t)
	fixture.claims.alias, _ = journalrecord.PiOrderDocumentAlias(target.ScopeGlobal)
	before := membershipNativeRow(t, fixture.claims.alias, "native-json-v1", "theme")
	before = selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, `"day"`))
	after := selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, `"night"`))
	fixture.before, fixture.after = selectionState(t, before), selectionState(t, after)
	edits := selectedCaptureEdits(t, fixture, loaded.Document, []aggregate.ProjectionContract{after.Contribution().Contract()}, []durable.ManagedAggregateState{after})
	capture := selectedCaptureFixture(t, fixture, loaded.Document, edits, &order, roots)
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err != nil {
		t.Fatal(err)
	}
	recorded := capture.recordedGroup(t, group)
	subjects, err := recorded.SelectedSubjects(t.Context())
	if err != nil || len(subjects) != 1 {
		t.Fatal("native/Pi group lost its selected subject", err)
	}
	_, hasOrder, err := recorded.PiQualification()
	if err != nil || !hasOrder || !comparison.Changed() {
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
	if err != nil {
		t.Fatal(err)
	}
	subjects, err := capture.recordedGroup(t, group).SelectedSubjects(t.Context())
	if err != nil || len(subjects) != 1 {
		t.Fatalf("whole-to-key capture failed: %v", err)
	}
	if subjects[0] == wholeSubject || subjects[0] != selected.Subject() {
		t.Fatal("whole membership leaked into the selected frontier")
	}
	capture.requireCompleteStage(t, group, comparison)
}

func TestSelectedGroupCaptureRejectsWrongPiCommandRootBeforeRead(t *testing.T) {
	loaded, order := piEvidenceFixture(t, target.ScopeGlobal)
	roots := namespacetest.LegacyRoots(t, map[output.RootRole]journalnamespace.RootSelection{output.RootProject: nativeNamespaceRoot(t, t.TempDir()), output.RootPiAgent: nativeNamespaceRoot(t, loaded.SettingsContext.BaseRoot())})
	fixture := nativeJournalFixture(t)
	var err error
	fixture.claims.alias, err = journalrecord.PiOrderDocumentAlias(target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	fixture.before, fixture.after = durable.EmptySnapshot(), durable.EmptySnapshot()
	capture := selectedCaptureFixture(t, fixture, loaded.Document, nil, &order, roots)
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
	if err == nil || !reflect.ValueOf(group).IsZero() || comparison.Validate() == nil || capture.reader.imageReads != 0 {
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
				capture.namespace, err = journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{other}, capture.roots, capture.reader, capture.input.Budget())
				if err != nil {
					t.Fatal(err)
				}
			case "zero edit":
				capture.input.edits = []compose.ProjectionEdit{{}}
			case "wrong owner":
				capture.input.CaptureInput = reviseCaptureInput(capture.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Owner = documentClaimsForTest(t).owner })
			case "wrong Pi alias":
				_, order := piEvidenceFixture(t, target.ScopeGlobal)
				capture.input.order = &order
			}
			group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
			if err == nil || !reflect.ValueOf(group).IsZero() || comparison.Validate() == nil || capture.reader.imageReads != 0 {
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
				capture.input.CaptureInput = reviseCaptureInput(capture.input.CaptureInput, func(spec *journalobservation.CaptureInputSpec) { spec.Limits.MaximumBytes = 2 })
			}
			group, comparison, err := prepareSelectedDocumentGroup(ctx, capture.namespace, capture.input, capture.subjects)
			if err == nil || !reflect.ValueOf(group).IsZero() || comparison.Validate() == nil {
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
		if refs[0] != before.Subject() {
			t.Fatal("retargeting changed the canonical full subject identity")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if refs, err := index.selectedSubjects(ctx, oldEdits, []aggregate.DocumentAddress{fixture.claims.alias}); !errors.Is(err, context.Canceled) || refs != nil {
		t.Fatalf("canceled lookup returned partial references: %v", err)
	}
}

func (capture selectedGroupCaptureFixture) requireCompleteStage(t *testing.T, group journalrecord.SealedDocumentGroup, comparison journaldocument.Comparison) journalrecord.Native {
	t.Helper()
	fixture := capture.fixture
	journal := capture.recordForGroups(t, []journalrecord.SealedDocumentGroup{group})
	if _, err := journal.Encode(t.Context(), fixture.codec); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.read.Limits, fixture.read.Codecs); err != nil {
		t.Fatal(err)
	}
	decoded := requireStagedDocumentJournal(t, fixture, journal, []journaldocument.Comparison{comparison})
	authority, err := namespacetest.LegacyRoot(t, capture.roots, output.RootProject).Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.MatchesManifestRoot(provenance) || !decoded.MatchesContext(capture.input.Owner(), fixture.before, fixture.after) || decoded.OperationID() != capture.input.OperationID() {
		t.Fatal("selected publication retained a foreign template header")
	}
	actual, err := os.ReadFile(capture.path)
	if !capture.original.Exists() {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("selected capture/staging created host file: %v", err)
		}
		return decoded
	}
	if err != nil || !slices.Equal(actual, capture.original.Content()) {
		t.Fatalf("selected capture/staging changed host file: %v", err)
	}
	return decoded
}

func TestNativeSelectedRemovalUsesActualEditsNotObservedAliases(t *testing.T) {
	fixture := nativeJournalFixture(t)
	fixture.before = durable.EmptySnapshot()
	selected := fixture.after.ManagedAggregates()[0]
	original := aggregate.AbsentDocument()
	edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{selected.Contribution().Contract()}, []durable.ManagedAggregateState{selected})
	capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
	roots := capture.roots
	if err := os.Symlink(namespacetest.LegacyRoot(t, roots, output.RootPiAgent).SelectedPath, filepath.Join(namespacetest.LegacyRoot(t, roots, output.RootProject).SelectedPath, ".pi")); err != nil {
		t.Fatal(err)
	}
	observedAlias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/"+fixture.claims.alias.AggregateRoot().RelativePath())
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{fixture.claims.alias, observedAlias}, roots, capture.reader, capture.input.Budget())
	if err != nil {
		t.Fatal(err)
	}
	group, comparison, err := prepareSelectedDocumentGroup(t.Context(), namespace, capture.input, capture.subjects)
	if err != nil {
		t.Fatal(err)
	}
	recorded := capture.recordedGroup(t, group)
	removal, present, err := recorded.Removal()
	if err != nil || !present || len(recorded.Aliases()) != 2 || removal.Demands().Len() != 1 {
		t.Fatalf("observed alias gained removal debt: %v", err)
	}
	demand := removal.Demands().Demands()[0]
	if demand.Scope() != fixture.claims.alias.Scope() || demand.Destination() != fixture.claims.alias.AggregateRoot() {
		t.Fatal("actual edit relation was replaced by the observed alias")
	}
	capture.namespace = namespace
	baseline := capture.requireCompleteStage(t, group, comparison)
	prepared, err := baseline.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.read.Limits, fixture.read.Codecs)
	if err != nil {
		t.Fatalf("valid selected removal failed before its negative control: %v", err)
	}
	if comparisons, err := prepared.Comparisons(); err != nil || len(comparisons) != 1 {
		t.Fatal("valid selected removal lost complete preparation", err)
	}
	variant := capture.fixture
	variant.journal = baseline
	variant.read.Artifacts = nativeNamespaceRoot(t, t.TempDir()).Root
	decoded := publishedNativeGroupVariant(t, variant, func(group map[string]json.RawMessage) {
		var removal map[string]json.RawMessage
		if err := json.Unmarshal(group["removal"], &removal); err != nil {
			t.Fatal(err)
		}
		var demands []map[string]json.RawMessage
		if err := json.Unmarshal(removal["demands"], &demands); err != nil {
			t.Fatal(err)
		}
		if len(demands) != 1 {
			t.Fatal("selected removal variant requires one actual relation")
		}
		demands = append(demands, map[string]json.RawMessage{
			"scope":       nativeRecordJSON(t, string(observedAlias.Scope())),
			"destination": nativeRecordJSON(t, observedAlias.AggregateRoot().String()),
		})
		removal["demands"] = nativeRecordJSON(t, demands)
		group["removal"] = nativeRecordJSON(t, removal)
	})
	authority, err := namespacetest.LegacyRoot(t, capture.roots, output.RootProject).Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.MatchesManifestRoot(provenance) {
		t.Fatal("removal variant changed its captured manifest")
	}
	result, err := decoded.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.read.Limits, fixture.read.Codecs)
	assertNoPreparedDocumentRecord(t, result, err, fixture.codec)
}

func TestNativeSelectedLastKeyReleasePreservesExistingRoot(t *testing.T) {
	for _, content := range []string{`{"model":true}`, `{"model":true,"foreign":1}`} {
		t.Run(content, func(t *testing.T) {
			fixture := nativeJournalFixture(t)
			before := fixture.before.ManagedAggregates()[0]
			fixture.after = durable.EmptySnapshot()
			original := aggregate.ExistingDocument([]byte(content))
			edits := selectedCaptureEdits(t, fixture, original, []aggregate.ProjectionContract{before.Contribution().Contract()}, nil)
			capture := selectedCaptureFixture(t, fixture, original, edits, nil, journalnamespace.Roots{})
			group, comparison, err := prepareSelectedDocumentGroup(t.Context(), capture.namespace, capture.input, capture.subjects)
			if err != nil {
				t.Fatal(err)
			}
			_, hasRemoval, err := capture.recordedGroup(t, group).Removal()
			_, regular := comparison.ExpectedState().RegularFileMode()
			if err != nil || !comparison.ExpectedDocument().Exists() || !regular || hasRemoval {
				t.Fatalf("last selected-key release acquired root deletion: %v", err)
			}
			var remaining map[string]json.RawMessage
			if err := json.Unmarshal(comparison.ExpectedDocument().Content(), &remaining); err != nil {
				t.Fatal(err)
			}
			if _, retained := remaining["model"]; retained {
				t.Fatal("actual edit did not release the selected key")
			}
			capture.requireCompleteStage(t, group, comparison)
		})
	}
}
