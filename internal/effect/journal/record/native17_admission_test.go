package record

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestNative17RefusesIncompleteOrAmbiguousNamespaceEvidence(t *testing.T) {
	fixture := nativeJournalFixture(t)
	for name, mutate := range map[string]func(*native17JournalRecord){
		"root ordering":  func(wire *native17JournalRecord) { slices.Reverse(wire.Roots) },
		"duplicate root": func(wire *native17JournalRecord) { wire.Roots = append(wire.Roots[:1], wire.Roots...) },
		"manifest revision": func(wire *native17JournalRecord) {
			wire.Manifest.Locator = "selected-project-v2"
			wire.Roots[1].Reference = wire.Manifest
		},
		"alias before": func(wire *native17JournalRecord) {
			wire.DocumentGroups[0].Aliases[0].ObservedBefore = recoveryDocumentObservation{Kind: "absent"}
		},
		"unknown sequence alias": func(wire *native17JournalRecord) {
			wire.DocumentGroups[0].Sequences[0].Alias.Destination = "@pi-agent/unknown.json"
		},
		"root array":             func(wire *native17JournalRecord) { wire.DocumentGroups[0].Sequences[0].Path = []string{} },
		"negative removed slot":  func(wire *native17JournalRecord) { wire.DocumentGroups[0].Sequences[0].Removed = []int{-1} },
		"unsorted removed slots": func(wire *native17JournalRecord) { wire.DocumentGroups[0].Sequences[0].Removed = []int{1, 0} },
		"duplicate survivor":     func(wire *native17JournalRecord) { wire.DocumentGroups[0].Sequences[0].Order = []int{0, 0} },
		"overlapping array paths": func(wire *native17JournalRecord) {
			other := wire.DocumentGroups[0].Sequences[0]
			other.Path = []string{"packages", "nested"}
			wire.DocumentGroups[0].Sequences = append(wire.DocumentGroups[0].Sequences, other)
		},
	} {
		t.Run(name, func(t *testing.T) {
			wire := native17RecordFixture(t, fixture)
			wire.DocumentGroups[0].Sequences = []nativeSequenceWitness{{Alias: wire.DocumentGroups[0].Aliases[0].Address, Path: []string{"packages"}, Syntax: "json-v1", Removed: []int{}, Order: []int{1, 0}}}
			positive, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeNativeJournalRecord(t.Context(), positive, fixture.codec); err != nil {
				t.Fatal("invalid positive metadata control", err)
			}
			mutate(&wire)
			content, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if value, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec); err == nil || value.requireNativeValue() == nil {
				t.Fatal("invalid namespace evidence returned usable or partial native facts", err)
			}
		})
	}
	content, err := json.Marshal(native17RecordFixture(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	var groups []map[string]json.RawMessage
	if err := json.Unmarshal(fields["document_groups"], &groups); err != nil {
		t.Fatal(err)
	}
	groups[0]["pi_order"] = json.RawMessage(`null`)
	fields["document_groups"], err = json.Marshal(groups)
	if err != nil {
		t.Fatal(err)
	}
	content, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec); err == nil {
		t.Fatal("native17 admitted the historical16 host-specific order field")
	}
}

