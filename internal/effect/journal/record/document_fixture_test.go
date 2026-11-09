package record

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type documentClaimFixture struct {
	owner       stateauthority.Authority
	exact       pathauthority.Exact
	provisional pathauthority.Provisional
	alias       aggregate.DocumentAddress
}

type nativeRecordFixture struct {
	record             nativeJournalRecord
	before, after      durable.Snapshot
	codec              durable.SnapshotCodec
	codecs             aggregate.CodecCatalog
	limits             configdocument.Limits
	claims             documentClaimFixture
	original, expected aggregate.Document
}

func (fixture nativeRecordFixture) nativeValue(t *testing.T) Native {
	t.Helper()
	value, err := newNative16Value(fixture.record, fixture.claims.owner, fixture.before, fixture.after)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func nativeJournalFixture(t *testing.T) nativeRecordFixture {
	t.Helper()
	claims := documentClaimsForTest(t)
	beforeRow := membershipNativeRow(t, claims.alias, "native-json-v1", "model")
	beforeRow = selectionNativeRow(t, beforeRow.Contribution().Contract(), nativeRecordCanonical(t, "true"))
	afterRow := selectionNativeRow(t, beforeRow.Contribution().Contract(), nativeRecordCanonical(t, `"fast"`))
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	codecs, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{beforeRow.Contribution().CodecContractID()}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := rootedpath.NewAuthorityProvenance(filepath.Join(t.TempDir(), "record-root"), testContentHashString("record object"), testContentHashString("record mount"))
	if err != nil {
		t.Fatal(err)
	}
	root := persistedNativeRoot(provenance)
	observation := recoveryDocumentObservation{Kind: "exact", Authority: persistedPathAuthority(claims.exact)}
	fixture := nativeRecordFixture{
		record: nativeJournalRecord{
			Version: NativeVersion, Operation: OperationApply, OperationID: "native-record-apply", CreatedAt: "2026-01-01T00:00:00Z",
			StateAuthority:         recoveryStateAuthority{StatefileAuthority: *persistedPathAuthority(claims.owner.StatefileAuthority()), ManifestPath: claims.owner.ManifestPath()},
			ManifestRootProvenance: root, Entries: []recoveryEntry{}, ClaimTransitions: []recoveryClaimTransition{}, ProvisionalAcquires: []recoveryProvisionalAcquireIntent{}, RemovalIntents: []recoveryRemovalIntent{},
			DocumentGroups: []recoveryDocumentGroup{{
				CapturedTarget: observation, Writer: recoveryDocumentWriter{Root: root, RelativePath: "host/custom.json"},
				Aliases: []recoveryDocumentAlias{{Address: persistedDocumentAlias(claims.alias), LogicalRoot: recoveryDocumentLogicalRoot{Role: "global-root", Provenance: root}, SelectedObservation: observation, ObservedBefore: observation}},
				Extent:  "selected",
			}},
		},
		codec: codec, codecs: codecs, limits: limits, claims: claims,
	}
	fixture.withSelectedRows(t, beforeRow, afterRow)
	fixture.withImages(t, aggregate.ExistingDocument([]byte(`{"model":true,"outside":1}`)), aggregate.ExistingDocument([]byte(`{"model":"fast","outside":1}`)))
	return fixture
}

func (fixture *nativeRecordFixture) withSelectedRows(t *testing.T, before, after durable.ManagedAggregateState) {
	t.Helper()
	fixture.before, fixture.after = selectionState(t, before), selectionState(t, after)
	group := &fixture.record.DocumentGroups[0]
	group.SelectedSubjects = []persistedSubjectRef{{Kind: string(before.Subject().Kind()), Namespace: before.Subject().Namespace(), Name: before.Subject().Key()}}
	claim := documentActiveClaim(t, fixture.claims, before.Contribution().Contract().Address().Footprint())
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	group.ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, fixture.claims)
	binding, err := persistedDocumentBindingIntent(membershipIntent(t, fixture.claims, value, value))
	if err != nil {
		t.Fatal(err)
	}
	group.BindingIntents = []recoveryDocumentBindingIntent{binding}
}

