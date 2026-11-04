package record

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
)

type countingNativeSnapshotCodec struct {
	durable.SnapshotCodec
	decodes int
}

func (codec *countingNativeSnapshotCodec) Decode(content []byte) (durable.Snapshot, error) {
	codec.decodes++
	return codec.SnapshotCodec.Decode(content)
}

func loadRecordFixtureImage(reference DocumentImage, document aggregate.Document) (LoadedImage, error) {
	if !reference.Exists() {
		return reference.Absent()
	}
	return reference.Load(document.Content(), Mode)
}

func reconstructRecordFixture(ctx context.Context, journal Native, fixture nativeRecordFixture) (LoadedDocuments, error) {
	session, err := journal.BeginLoadedDocuments(ctx, fixture.limits, fixture.codecs)
	if err != nil {
		return LoadedDocuments{}, err
	}
	for {
		demand, present, err := session.Next(ctx)
		if err != nil {
			return LoadedDocuments{}, err
		}
		if !present {
			break
		}
		original, err := loadRecordFixtureImage(demand.Original(), fixture.original)
		if err != nil {
			return LoadedDocuments{}, err
		}
		expected, err := loadRecordFixtureImage(demand.Expected(), fixture.expected)
		if err != nil {
			return LoadedDocuments{}, err
		}
		if err := session.SupplyLoaded(ctx, demand, original, expected); err != nil {
			return LoadedDocuments{}, err
		}
	}
	return session.FinishLoaded(ctx)
}

func TestNativeJournalRecordRoundtripsFullStateAndActualImages(t *testing.T) {
	fixture := nativeJournalFixture(t)
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil || !decoded.before.Equal(fixture.before) || !decoded.after.Equal(fixture.after) {
		t.Fatalf("full native Snapshots did not survive the selected codec: %v", err)
	}
	loaded, err := reconstructRecordFixture(t.Context(), decoded, fixture)
	if err != nil || len(loaded.groups) != 1 || len(loaded.images) != 1 || len(loaded.claims[0].After()) != 1 {
		t.Fatalf("complete record reconstruction: %v", err)
	}
	mode := recovery.NewPermissionMode(0o600)
	current := aggregate.ExistingDocument([]byte(`{"model":"fast","outside":"fresh"}`))
	restored, err := loaded.groups[0].Comparison().RestoreBefore(t.Context(), current, mode)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(restored.Document().Content(), &values); err != nil || string(values["model"]) != "true" || string(values["outside"]) != `"fresh"` {
		t.Fatalf("record-backed rollback changed foreign data or lost before values: %v", err)
	}
	if retry, err := loaded.groups[0].Comparison().RestoreBefore(t.Context(), restored.Document(), mode); err != nil || !retry.Document().Equal(restored.Document()) {
		t.Fatalf("record-backed restore is not retry-safe: %v", err)
	}
	if result, err := decodeNativeJournalRecord(t.Context(), content, statefile.Codec{}); err != nil || !result.before.Equal(fixture.before) || !result.after.Equal(fixture.after) {
		t.Fatalf("current Snapshot codec lost native journal state: %v", err)
	}
	restricted, err := statefile.NewCodec(aggregatecodec.Catalog().Contributions())
	if err != nil {
		t.Fatal(err)
	}
	if result, err := decodeNativeJournalRecord(t.Context(), content, restricted); err == nil || result.record.Version != 0 {
		t.Fatal("native record bypassed explicitly restricted Snapshot admission")
	}
	if result, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, restricted); err == nil || result != nil {
		t.Fatal("restricted Snapshot codec emitted native baselines")
	}
}

func TestNativeJournalVersionGatePrecedesNestedSnapshotDecoding(t *testing.T) {
	fixture := nativeJournalFixture(t)
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{15, 17} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(content, &object); err != nil {
			t.Fatal(err)
		}
		object["version"], err = json.Marshal(version)
		if err != nil {
			t.Fatal(err)
		}
		object["statefile_before"] = json.RawMessage(`{"version":"not-a-Snapshot"}`)
		changed, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		codec := &countingNativeSnapshotCodec{SnapshotCodec: fixture.codec}
		if result, err := decodeNativeJournalRecord(t.Context(), changed, codec); err == nil || result.record.Version != 0 || codec.decodes != 0 {
			t.Fatal("unsupported version reached nested Snapshot decoding or returned facts")
		}
	}
}

