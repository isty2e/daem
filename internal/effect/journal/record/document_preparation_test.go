package record

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/supply/artifact"
)

type preparedNativeCallerCodec struct {
	aggregate.Codec
	refuse bool
}

func (codec *preparedNativeCallerCodec) Read(document aggregate.Document, selection aggregate.Selection) (aggregate.Snapshot, *aggregate.CodecFailure) {
	if codec.refuse {
		failure, _ := aggregate.NewCodecFailure(aggregate.CodecFailureResourceLimitExceeded, output.KeyPath{})
		return aggregate.Snapshot{}, failure
	}
	return codec.Codec.Read(document, selection)
}

func assertNoPreparedDocumentRecord(t *testing.T, value PreparedDocuments, err error, codec durable.SnapshotCodec) {
	t.Helper()
	if err == nil || value.initialized || value.documents.Len() != 0 {
		t.Fatalf("invalid preparation exposed complete or partial facts: %v", err)
	}
	if encoded, err := value.Encode(t.Context(), codec); err == nil || encoded != nil {
		t.Fatal("invalid preparation produced an encodable record")
	}
}

func TestPreparedNativeDocumentsAdmitActualPlanBeforeArtifactsExist(t *testing.T) {
	fixture, decoded, comparison := preparedNativeFixture(t)
	prepared, err := decoded.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.limits, fixture.codecs)
	if err != nil || len(prepared.documents.groups) != 1 || len(prepared.documents.claims) != 1 || len(prepared.documents.images) != 1 {
		t.Fatalf("actual prepared plan cannot pass pre-artifact construction: %v", err)
	}
	mode := recovery.NewPermissionMode(0o600)
	for _, image := range []LoadedImage{prepared.documents.images[0].original, prepared.documents.images[0].expected} {
		match, err := prepared.documents.groups[0].Comparison().Match(t.Context(), image.document, mode)
		if err != nil || !match.Recoverable() {
			t.Fatalf("prepared construction lost an actual comparison image: %+v, %v", match, err)
		}
	}
}

