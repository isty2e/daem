package journal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestNativeDocumentAssessmentMixedSelectedValuesBlockAfterOnly(t *testing.T) {
	for _, committed := range []bool{false, true} {
		setup := newNativeAssessmentFixture(t)
		fixture := &setup.fixture
		beforeAdded := selectionNativeRow(t, stateGroupContract(t, fixture.claims.alias, "other"), nativeRecordCanonical(t, "0"))
		afterAdded := selectionNativeRow(t, beforeAdded.Contribution().Contract(), nativeRecordCanonical(t, "2"))
		fixture.before = selectionState(t, fixture.before.ManagedAggregates()[0], beforeAdded)
		fixture.after = selectionState(t, fixture.after.ManagedAggregates()[0], afterAdded)
		nativeRecordImages(t, fixture, aggregate.ExistingDocument([]byte(`{"model":true,"other":0,"outside":1}`)), aggregate.ExistingDocument([]byte(`{"model":"fast","other":2,"outside":1}`)))
		fixture.record.DocumentGroups[0].SelectedSubjects = append(fixture.record.DocumentGroups[0].SelectedSubjects, persistedSubjectRef{Kind: string(afterAdded.Subject().Kind()), Namespace: afterAdded.Subject().Namespace(), Name: afterAdded.Subject().Key()})
		claims := []ownership.Claim{documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "model")), documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "other"))}
		transition, err := ownershipmutation.NewDocumentClaimTransition(claims, claims, fixture.record.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		fixture.record.DocumentGroups[0].ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		binding, _ := membershipBindingValue(t, fixture.claims).Get()
		registry := assessmentRegistry(t, claims, binding)
		state := fixture.before
		if committed {
			state = fixture.after
		}
		host := []byte(`{"model":"fast","other":0,"outside":1}`)
		setup.persist(t, state, registry, host)
		journal, options := setup.reader(t)
		result, err := journal.assessDocumentRecovery(t.Context(), options)
		if committed {
			if err == nil || result.stages != nil {
				t.Fatal("committed state admitted a mixed before/expected host")
			}
		} else if err != nil || result.disposition != nativeDocumentsRollback || !result.stages[0].restoration.Changed() || result.stages[0].expected {
			t.Fatalf("before-state mixed document did not stage rollback: %v", err)
		}
		setup.assertPersisted(t, state, registry, host)
	}
}

func TestNativeDocumentAssessmentLateRefusalDiscardsEveryEarlierStage(t *testing.T) {
	setup := newNativeAssessmentFixture(t)
	fixture := &setup.fixture
	otherPath := filepath.Join(setup.roots.PiAgent.SelectedPath, "other.json")
	invalid := []byte(`{"model":false,"outside":1}`)
	if err := os.WriteFile(otherPath, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/other.json")
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{alias}, setup.roots, fixture.read.filesystem, fixture.read.budget)
	if err != nil {
		t.Fatal(err)
	}
	exact, found := namespace.observation.Exact()
	if !found {
		t.Fatal("second real file did not provide exact evidence")
	}
	id, err := entity.New(entity.KindHostConfig, "other-document")
	if err != nil {
		t.Fatal(err)
	}
	key := stateGroupContract(t, alias, "model")
	subject, err := topologyprojection.HostConfigKeySubject(id, alias.Target(), alias.Scope(), key.Address().KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	beforeRow, err := durable.NewManagedAggregateState(subject, selectionNativeRow(t, key, nativeRecordCanonical(t, "true")).Contribution())
	if err != nil {
		t.Fatal(err)
	}
	afterRow, err := durable.NewManagedAggregateState(subject, selectionNativeRow(t, key, nativeRecordCanonical(t, `"fast"`)).Contribution())
	if err != nil {
		t.Fatal(err)
	}
	fixture.before = selectionState(t, fixture.before.ManagedAggregates()[0], beforeRow)
	fixture.after = selectionState(t, fixture.after.ManagedAggregates()[0], afterRow)
	second := nativeNamespaceGroupFacts(t, fixture.record.DocumentGroups[0], namespace)
	second.SelectedSubjects = []persistedSubjectRef{{Kind: string(subject.Kind()), Namespace: subject.Namespace(), Name: subject.Key()}}
	claims := fixture.claims
	claims.exact, claims.alias = exact, alias
	otherClaim := documentActiveClaim(t, claims, documentKeyFootprint(t, "model"))
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{otherClaim}, []ownership.Claim{otherClaim}, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	second.ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, claims)
	second.BindingIntents = []recoveryDocumentBindingIntent{assessmentBinding(t, membershipIntent(t, claims, value, value))}
	fixture.record.DocumentGroups = append(fixture.record.DocumentGroups, second)
	firstClaim := documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "model"))
	firstBinding, _ := membershipBindingValue(t, fixture.claims).Get()
	secondBinding, _ := value.Get()
	registry := assessmentRegistry(t, []ownership.Claim{firstClaim, otherClaim}, firstBinding, secondBinding)
	host := assessmentImage(t, *fixture, true)
	setup.persist(t, fixture.before, registry, host)
	journal, options := setup.reader(t)
	result, err := journal.assessDocumentRecovery(t.Context(), options)
	if err == nil || result.stages != nil || result.disposition != "" || result.qualified.records.groups != nil {
		t.Fatal("late third-value refusal exposed a staged prefix")
	}
	setup.assertPersisted(t, fixture.before, registry, host)
	actual, err := os.ReadFile(otherPath)
	if err != nil || !bytes.Equal(actual, invalid) {
		t.Fatalf("late-refused document was modified: %v", err)
	}
}