func native17PointMetadata(t *testing.T, fixture nativeRecordFixture) native17JournalRecord {
	t.Helper()
	wire := native17RecordFixture(t, fixture)
	point := nativeRootReference{Identity: "claude-account", Locator: "claude-account-point-v2"}
	wire.Roots[0].Reference, wire.Roots[0].Kind, wire.Roots[0].Alternative = point, "point", "fallback"
	group := &wire.DocumentGroups[0]
	group.Writer = native17Writer{Root: point, Coordinate: nativeCoordinate{Kind: "point-self"}, Spelling: filepath.Join(wire.Roots[0].Provenance.PhysicalRoot, "configured", "fallback.json")}
	destination, err := output.Parse(aggregate.ClaudeGlobalMCPConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(target.TargetClaudeCode, target.ScopeGlobal, destination)
	if err != nil {
		t.Fatal(err)
	}
	group.Aliases[0].Address, group.Aliases[0].Root = persistedDocumentAlias(address), point
	group.Aliases[0].Coordinate, group.Aliases[0].Policy = nativeCoordinate{Kind: "point-self"}, "claude-account-carrier-v2"
	return wire
}

func TestNative17PointMetadataRequiresOwnAliasAndContainedSpelling(t *testing.T) {
	fixture := nativeJournalFixture(t)
	wire := native17PointMetadata(t, fixture)
	content, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal("point metadata positive control", err)
	}
	document, err := journal.DocumentAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := document.RequireWriterSpelling(wire.DocumentGroups[0].Writer.Spelling); err != nil {
		t.Fatal(err)
	}
	if err := document.RequireWriterSpelling(filepath.Join(wire.Roots[0].Provenance.PhysicalRoot, "other.json")); err == nil {
		t.Fatal("point spelling was not tied to the qualified writer path")
	}
	for name, mutate := range map[string]func(*native17JournalRecord){
		"outside witness": func(wire *native17JournalRecord) {
			wire.DocumentGroups[0].Writer.Spelling = filepath.Join(filepath.Dir(wire.Roots[0].Provenance.PhysicalRoot), "other.json")
		},
		"witness itself": func(wire *native17JournalRecord) {
			wire.DocumentGroups[0].Writer.Spelling = wire.Roots[0].Provenance.PhysicalRoot
		},
		"missing spelling": func(wire *native17JournalRecord) { wire.DocumentGroups[0].Writer.Spelling = "" },
		"no own point alias": func(wire *native17JournalRecord) {
			directory := native17RecordFixture(t, fixture)
			wire.Roots = append(wire.Roots[:1], directory.Roots...)
			wire.DocumentGroups[0].Aliases = directory.DocumentGroups[0].Aliases
		},
		"directory spelling": func(wire *native17JournalRecord) {
			*wire = native17RecordFixture(t, fixture)
			wire.DocumentGroups[0].Writer.Spelling = filepath.Join(wire.Roots[0].Provenance.PhysicalRoot, "custom.json")
		},
		"same identity two revisions": func(wire *native17JournalRecord) {
			anchor := wire.Roots[0]
			anchor.Reference.Locator, anchor.Kind, anchor.Alternative = "claude-account-location-v1", "directory", ""
			wire.Roots = append([]native17Root{anchor}, wire.Roots...)
			wire.DocumentGroups[0].Writer = native17Writer{Root: anchor.Reference, Coordinate: nativeCoordinate{Kind: "directory-child", Child: "configured/fallback.json"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			wire := native17PointMetadata(t, fixture)
			mutate(&wire)
			content, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if value, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec); err == nil || value.requireNativeValue() == nil {
				t.Fatal("invalid point metadata returned usable or partial native facts", err)
			}
		})
	}
}

func TestNative17PromotionEncodesOnlyTheCurrentBody(t *testing.T) {
	fixture := nativeJournalFixture(t)
	fixture.before = durable.EmptySnapshot()
	fixture.withImages(t, aggregate.AbsentDocument(), aggregate.ExistingDocument([]byte(`{"model":"fast"}`)))
	group := &fixture.record.DocumentGroups[0]
	pending := fixture.claims.provisional
	observation := recoveryDocumentObservation{Kind: "provisional", Authority: &pathAuthorityDTO{Key: pending.CandidateKey(), Witness: pending.CandidateWitness()}, NamespaceAuthority: persistedPathAuthority(pending.Namespace())}
	group.CapturedTarget = observation
	group.Aliases[0].SelectedObservation, group.Aliases[0].ObservedBefore = observation, observation
	intent, err := ownership.NewProvisionalAcquireIntent(fixture.claims.alias.AggregateRoot(), documentKeyFootprint(t, "model"), pending, fixture.claims.owner, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	group.ExclusiveClaims, err = persistedDocumentProvisionalClaims([]journaldocument.ProvisionalAcquire{journaldocument.NewProvisionalAcquire(fixture.claims.alias, intent)}, journaldocument.NativeClaims)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewProvisionalDocumentBindingIntent(fixture.claims.owner, fixture.claims.alias.AggregateRoot(), pending)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := persistedDocumentBindingIntent(binding)
	if err != nil {
		t.Fatal(err)
	}
	group.BindingIntents = []recoveryDocumentBindingIntent{persisted}
	wire := native17RecordFixture(t, fixture)
	journal, err := wire.reconstruct(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	original, err := journal.Encode(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	promoted, changed, err := journal.PromoteDocument(t.Context(), 0, []aggregate.DocumentAddress{fixture.claims.alias}, fixture.claims.exact)
	if err != nil || !changed {
		t.Fatal("native17 did not promote its pending body", err)
	}
	content, err := promoted.Encode(t.Context(), fixture.codec)
	if err != nil || bytes.Equal(content, original) {
		t.Fatal("native17 encoder ignored the promoted body", err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reconstructRecordFixture(t.Context(), decoded, fixture)
	if err != nil || len(loaded.provisional[0]) != 0 || len(loaded.claims[0].Prepared()) != 1 || loaded.images[0].Original().Document().Exists() {
		t.Fatal("serialized promotion lost exact exclusion or the retained before image", err)
	}
	unchanged, err := journal.Encode(t.Context(), fixture.codec)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("promotion mutated its input journal", err)
	}
}
