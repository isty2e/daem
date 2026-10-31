package journal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
)

type nativeRecordFixture struct {
	record nativeJournalRecord
	before durable.Snapshot
	after  durable.Snapshot
	codec  durable.SnapshotCodec
	read   nativeDocumentReadOptions
	claims documentClaimFixture
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
	remaining := fixture.read.budget.RemainingBytes()
	loaded, err := decoded.loadDocuments(t.Context(), fixture.read)
	if err != nil || len(loaded.groups) != 1 || len(loaded.images) != 1 || len(loaded.claims[0].After()) != 1 {
		t.Fatalf("complete record reconstruction: %v", err)
	}
	wantBytes := *fixture.record.DocumentGroups[0].Original.Artifact.Size + *fixture.record.DocumentGroups[0].Expected.Artifact.Size
	if remaining-fixture.read.budget.RemainingBytes() != wantBytes {
		t.Fatal("the whole image pair did not use the shared physical byte allocation")
	}
	mode := recovery.NewPermissionMode(0o600)
	current := aggregate.ExistingDocument([]byte(`{"model":"fast","outside":"fresh"}`))
	restored, err := loaded.groups[0].comparison.RestoreBefore(t.Context(), current, mode)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(restored.Document().Content(), &values); err != nil || string(values["model"]) != "true" || string(values["outside"]) != `"fresh"` {
		t.Fatalf("record-backed rollback changed foreign data or lost before values: %v", err)
	}
	if retry, err := loaded.groups[0].comparison.RestoreBefore(t.Context(), restored.Document(), mode); err != nil || !retry.Document().Equal(restored.Document()) {
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
	loaded, err := decoded.loadDocuments(t.Context(), fixture.read)
	if err == nil || loaded.groups != nil || loaded.images != nil || loaded.claims != nil || loaded.provisional != nil {
		t.Fatal("late full-reference failure retained a usable first group")
	}
	if err := os.WriteFile(filepath.Join(fixture.record.ManifestRootProvenance.PhysicalRoot, "images", "expected"), []byte(`{"model":"wrong"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	decoded.record = fixture.record
	loaded, err = decoded.loadDocuments(t.Context(), fixture.read)
	if err == nil || loaded.groups != nil || loaded.images != nil {
		t.Fatal("changed private expected payload returned compiled reconstruction")
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
			if loaded, err := decoded.loadDocuments(t.Context(), fixture.read); err == nil || loaded.groups != nil || loaded.images != nil {
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
	if result, err := decoded.loadDocuments(ctx, fixture.read); !errors.Is(err, context.Canceled) || result.groups != nil || result.images != nil {
		t.Fatal("canceled reconstruction returned a group")
	}
}

func nativeJournalFixture(t *testing.T) nativeRecordFixture {
	t.Helper()
	claims := documentClaimsForTest(t)
	beforeRow := membershipNativeRow(t, claims.alias, "native-json-v1", "model")
	beforeRow = selectionNativeRow(t, beforeRow.Contribution().Contract(), nativeRecordCanonical(t, "true"))
	afterRow := selectionNativeRow(t, beforeRow.Contribution().Contract(), nativeRecordCanonical(t, `"fast"`))
	before, after := selectionState(t, beforeRow), selectionState(t, afterRow)
	originalContent, expectedContent := []byte(`{"model":true,"outside":1}`), []byte(`{"model":"fast","outside":1}`)
	root, originalPath := imageFixture(t, originalContent)
	if err := os.WriteFile(filepath.Join(filepath.Dir(originalPath), "expected"), expectedContent, 0o600); err != nil {
		t.Fatal(err)
	}
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	persistedRoot := *persistedRecoveryRootProvenanceFromCanonical(mustRecoveryRootProvenance(t, recoveryRootProvenance{
		PhysicalRoot: provenance.PhysicalRoot(), ObjectFingerprint: provenance.ObjectFingerprint(), MountFingerprint: provenance.MountFingerprint(),
	}))
	mode := recovery.NewPermissionMode(0o600)
	original, err := persistedDocumentImage(aggregate.ExistingDocument(originalContent), mode, "images/before")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := persistedDocumentImage(aggregate.ExistingDocument(expectedContent), mode, "images/expected")
	if err != nil {
		t.Fatal(err)
	}
	operationID := "native-record-apply"
	claim := documentActiveClaim(t, claims, beforeRow.Contribution().Contract().Address().Footprint())
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, operationID)
	if err != nil {
		t.Fatal(err)
	}
	claimRecord, err := persistedDocumentClaimTransition(transition, operationID)
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, claims)
	binding, err := persistedDocumentBindingIntent(membershipIntent(t, claims, value, value))
	if err != nil {
		t.Fatal(err)
	}
	observation := recoveryDocumentObservation{Kind: "exact", Authority: persistedPathAuthority(claims.exact)}
	group := recoveryDocumentGroup{
		CapturedTarget: observation, Writer: recoveryDocumentWriter{Root: persistedRoot, RelativePath: "host/custom.json"},
		Aliases: []recoveryDocumentAlias{{Address: persistedDocumentAlias(claims.alias), LogicalRoot: recoveryDocumentLogicalRoot{Role: "global-root", Provenance: persistedRoot}, SelectedObservation: observation, ObservedBefore: observation}},
		Extent:  "selected", SelectedSubjects: []persistedSubjectRef{{Kind: string(beforeRow.Subject().Kind()), Namespace: beforeRow.Subject().Namespace(), Name: beforeRow.Subject().Key()}},
		Original: original, Expected: expected, ExclusiveClaims: claimRecord, BindingIntents: []recoveryDocumentBindingIntent{binding},
	}
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	codecs, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{beforeRow.Contribution().CodecContractID()}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	return nativeRecordFixture{
		record: nativeJournalRecord{
			Version: nativeRecoveryJournalVersion, Operation: recoveryOperationApply, OperationID: operationID, CreatedAt: "2026-01-01T00:00:00Z",
			StateAuthority:         recoveryStateAuthority{StatefileAuthority: *persistedPathAuthority(claims.owner.StatefileAuthority()), ManifestPath: claims.owner.ManifestPath()},
			ManifestRootProvenance: persistedRoot, Entries: []recoveryEntry{}, ClaimTransitions: []recoveryClaimTransition{}, ProvisionalAcquires: []recoveryProvisionalAcquireIntent{}, RemovalIntents: []recoveryRemovalIntent{}, DocumentGroups: []recoveryDocumentGroup{group},
		},
		before: before, after: after, codec: codec, claims: claims,
		read: nativeDocumentReadOptions{artifacts: root, filesystem: journalTestFilesystem(), budget: imageBudget(t), limits: limits, codecs: codecs},
	}
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

func mustRecoveryRootProvenance(t *testing.T, record recoveryRootProvenance) recovery.RootProvenance {
	t.Helper()
	value, err := canonicalRecoveryRootProvenance(record)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type countingNativeSnapshotCodec struct {
	durable.SnapshotCodec
	decodes int
}

func (codec *countingNativeSnapshotCodec) Decode(content []byte) (durable.Snapshot, error) {
	codec.decodes++
	return codec.SnapshotCodec.Decode(content)
}