func TestNativeDocumentAssessmentProvisionalDebtDoesNotAuthorizeAfterState(t *testing.T) {
	setup := newNativeAssessmentFixture(t)
	fixture := &setup.fixture
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeGlobal, "@pi-agent/Caf\u00e9.json")
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{alias}, setup.roots, fixture.read.filesystem, fixture.read.budget)
	if err != nil {
		t.Fatal(err)
	}
	candidate, provisional := namespace.observation.Provisional()
	if !provisional {
		t.Skip("this filesystem observes the missing Unicode destination as exact")
	}
	fixture.claims.alias = alias
	fixture.before = durable.EmptySnapshot()
	row := selectionNativeRow(t, stateGroupContract(t, alias, "model"), nativeRecordCanonical(t, `"fast"`))
	fixture.after = selectionState(t, row)
	nativeRecordImages(t, fixture, aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{"model":"fast"}`)))
	group := &fixture.record.DocumentGroups[0]
	*group = nativeNamespaceGroupFacts(t, *group, namespace)
	fixtureDocumentRemoval(t, group, aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{"model":"fast"}`)))
	group.SelectedSubjects = []persistedSubjectRef{{Kind: string(row.Subject().Kind()), Namespace: row.Subject().Namespace(), Name: row.Subject().Key()}}
	intent, err := ownership.NewProvisionalAcquireIntent(alias.AggregateRoot(), documentKeyFootprint(t, "model"), candidate, fixture.claims.owner, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	group.ExclusiveClaims, err = persistedDocumentProvisionalClaims([]documentProvisionalAcquire{{alias: alias, intent: intent}})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewProvisionalDocumentBindingIntent(fixture.claims.owner, alias.AggregateRoot(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	group.BindingIntents = []recoveryDocumentBindingIntent{assessmentBinding(t, binding)}
	setup.host = filepath.Join(setup.roots.PiAgent.SelectedPath, "Caf\u00e9.json")
	host := []byte(`{"model":"fast"}`)
	registry := ownership.EmptyRegistry()
	setup.persist(t, fixture.before, registry, host)
	journal, options := setup.reader(t)
	result, err := journal.assessDocumentRecovery(t.Context(), options)
	if err != nil || result.disposition != nativeDocumentsRollback || result.stages[0].restoration.Document().Exists() {
		t.Fatalf("before-side provisional crash did not stage guarded deletion: %v", err)
	}
	setup.assertPersisted(t, fixture.before, registry, host)
	setup.persist(t, fixture.after, registry, host)
	journal, options = setup.reader(t)
	if result, err := journal.assessDocumentRecovery(t.Context(), options); err == nil || result.stages != nil {
		t.Fatal("unpromoted claim/binding debt became after-side authority")
	}
	setup.assertPersisted(t, fixture.after, registry, host)
}

func TestNativeDocumentAssessmentEqualSnapshotsKeepPiOnlyRollback(t *testing.T) {
	setup := newNativeAssessmentFixture(t)
	fixture := &setup.fixture
	input, order := piEvidenceFixture(t, target.ScopeProject)
	context, known := input.Relations[0].SourceContext()
	if !known {
		t.Fatal("Pi relation lost its captured context")
	}
	root := nativeNamespaceRoot(t, context.BaseRoot())
	if err := os.Mkdir(filepath.Join(root.SelectedPath, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	expected, err := order.Project(input.Document)
	if err != nil {
		t.Fatal(err)
	}
	nativeRecordImages(t, fixture, input.Document, expected)
	setup.roots = NativeDocumentRoots{Manifest: root}
	setup.host = filepath.Join(root.SelectedPath, ".pi", "settings.json")
	if err := os.WriteFile(setup.host, input.Document.Content(), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/settings.json")
	namespace, err := ObserveNativeDocumentNamespace(t.Context(), []aggregate.DocumentAddress{alias}, setup.roots, fixture.read.filesystem, fixture.read.budget)
	if err != nil {
		t.Fatal(err)
	}
	fixture.record.ManifestRootProvenance = persistedNativeRoot(namespace.aliases[0].root.provenance)
	group := &fixture.record.DocumentGroups[0]
	*group = nativeNamespaceGroupFacts(t, *group, namespace)
	group.SelectedSubjects, group.BindingIntents = []persistedSubjectRef{}, []recoveryDocumentBindingIntent{}
	group.ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
	evidence, err := persistedPiOrderEvidence(order)
	if err != nil {
		t.Fatal(err)
	}
	group.PiOrder = &recoveryDocumentPiOrder{Alias: persistedDocumentAlias(alias), CommandRoot: recoveryDocumentLogicalRoot{Role: "manifest-root", Provenance: fixture.record.ManifestRootProvenance}, Evidence: evidence}
	fixture.before, fixture.after = durable.EmptySnapshot(), durable.EmptySnapshot()
	for _, after := range []bool{false, true} {
		host, want := input.Document.Content(), nativeDocumentsRollback
		if after {
			host, want = expected.Content(), nativeDocumentsAfter
		}
		setup.persist(t, fixture.before, ownership.EmptyRegistry(), host)
		journal, options := setup.reader(t)
		result, err := journal.assessDocumentRecovery(t.Context(), options)
		if err != nil || result.disposition != want || result.stages[0].restoration.Changed() != after {
			t.Fatalf("equal Snapshot Pi phase=%s want=%s: %v", result.disposition, want, err)
		}
		setup.assertPersisted(t, fixture.before, ownership.EmptyRegistry(), host)
	}
}

func TestNativeDocumentAssessmentRestoresRemovedWholeFileBeforeCommit(t *testing.T) {
	setup := newNativeAssessmentFixture(t)
	fixture := &setup.fixture
	original := aggregate.ExistingDocument([]byte("opaque actual original\x00\xff\n"))
	nativeRecordImages(t, fixture, original, aggregate.AbsentDocument())
	whole := nativeWholeFileJournalBaseline(t, target.TargetPi, target.ScopeGlobal)
	row, err := durable.NewManagedPathState(whole.Subject(), whole.ConsumerTargets(), whole.Scope(), fixture.claims.alias.AggregateRoot(), artifact.HashFileContent(original.Content()), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	fixture.before, err = durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{row}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.after = durable.EmptySnapshot()
	group := &fixture.record.DocumentGroups[0]
	group.Extent, group.SelectedSubjects = "whole", []persistedSubjectRef{}
	claim := documentActiveClaim(t, fixture.claims, output.WholePathFootprint())
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, nil, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	group.ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	beforeBinding := membershipBindingValue(t, fixture.claims)
	group.BindingIntents = []recoveryDocumentBindingIntent{assessmentBinding(t, membershipIntent(t, fixture.claims, beforeBinding, ownership.NoDocumentBinding()))}
	binding, _ := beforeBinding.Get()
	registry := assessmentRegistry(t, []ownership.Claim{claim}, binding)
	setup.persist(t, fixture.before, registry, original.Content())
	if err := os.Remove(setup.host); err != nil {
		t.Fatal(err)
	}
	journal, options := setup.reader(t)
	result, err := journal.assessDocumentRecovery(t.Context(), options)
	if err != nil || result.disposition != nativeDocumentsRollback || !result.stages[0].restoration.Document().Equal(original) {
		t.Fatalf("missing whole file did not stage its opaque actual original: %v", err)
	}
	if _, err := os.Stat(setup.host); !os.IsNotExist(err) {
		t.Fatalf("read-only staging recreated the removed host: %v", err)
	}
}
