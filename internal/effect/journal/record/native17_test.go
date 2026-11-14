package record

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
	"time"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func native17RecordFixture(t *testing.T, fixture nativeRecordFixture) native17JournalRecord {
	t.Helper()
	manifest, err := configuration.ProjectRootReference()
	if err != nil {
		t.Fatal(err)
	}
	legacy := fixture.record.DocumentGroups[0]
	address, err := legacy.Aliases[0].Address.canonical()
	if err != nil {
		t.Fatal(err)
	}
	binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot()})
	if err != nil {
		t.Fatal(err)
	}
	before, after, err := EncodeSnapshots(fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	body := native16RecordBody(fixture.record)
	body.StatefileBefore, body.StatefileAfter = before, after
	roots := []native17Root{{Reference: persistedRootReference(manifest), Kind: "directory", Provenance: fixture.record.ManifestRootProvenance}}
	if binding.RootReference() != manifest {
		roots = append(roots, native17Root{Reference: persistedRootReference(binding.RootReference()), Kind: "directory", Provenance: legacy.Writer.Root})
	}
	slices.SortFunc(roots, func(left, right native17Root) int {
		a, _ := left.Reference.canonical()
		b, _ := right.Reference.canonical()
		return a.Compare(b)
	})
	return native17JournalRecord{
		Version: native17Version, Purpose: "host-mutation", nativeRecordBody: body,
		Manifest: persistedRootReference(manifest),
		Roots:    roots,
		DocumentGroups: []native17Document{{
			nativeDocumentBody: native16GroupBody(legacy),
			native17DocumentEnvelope: native17DocumentEnvelope{
				Writer:    native17Writer{Root: persistedRootReference(binding.RootReference()), Coordinate: nativeCoordinate{Kind: "directory-child", Child: legacy.Writer.RelativePath}},
				Aliases:   []native17Alias{{Address: legacy.Aliases[0].Address, Root: persistedRootReference(binding.RootReference()), Coordinate: persistedCoordinate(binding.Coordinate()), Policy: binding.PolicyID(), SelectedObservation: legacy.Aliases[0].SelectedObservation, ObservedBefore: legacy.Aliases[0].ObservedBefore}},
				Sequences: []nativeSequenceWitness{},
			},
		}},
	}
}