func TestPreparedNativeDocumentsRejectMismatchedImagesAndWholeOperation(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*testing.T, *nativeRecordFixture, *Native, *[]journaldocument.Comparison)
	}{
		{"missing comparison", func(_ *testing.T, _ *nativeRecordFixture, _ *Native, comparisons *[]journaldocument.Comparison) {
			*comparisons = nil
		}},
		{"extra comparison", func(_ *testing.T, _ *nativeRecordFixture, _ *Native, comparisons *[]journaldocument.Comparison) {
			*comparisons = append(*comparisons, (*comparisons)[0])
		}},
		{"zero comparison", func(_ *testing.T, _ *nativeRecordFixture, _ *Native, comparisons *[]journaldocument.Comparison) {
			(*comparisons)[0] = journaldocument.Comparison{}
		}},
		{"wrong original digest", func(_ *testing.T, _ *nativeRecordFixture, journal *Native, _ *[]journaldocument.Comparison) {
			journal.record.DocumentGroups[0].Original.Artifact.Digest = artifact.HashFileContent([]byte("other original"))
		}},
		{"wrong expected digest", func(_ *testing.T, _ *nativeRecordFixture, journal *Native, _ *[]journaldocument.Comparison) {
			journal.record.DocumentGroups[0].Expected.Artifact.Digest = artifact.HashFileContent([]byte("other expected"))
		}},
		{"wrong expected size", func(_ *testing.T, _ *nativeRecordFixture, journal *Native, _ *[]journaldocument.Comparison) {
			size := *journal.record.DocumentGroups[0].Expected.Artifact.Size + 1
			journal.record.DocumentGroups[0].Expected.Artifact.Size = &size
		}},
		{"wrong expected mode", func(_ *testing.T, _ *nativeRecordFixture, journal *Native, _ *[]journaldocument.Comparison) {
			journal.record.DocumentGroups[0].Expected.Mode = recovery.NewPermissionMode(0o640)
		}},
		{"wrong expected presence", func(_ *testing.T, _ *nativeRecordFixture, journal *Native, _ *[]journaldocument.Comparison) {
			journal.record.DocumentGroups[0].Expected = recoveryDocumentImage{Kind: "absent"}
		}},
		{"different limits", func(t *testing.T, fixture *nativeRecordFixture, _ *Native, comparisons *[]journaldocument.Comparison) {
			input := preparedComparisonInput((*comparisons)[0])
			input.Limits.MaximumBytes--
			comparison, err := journaldocument.NewSelectedComparison(t.Context(), input, []compose.Participant{preparedParticipant(t, *fixture)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			(*comparisons)[0] = comparison
		}},
		{"wrong extent", func(t *testing.T, _ *nativeRecordFixture, _ *Native, comparisons *[]journaldocument.Comparison) {
			comparison, err := journaldocument.NewWholeComparison(t.Context(), preparedComparisonInput((*comparisons)[0]))
			if err != nil {
				t.Fatal(err)
			}
			(*comparisons)[0] = comparison
		}},
		{"wrong selected frontier", func(t *testing.T, _ *nativeRecordFixture, _ *Native, comparisons *[]journaldocument.Comparison) {
			comparison, err := journaldocument.NewSelectedComparison(t.Context(), preparedComparisonInput((*comparisons)[0]), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			(*comparisons)[0] = comparison
		}},
		{"missing changed group", func(_ *testing.T, _ *nativeRecordFixture, journal *Native, comparisons *[]journaldocument.Comparison) {
			journal.record.DocumentGroups = nil
			*comparisons = nil
		}},
		{"wrong after baseline", func(t *testing.T, fixture *nativeRecordFixture, journal *Native, _ *[]journaldocument.Comparison) {
			row := fixture.after.ManagedAggregates()[0]
			wrong := selectionNativeRow(t, row.Contribution().Contract(), nativeRecordCanonical(t, `"wrong"`))
			journal.after = selectionState(t, wrong)
		}},
		{"wrong binding before", func(_ *testing.T, _ *nativeRecordFixture, journal *Native, _ *[]journaldocument.Comparison) {
			journal.record.DocumentGroups[0].BindingIntents[0].Before = recoveryDocumentBindingPhase{Kind: "absent"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, decoded, comparison := preparedNativeFixture(t)
			comparisons := []journaldocument.Comparison{comparison}
			test.edit(t, &fixture, &decoded, &comparisons)
			prepared, err := decoded.PrepareDocuments(t.Context(), comparisons, fixture.limits, fixture.codecs)
			assertNoPreparedDocumentRecord(t, prepared, err, fixture.codec)
		})
	}
}

func TestPreparedNativeDocumentsDiscardEarlierGroupOnLaterFailure(t *testing.T) {
	fixture, decoded, comparison := preparedNativeFixture(t)
	late := decoded.record.DocumentGroups[0]
	late.Aliases = append([]recoveryDocumentAlias(nil), late.Aliases...)
	late.Aliases[0].Address.Destination = "@pi-agent/settings.json"
	late.SelectedSubjects = []persistedSubjectRef{}
	late.BindingIntents = []recoveryDocumentBindingIntent{}
	late.ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
	late.Original, late.Expected = recoveryDocumentImage{Kind: "absent"}, recoveryDocumentImage{Kind: "absent"}
	decoded.record.DocumentGroups = append(decoded.record.DocumentGroups, late)
	prepared, err := decoded.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison, {}}, fixture.limits, fixture.codecs)
	assertNoPreparedDocumentRecord(t, prepared, err, fixture.codec)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	prepared, err = decoded.PrepareDocuments(ctx, []journaldocument.Comparison{comparison, {}}, fixture.limits, fixture.codecs)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	assertNoPreparedDocumentRecord(t, prepared, err, fixture.codec)
	prepared, err = decoded.PrepareDocuments(nil, []journaldocument.Comparison{comparison, {}}, fixture.limits, fixture.codecs)
	assertNoPreparedDocumentRecord(t, prepared, err, fixture.codec)
}

func TestPreparedNativeDocumentsRejectNoncanonicalRecoveryPolicy(t *testing.T) {
	for _, policy := range []string{"broader area", "different syntax"} {
		t.Run(policy, func(t *testing.T) {
			fixture := nativeJournalFixture(t)
			row := membershipNativeRow(t, fixture.claims.alias, "native-json-v1", "preferences", "model")
			beforeRow := selectionNativeRow(t, row.Contribution().Contract(), nativeRecordCanonical(t, "true"))
			afterRow := selectionNativeRow(t, row.Contribution().Contract(), nativeRecordCanonical(t, `"fast"`))
			fixture.before, fixture.after = selectionState(t, beforeRow), selectionState(t, afterRow)
			fixture.record.DocumentGroups[0].SelectedSubjects = []persistedSubjectRef{{Kind: string(row.Subject().Kind()), Namespace: row.Subject().Namespace(), Name: row.Subject().Key()}}
			claim := documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "preferences", "model"))
			transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.record.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			fixture.record.DocumentGroups[0].ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			fixture, decoded, canonical := preparedNativeFixtureFor(t, fixture, aggregate.ExistingDocument([]byte(`{"preferences":{"model":true,"foreign":1},"outside":2}`)))
			participant := preparedParticipant(t, fixture)
			codec, found := fixture.codecs.Lookup(participant.Selection().CodecContractID())
			if !found {
				t.Fatal("fixture codec missing")
			}
			syntax, areas := participant.Syntax(), participant.RewriteAreas()
			if policy == "broader area" {
				parent, err := output.NewKeyPath("preferences")
				if err != nil {
					t.Fatal(err)
				}
				areas = []output.KeyPath{parent}
			} else {
				syntax = configdocument.JSONC
			}
			changed, err := compose.NewParticipant(codec, participant.Selection(), syntax, areas)
			if err != nil {
				t.Fatal(err)
			}
			comparison, err := journaldocument.NewSelectedComparison(t.Context(), journaldocument.ComparisonInput{Before: canonical.BeforeDocument(), Expected: canonical.ExpectedDocument(), BeforeMode: canonical.BeforeMode(), ExpectedMode: recovery.NewPermissionMode(0o600), Limits: fixture.limits}, []compose.Participant{changed}, nil)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := decoded.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.limits, fixture.codecs)
			assertNoPreparedDocumentRecord(t, prepared, err, fixture.codec)
		})
	}
}