func TestNativeJournalRecordRejectsClosedGrammarCorruption(t *testing.T) {
	fixture := nativeJournalFixture(t)
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"missing groups", "null groups", "unknown", "alias spelling", "nested null", "missing root", "ambiguous observation", "whole selection", "noncanonical writer", "duplicate alias"} {
		t.Run(change, func(t *testing.T) {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(content, &object); err != nil {
				t.Fatal(err)
			}
			var groups []map[string]json.RawMessage
			if err := json.Unmarshal(object["document_groups"], &groups); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "missing groups":
				delete(object, "document_groups")
			case "null groups":
				object["document_groups"] = json.RawMessage(`null`)
			case "unknown":
				object["unknown"] = json.RawMessage(`true`)
			case "alias spelling":
				groups[0]["Original"] = groups[0]["original"]
			case "nested null":
				groups[0]["writer"] = json.RawMessage(`{"root_provenance":null,"relative_path":"host/custom.json"}`)
			case "missing root":
				groups[0]["writer"] = json.RawMessage(`{"relative_path":"host/custom.json"}`)
			case "ambiguous observation":
				var observed map[string]json.RawMessage
				if err := json.Unmarshal(groups[0]["captured_target"], &observed); err != nil {
					t.Fatal(err)
				}
				observed["namespace_authority"] = observed["authority"]
				groups[0]["captured_target"] = nativeRecordJSON(t, observed)
			case "whole selection":
				groups[0]["extent"] = json.RawMessage(`"whole"`)
			case "noncanonical writer":
				var writer map[string]json.RawMessage
				if err := json.Unmarshal(groups[0]["writer"], &writer); err != nil {
					t.Fatal(err)
				}
				writer["relative_path"] = json.RawMessage(`"host/../custom.json"`)
				groups[0]["writer"] = nativeRecordJSON(t, writer)
			case "duplicate alias":
				var aliases []json.RawMessage
				if err := json.Unmarshal(groups[0]["aliases"], &aliases); err != nil {
					t.Fatal(err)
				}
				groups[0]["aliases"] = nativeRecordJSON(t, append(aliases, aliases[0]))
			}
			if change != "missing groups" && change != "null groups" {
				object["document_groups"] = nativeRecordJSON(t, groups)
			}
			if result, err := decodeNativeJournalRecord(t.Context(), nativeRecordJSON(t, object), fixture.codec); err == nil || result.record.Version != 0 {
				t.Fatal("malformed complete record returned decoded facts")
			}
		})
	}
}

func TestNativeJournalReconstructionRejectsLateGroupWithoutPartialPrefix(t *testing.T) {
	fixture := nativeJournalFixture(t)
	record := fixture.record
	late := record.DocumentGroups[0]
	late.Writer.RelativePath = "late.json"
	late.Aliases = []recoveryDocumentAlias{late.Aliases[0]}
	late.Aliases[0].Address.Destination = "@pi-agent/late.json"
	late.BindingIntents = []recoveryDocumentBindingIntent{late.BindingIntents[0]}
	late.BindingIntents[0].Destination = "@pi-agent/late.json"
	missing := late.SelectedSubjects[0]
	missing.Name += "-missing"
	late.SelectedSubjects = []persistedSubjectRef{missing}
	record.DocumentGroups = append(record.DocumentGroups, late)
	content, err := marshalNativeJournalRecord(t.Context(), record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reconstructRecordFixture(t.Context(), decoded, fixture)
	if err == nil || loaded.groups != nil || loaded.images != nil || loaded.claims != nil || loaded.provisional != nil {
		t.Fatal("late full-reference failure retained a usable first group")
	}
}

func TestNativeJournalReconstructionRefusesClaimAndBindingForeignKeys(t *testing.T) {
	for _, change := range []string{"missing claims", "wrong footprint", "wrong owner", "wrong binding target", "missing binding", "unselected change", "missing changed group", "wrong after baseline"} {
		t.Run(change, func(t *testing.T) {
			fixture := nativeJournalFixture(t)
			group := &fixture.record.DocumentGroups[0]
			switch change {
			case "missing claims":
				group.ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
			case "wrong footprint":
				claim := documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "other"))
				transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.record.OperationID)
				if err != nil {
					t.Fatal(err)
				}
				group.ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
				if err != nil {
					t.Fatal(err)
				}
			case "wrong owner":
				group.BindingIntents[0].StatefileAuthority = *persistedPathAuthority(documentClaimsForTest(t).owner.StatefileAuthority())
			case "wrong binding target":
				group.BindingIntents[0].Before.Authority = persistedPathAuthority(documentClaimsForTest(t).exact)
				group.BindingIntents[0].After = group.BindingIntents[0].Before
			case "missing binding":
				group.BindingIntents = []recoveryDocumentBindingIntent{}
			case "unselected change":
				group.SelectedSubjects = []persistedSubjectRef{}
			case "missing changed group":
				fixture.record.DocumentGroups = []recoveryDocumentGroup{}
			case "wrong after baseline":
				row := fixture.after.ManagedAggregates()[0]
				fixture.after = selectionState(t, selectionNativeRow(t, row.Contribution().Contract(), nativeRecordCanonical(t, `"different"`)))
			}
			content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
			if err != nil {
				// Header/shape admission may reject earlier than semantic reconstruction.
				return
			}
			decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
			if err != nil {
				t.Fatal(err)
			}
			if loaded, err := reconstructRecordFixture(t.Context(), decoded, fixture); err == nil || loaded.groups != nil || loaded.images != nil {
				t.Fatal("foreign metadata or uncovered changed state returned a usable group")
			}
		})
	}
}

func TestNativeJournalCancellationReturnsNoRecordOrGroup(t *testing.T) {
	fixture := nativeJournalFixture(t)
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := decodeNativeJournalRecord(ctx, content, fixture.codec); !errors.Is(err, context.Canceled) || result.record.Version != 0 {
		t.Fatal("canceled decode returned record facts")
	}
	if result, err := reconstructRecordFixture(ctx, decoded, fixture); !errors.Is(err, context.Canceled) || result.groups != nil || result.images != nil {
		t.Fatal("canceled reconstruction returned a group")
	}
}