func nativeCreateFixture(t *testing.T) nativeRecordFixture {
	t.Helper()
	fixture := nativeJournalFixture(t)
	fixture.before = durable.EmptySnapshot()
	after := fixture.after.ManagedAggregates()[0]
	group := &fixture.record.DocumentGroups[0]
	group.SelectedSubjects = []persistedSubjectRef{{Kind: string(after.Subject().Kind()), Namespace: after.Subject().Namespace(), Name: after.Subject().Key()}}
	claim := documentActiveClaim(t, fixture.claims, after.Contribution().Contract().Address().Footprint())
	transition, err := ownershipmutation.NewDocumentClaimTransition(nil, []ownership.Claim{claim}, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	group.ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, fixture.claims)
	binding, err := persistedDocumentBindingIntent(membershipIntent(t, fixture.claims, ownership.NoDocumentBinding(), value))
	if err != nil {
		t.Fatal(err)
	}
	group.BindingIntents = []recoveryDocumentBindingIntent{binding}
	fixture.withImages(t, aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{"model":"fast"}`)))
	return fixture
}

func (fixture *nativeRecordFixture) withImages(t *testing.T, original, expected aggregate.Document) {
	t.Helper()
	fixture.original, fixture.expected = original, expected
	group := &fixture.record.DocumentGroups[0]
	for index, document := range []aggregate.Document{original, expected} {
		reference := []string{"images/before", "images/expected"}[index]
		var mode *recovery.PermissionMode
		if document.Exists() {
			mode = recovery.NewPermissionMode(0o600)
		} else {
			reference = ""
		}
		image, err := persistedDocumentImage(document, mode, reference)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			group.Original = image
		} else {
			group.Expected = image
		}
	}
	fixtureDocumentRemoval(t, group, original, expected)
}

func fixtureDocumentRemoval(t *testing.T, group *recoveryDocumentGroup, before, expected aggregate.Document) {
	t.Helper()
	states, err := journaldocument.RemovalStates(before, group.Original.Mode, expected, group.Expected.Mode)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) == 0 {
		group.Removal = nil
		return
	}
	demands := make([]recovery.RemovalDemand, 0, len(group.Aliases))
	for _, alias := range group.Aliases {
		address, err := alias.Address.canonical()
		if err != nil {
			t.Fatal(err)
		}
		demand, err := recovery.NewRemovalDemand(address.Scope(), address.AggregateRoot(), states)
		if err != nil {
			t.Fatal(err)
		}
		demands = append(demands, demand)
	}
	set, err := recovery.NewRemovalDemandSet(demands)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(filepath.Join(group.Writer.Root.PhysicalRoot, filepath.FromSlash(group.Writer.RelativePath)))
	root, err := recovery.NewRootProvenance(parent, testContentHashString("record writer parent"), group.Writer.Root.MountFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	names, err := mutationfs.NewLogicalRemovalNames(".daem-tombstone-00000000000000000000000000000001", ".daem-cleanup-00000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := recovery.NewExistingParentAuthority(root, names)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := recovery.NewDocumentRemoval(set, namespace)
	if err != nil {
		t.Fatal(err)
	}
	record, err := persistedDocumentRemoval(removal)
	if err != nil {
		t.Fatal(err)
	}
	group.Removal = &record
}

func documentClaimsForTest(t *testing.T) documentClaimFixture {
	t.Helper()
	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	exact := pathtest.DarwinCaseSensitive(filepath.Join(root, "host", "Caf\u00e9.json"))
	namespace := pathtest.DarwinCaseSensitive(root)
	provisional, err := pathauthority.NewProvisional(exact.Key(), exact.Witness(), namespace.Key(), namespace.Witness())
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("@pi-agent/custom.json")
	if err != nil {
		t.Fatal(err)
	}
	id, found := aggregate.HostConfigCodecForSyntax("json")
	if !found {
		t.Fatal("native JSON codec is unavailable")
	}
	key, err := output.NewKeyPath("selected")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, destination, footprint, id)
	if err != nil {
		t.Fatal(err)
	}
	return documentClaimFixture{owner: owner, exact: exact, provisional: provisional, alias: contract.Address().Document()}
}

func documentKeyFootprint(t *testing.T, parts ...string) output.Footprint {
	t.Helper()
	key, err := output.NewKeyPath(parts...)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	return footprint
}

func documentActiveClaim(t *testing.T, fixture documentClaimFixture, footprint output.Footprint) ownership.Claim {
	t.Helper()
	address, err := ownership.NewManagedAddress(fixture.exact, footprint)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, fixture.owner)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func nativeRecordJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func nativeRecordCanonical(t *testing.T, raw string) string {
	t.Helper()
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	document, err := configdocument.Parse(t.Context(), []byte(`{"value":`+raw+`}`), configdocument.JSON, limits)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("value")
	if err != nil {
		t.Fatal(err)
	}
	value, found, err := document.Lookup(key)
	if err != nil || !found {
		t.Fatalf("canonical fixture value: %v", err)
	}
	canonical, err := value.CanonicalContext(t.Context(), limits)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func preparedNativeFixture(t *testing.T) (nativeRecordFixture, Native, journaldocument.Comparison) {
	t.Helper()
	fixture := nativeJournalFixture(t)
	return preparedNativeFixtureFor(t, fixture, aggregate.ExistingDocument([]byte(`{"model":true,"outside":1}`)))
}

func preparedParticipant(t *testing.T, fixture nativeRecordFixture) compose.Participant {
	t.Helper()
	contract := fixture.after.ManagedAggregates()[0].Contribution().Contract()
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := journaldocument.Participant(selection, fixture.codecs)
	if err != nil {
		t.Fatal(err)
	}
	return participant
}

func preparedComparisonInput(comparison journaldocument.Comparison) journaldocument.ComparisonInput {
	var expectedMode *recovery.PermissionMode
	if mode, present := comparison.ExpectedState().RegularFileMode(); present {
		expectedMode = &mode
	}
	return journaldocument.ComparisonInput{Before: comparison.BeforeDocument(), Expected: comparison.ExpectedDocument(), BeforeMode: comparison.BeforeMode(), ExpectedMode: expectedMode, Limits: comparison.Limits()}
}

func preparedNativeFixtureFor(t *testing.T, fixture nativeRecordFixture, before aggregate.Document) (nativeRecordFixture, Native, journaldocument.Comparison) {
	t.Helper()
	row := fixture.after.ManagedAggregates()[0]
	contract := row.Contribution().Contract()
	codec, found := fixture.codecs.Lookup(contract.CodecContractID())
	if !found {
		t.Fatal("fixture codec missing")
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	baseline, failure := codec.Read(before, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	item, err := aggregate.NewSubjectContribution(row.Subject(), row.Contribution())
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := aggregate.NewProjectionIntent(baseline.States()[0], &set)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(baseline, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := compose.NewProjectionEdit(codec, plan, configdocument.JSON, []output.KeyPath{contract.Address().KeyPath()})
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.NewPermissionMode(0o600)
	var beforeMode *recovery.PermissionMode
	if before.Exists() {
		beforeMode = mode
	}
	candidate, err := journaldocument.PrepareSelected(t.Context(), journaldocument.SelectedPreparationInput{Before: before, BeforeMode: beforeMode, ExpectedMode: mode, Limits: fixture.limits}, []compose.ProjectionEdit{edit}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fixture.withImages(t, before, candidate.Document())
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, decoded, candidate.Comparison()
}

func selectionNativeRow(t *testing.T, contract aggregate.ProjectionContract, value string) durable.ManagedAggregateState {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, "native")
	if err != nil {
		t.Fatal(err)
	}
	address := contract.Address()
	subject, err := topologyprojection.HostConfigKeySubject(id, address.Document().Target(), address.Document().Scope(), address.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewManagedContribution(contract, value)
	if err != nil {
		t.Fatal(err)
	}
	row, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func selectionState(t *testing.T, rows ...durable.ManagedAggregateState) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: rows})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func membershipNativeRow(t *testing.T, alias aggregate.DocumentAddress, codec aggregate.CodecContractID, parts ...string) durable.ManagedAggregateState {
	t.Helper()
	contract, err := aggregate.NewHostConfigProjectionContract(alias.Target(), alias.Scope(), alias.AggregateRoot(), documentKeyFootprint(t, parts...), codec)
	if err != nil {
		t.Fatal(err)
	}
	return selectionNativeRow(t, contract, "1")
}

func membershipBindingValue(t *testing.T, fixture documentClaimFixture) ownership.DocumentBindingValue {
	t.Helper()
	binding, err := ownership.NewDocumentBinding(fixture.owner, fixture.alias.AggregateRoot(), fixture.exact)
	if err != nil {
		t.Fatal(err)
	}
	value, err := ownership.PresentDocumentBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func membershipIntent(t *testing.T, fixture documentClaimFixture, before, after ownership.DocumentBindingValue) ownership.DocumentBindingIntent {
	t.Helper()
	intent, err := ownership.NewExactDocumentBindingIntent(fixture.owner, fixture.alias.AggregateRoot(), before, after)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}