func TestPreparedNativeDocumentsRestoreOnlyThroughOperationCatalog(t *testing.T) {
	fixture, decoded, canonical := preparedNativeFixture(t)
	participant := preparedParticipant(t, fixture)
	codec, found := fixture.codecs.Lookup(participant.Selection().CodecContractID())
	if !found {
		t.Fatal("fixture codec missing")
	}
	caller := &preparedNativeCallerCodec{Codec: codec}
	provided, err := compose.NewParticipant(caller, participant.Selection(), participant.Syntax(), participant.RewriteAreas())
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := journaldocument.NewSelectedComparison(t.Context(), journaldocument.ComparisonInput{Before: canonical.BeforeDocument(), Expected: canonical.ExpectedDocument(), BeforeMode: canonical.BeforeMode(), ExpectedMode: recovery.NewPermissionMode(0o600), Limits: fixture.limits}, []compose.Participant{provided}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := decoded.PrepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.limits, fixture.codecs)
	if err != nil {
		t.Fatal(err)
	}
	caller.refuse = true
	current := aggregate.ExistingDocument([]byte(`{"model":"fast","outside":"fresh"}`))
	restored, err := prepared.documents.groups[0].Comparison().RestoreBefore(t.Context(), current, recovery.NewPermissionMode(0o600))
	if err != nil {
		t.Fatalf("prepared recovery retained caller codec behavior instead of operation catalog: %v", err)
	}
	var values struct {
		Model   bool   `json:"model"`
		Outside string `json:"outside"`
	}
	if err := json.Unmarshal(restored.Document().Content(), &values); err != nil || !values.Model || values.Outside != "fresh" {
		t.Fatalf("catalog-bound restoration lost selected before or foreign current value: %v", err)
	}
}

func TestPreparedRecoveryPolicyAllowsReorderedPartitions(t *testing.T) {
	fixture, _, comparison := preparedNativeFixture(t)
	model := preparedParticipant(t, fixture).Selection().Contracts()[0]
	outside := membershipNativeRow(t, fixture.claims.alias, "native-json-v1", "outside").Contribution().Contract()
	codec, found := fixture.codecs.Lookup(model.CodecContractID())
	if !found {
		t.Fatal("fixture codec missing")
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{model, outside})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := compose.NewParticipant(codec, selection, configdocument.JSON, []output.KeyPath{model.Address().KeyPath(), outside.Address().KeyPath()})
	if err != nil {
		t.Fatal(err)
	}
	input := journaldocument.ComparisonInput{Before: comparison.BeforeDocument(), Expected: comparison.ExpectedDocument(), BeforeMode: comparison.BeforeMode(), ExpectedMode: recovery.NewPermissionMode(0o600), Limits: fixture.limits}
	canonical, err := journaldocument.NewSelectedComparison(t.Context(), input, []compose.Participant{participant}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var parts []compose.Participant
	for _, contract := range []aggregate.ProjectionContract{outside, model} {
		selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
		if err != nil {
			t.Fatal(err)
		}
		participant, err := compose.NewParticipant(codec, selection, configdocument.JSON, []output.KeyPath{contract.Address().KeyPath()})
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, participant)
	}
	partitioned, err := journaldocument.NewSelectedComparison(t.Context(), input, parts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := partitioned.RequireCanonicalRecoveryPolicy(t.Context(), canonical); err != nil {
		t.Fatalf("equivalent reordered policy partitions were refused: %v", err)
	}
}