func TestNative17CaptureUsesTheSameBoundNamespaceBeforeAndAfterPublicationPreparation(t *testing.T) {
	fixture := observedNativeRecordFixture(t)
	legacy := fixture.record.DocumentGroups[0]
	claims, err := legacy.ExclusiveClaims.canonicalObservedExact(fixture.claims.owner, fixture.record.OperationID, legacy.CapturedTarget)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := legacy.canonicalBindingIntents(t.Context(), fixture.claims.owner)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: fixture.claims.alias.Target(), Scope: fixture.claims.alias.Scope(), Destination: fixture.claims.alias.AggregateRoot()})
	if err != nil {
		t.Fatal(err)
	}
	coordinate, err := configuration.NewDirectoryChild(fixture.relative.Path())
	if err != nil {
		t.Fatal(err)
	}
	pin, err := journalnamespace.NewBoundWriterPin(binding.RootReference(), fixture.provenance, coordinate)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := BeginDocumentGroup(fixture.claims.owner, fixture.record.OperationID, bindings, &claims, nil)
	if err != nil {
		t.Fatal(err)
	}
	draft, err = draft.WithBoundNamespace(t.Context(), fixture.observation, pin, "", []DocumentGroupAlias{NewBoundDocumentGroupAlias(fixture.claims.alias, binding, fixture.provenance, fixture.observation)}, fixture.provenance)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reconstructRecordFixture(t.Context(), fixture.nativeValue(t), fixture.nativeRecordFixture)
	if err != nil {
		t.Fatal(err)
	}
	var subjects []topology.SubjectID
	for _, selected := range legacy.SelectedSubjects {
		subject, err := selected.canonical()
		if err != nil {
			t.Fatal(err)
		}
		subjects = append(subjects, subject)
	}
	comparison := loaded.groups[0].Comparison()
	candidate, err := draft.WithComparison(comparison, 0, subjects)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _, err := candidate.Seal(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := configuration.ProjectRootReference()
	if err != nil {
		t.Fatal(err)
	}
	var roots []RootCapture
	for _, reference := range []output.RootReference{manifest, binding.RootReference()} {
		root, err := NewRootCapture(reference, fixture.provenance, "")
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, root)
	}
	createdAt, err := time.Parse(time.RFC3339, fixture.record.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewNative17Capture(t.Context(), fixture.record.OperationID, createdAt, fixture.claims.owner, manifest, roots, fixture.before, fixture.after, []SealedDocumentGroup{sealed}, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := journal.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.limits, fixture.codecs)
	if err != nil {
		t.Fatal(err)
	}
	content, err := prepared.Encode(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	_, originalReference, expectedReference, err := documentImageReferences(comparison, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(nativeRecordJSON(t, decoded.record.DocumentGroups[0].Original), nativeRecordJSON(t, originalReference)) || !bytes.Equal(nativeRecordJSON(t, decoded.record.DocumentGroups[0].Expected), nativeRecordJSON(t, expectedReference)) {
		t.Fatal("native17 image references did not survive capture/prepare serialization")
	}
	for _, value := range []Native{journal, decoded} {
		document, err := value.DocumentAt(0)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := document.WriterPin()
		if err != nil || actual != pin {
			t.Fatal("capture/prepare/serialization changed the bound writer", err)
		}
	}
}

func TestNative17RequiresProjectNativeClaimsWithoutReinterpretingNative16(t *testing.T) {
	fixture := nativeJournalFixture(t)
	destination, err := output.Parse(".pi/custom.json")
	if err != nil {
		t.Fatal(err)
	}
	alias, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeProject, destination)
	if err != nil {
		t.Fatal(err)
	}
	fixture.claims.alias = alias
	group := &fixture.record.DocumentGroups[0]
	group.Aliases[0].Address = persistedDocumentAlias(alias)
	group.Aliases[0].LogicalRoot.Role = "manifest-root"
	before := selectionNativeRow(t, membershipNativeRow(t, alias, "native-json-v1", "model").Contribution().Contract(), nativeRecordCanonical(t, "true"))
	after := selectionNativeRow(t, before.Contribution().Contract(), nativeRecordCanonical(t, `"fast"`))
	fixture.withSelectedRows(t, before, after)
	wire := native17RecordFixture(t, fixture)
	journal, err := wire.reconstruct(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := reconstructRecordFixture(t.Context(), journal, fixture); err != nil || len(loaded.claims[0].Before()) != 1 {
		t.Fatal("native17 omitted project native membership", err)
	}
	wire.DocumentGroups[0].ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
	journal, err = wire.reconstruct(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := reconstructRecordFixture(t.Context(), journal, fixture); err == nil || len(loaded.groups) != 0 {
		t.Fatal("native17 treated missing project claims as unmanaged")
	}
	fixture.record.DocumentGroups[0].ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
	if _, err := reconstructRecordFixture(t.Context(), fixture.nativeValue(t), fixture); err != nil {
		t.Fatal("native16 lost its frozen global-only claim grammar", err)
	}
}

func TestNative17RetainsBoundPoliciesAndReencodesTheCurrentBody(t *testing.T) {
	fixture := nativeJournalFixture(t)
	wire := native17RecordFixture(t, fixture)
	value, err := wire.reconstruct(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	content, err := value.Encode(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	for _, journal := range []Native{value, decoded} {
		document, err := journal.DocumentAt(0)
		if err != nil {
			t.Fatal(err)
		}
		pin, err := document.WriterPin()
		reference, bound := pin.RootReference()
		if err != nil || !bound || reference.Identity() != "pi-agent" {
			t.Fatal("native17 lost its bound writer", err)
		}
		binding, recorded := document.Aliases()[0].Binding()
		if !recorded || binding.PolicyID() != "pi-native-v1" {
			t.Fatal("native17 lost its retained policy")
		}
		if _, err := reconstructRecordFixture(t.Context(), journal, fixture); err != nil {
			t.Fatal(err)
		}
	}
	copy := decoded
	copy.record.CreatedAt = "2026-02-01T00:00:00Z"
	changed, err := copy.Encode(t.Context(), fixture.codec)
	if err != nil || bytes.Equal(changed, content) {
		t.Fatal("encoding ignored the single current mutable body", err)
	}
	original, err := decoded.Encode(t.Context(), fixture.codec)
	if err != nil || !bytes.Equal(original, content) {
		t.Fatal("derived value changed its input envelope or body", err)
	}
}

func TestNative17RequiresTheCompletePositionalSequenceSet(t *testing.T) {
	fixture := nativeJournalFixture(t)
	fixture.withImages(
		t,
		aggregate.ExistingDocument([]byte(`{"model":true,"left":["b","retired","a"],"right":[2,1]}`)),
		aggregate.ExistingDocument([]byte(`{"model":"fast","left":["a","b"],"right":[1,2]}`)),
	)
	wire := native17RecordFixture(t, fixture)
	alias := wire.DocumentGroups[0].Aliases[0].Address
	wire.DocumentGroups[0].Sequences = []nativeSequenceWitness{
		{Alias: alias, Path: []string{"right"}, Syntax: "json-v1", Removed: []int{}, Order: []int{1, 0}},
		{Alias: alias, Path: []string{"left"}, Syntax: "json-v1", Removed: []int{1}, Order: []int{1, 0}},
	}
	for _, count := range []int{2, 1, 0} {
		candidate := wire
		candidate.DocumentGroups = append([]native17Document(nil), wire.DocumentGroups...)
		candidate.DocumentGroups[0].Sequences = wire.DocumentGroups[0].Sequences[:count]
		journal, err := candidate.reconstruct(t.Context(), fixture.codec)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := reconstructRecordFixture(t.Context(), journal, fixture)
		if count == 2 && (err != nil || len(loaded.groups) != 1) {
			t.Fatal("complete disjoint sequence set did not reconstruct", err)
		}
		if count != 2 && (err == nil || len(loaded.groups) != 0) {
			t.Fatal("incomplete sequence set escaped the outside-area check")
		}
	}
}

func TestNative17RefusesMalformedRootPolicyAndPurposeBeforeEffects(t *testing.T) {
	fixture := nativeJournalFixture(t)
	for name, mutate := range map[string]func(*native17JournalRecord){
		"missing writer root": func(record *native17JournalRecord) { record.DocumentGroups[0].Writer.Root.Identity = "unknown" },
		"unknown policy":      func(record *native17JournalRecord) { record.DocumentGroups[0].Aliases[0].Policy = "pi-native-future" },
		"wrong coordinate kind": func(record *native17JournalRecord) {
			record.DocumentGroups[0].Aliases[0].Coordinate = nativeCoordinate{Kind: "point-self"}
		},
		"directory alternative": func(record *native17JournalRecord) { record.Roots[0].Alternative = "fallback" },
		"legacy locator reinterpreted as point": func(record *native17JournalRecord) {
			record.Roots[0].Kind, record.Roots[0].Alternative = "point", "preferred"
		},
		"extra root": func(record *native17JournalRecord) {
			reference, _ := output.NewRootReference("codex-home", "codex-home-location-v1")
			record.Roots = append([]native17Root{{Reference: persistedRootReference(reference), Kind: "directory", Provenance: record.Roots[0].Provenance}}, record.Roots...)
		},
		"release as host mutation": func(record *native17JournalRecord) { record.Purpose = "release-only" },
		"implicit sequence set":    func(record *native17JournalRecord) { record.DocumentGroups[0].Sequences = nil },
	} {
		t.Run(name, func(t *testing.T) {
			wire := native17RecordFixture(t, fixture)
			mutate(&wire)
			content, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if value, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec); err == nil || value.requireNativeValue() == nil {
				t.Fatal("malformed17 returned usable or partial native facts", err)
			}
		})
	}
}

func TestNative17RefusesSequenceContractsThatCannotReplay(t *testing.T) {
	fixture := nativeJournalFixture(t)
	for _, syntax := range []string{"toml-v1", "json-v2"} {
		t.Run(syntax, func(t *testing.T) {
			wire := native17RecordFixture(t, fixture)
			wire.DocumentGroups[0].Sequences = []nativeSequenceWitness{{Alias: wire.DocumentGroups[0].Aliases[0].Address, Path: []string{"packages"}, Syntax: syntax, Removed: []int{}, Order: []int{1, 0}}}
			content, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if journal, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec); err == nil || journal.requireNativeValue() == nil {
				t.Fatal("irreplayable sequence contract reached admitted native facts", err)
			}
		})
	}
}
