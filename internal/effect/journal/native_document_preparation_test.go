package journal

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestPreparedNativeDocumentsAdmitActualPlanBeforeArtifactsExist(t *testing.T) {
	fixture, decoded, comparison := preparedNativeFixture(t)
	artifacts, err := fixture.read.artifacts.Authority()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(artifacts.PhysicalRoot(), "images"), filepath.Join(artifacts.PhysicalRoot(), "unavailable-images")); err != nil {
		t.Fatal(err)
	}
	prepared, err := decoded.prepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.read.limits, fixture.read.codecs)
	if err != nil || len(prepared.documents.groups) != 1 || len(prepared.documents.claims) != 1 || len(prepared.documents.images) != 1 {
		t.Fatalf("actual prepared plan cannot pass pre-artifact construction: %v", err)
	}
	if _, err := loadDocuments(decoded, t.Context(), fixture.read); err == nil {
		t.Fatal("unavailable artifact fixture unexpectedly remained readable")
	}
	mode := recovery.NewPermissionMode(0o600)
	for _, image := range []loadedDocumentImage{prepared.documents.images[0].original, prepared.documents.images[0].expected} {
		match, err := prepared.documents.groups[0].Comparison().Match(t.Context(), image.document, mode)
		if err != nil || !match.Recoverable() {
			t.Fatalf("prepared construction lost an actual comparison image: %+v, %v", match, err)
		}
	}
}

func TestPreparedNativeDocumentsRejectMismatchedImagesAndWholeOperation(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*testing.T, *nativeRecordFixture, *nativeJournalSnapshots, *[]journaldocument.Comparison)
	}{
		{"missing comparison", func(_ *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]journaldocument.Comparison) {
			*comparisons = nil
		}},
		{"extra comparison", func(_ *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]journaldocument.Comparison) {
			*comparisons = append(*comparisons, (*comparisons)[0])
		}},
		{"zero comparison", func(_ *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]journaldocument.Comparison) {
			(*comparisons)[0] = journaldocument.Comparison{}
		}},
		{"wrong original digest", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]journaldocument.Comparison) {
			journal.record.DocumentGroups[0].Original.Artifact.Digest = artifact.HashFileContent([]byte("other original"))
		}},
		{"wrong expected digest", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]journaldocument.Comparison) {
			journal.record.DocumentGroups[0].Expected.Artifact.Digest = artifact.HashFileContent([]byte("other expected"))
		}},
		{"wrong expected size", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]journaldocument.Comparison) {
			size := *journal.record.DocumentGroups[0].Expected.Artifact.Size + 1
			journal.record.DocumentGroups[0].Expected.Artifact.Size = &size
		}},
		{"wrong expected mode", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]journaldocument.Comparison) {
			journal.record.DocumentGroups[0].Expected.Mode = recovery.NewPermissionMode(0o640)
		}},
		{"wrong expected presence", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]journaldocument.Comparison) {
			journal.record.DocumentGroups[0].Expected = recoveryDocumentImage{Kind: "absent"}
		}},
		{"different limits", func(t *testing.T, fixture *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]journaldocument.Comparison) {
			input := preparedComparisonInput((*comparisons)[0])
			input.Limits.MaximumBytes--
			comparison, err := journaldocument.NewSelectedComparison(t.Context(), input, []compose.Participant{preparedParticipant(t, *fixture)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			(*comparisons)[0] = comparison
		}},
		{"wrong extent", func(t *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]journaldocument.Comparison) {
			comparison, err := journaldocument.NewWholeComparison(t.Context(), preparedComparisonInput((*comparisons)[0]))
			if err != nil {
				t.Fatal(err)
			}
			(*comparisons)[0] = comparison
		}},
		{"wrong selected frontier", func(t *testing.T, _ *nativeRecordFixture, _ *nativeJournalSnapshots, comparisons *[]journaldocument.Comparison) {
			comparison, err := journaldocument.NewSelectedComparison(t.Context(), preparedComparisonInput((*comparisons)[0]), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			(*comparisons)[0] = comparison
		}},
		{"missing changed group", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, comparisons *[]journaldocument.Comparison) {
			journal.record.DocumentGroups = nil
			*comparisons = nil
		}},
		{"wrong after baseline", func(t *testing.T, fixture *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]journaldocument.Comparison) {
			row := fixture.after.ManagedAggregates()[0]
			wrong := selectionNativeRow(t, row.Contribution().Contract(), nativeRecordCanonical(t, `"wrong"`))
			journal.after = selectionState(t, wrong)
		}},
		{"wrong binding before", func(_ *testing.T, _ *nativeRecordFixture, journal *nativeJournalSnapshots, _ *[]journaldocument.Comparison) {
			journal.record.DocumentGroups[0].BindingIntents[0].Before = recoveryDocumentBindingPhase{Kind: "absent"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, decoded, comparison := preparedNativeFixture(t)
			comparisons := []journaldocument.Comparison{comparison}
			test.edit(t, &fixture, &decoded, &comparisons)
			prepared, err := decoded.prepareDocuments(t.Context(), comparisons, fixture.read.limits, fixture.read.codecs)
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
	prepared, err := decoded.prepareDocuments(t.Context(), []journaldocument.Comparison{comparison, {}}, fixture.read.limits, fixture.read.codecs)
	assertNoPreparedDocumentRecord(t, prepared, err, fixture.codec)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	prepared, err = decoded.prepareDocuments(ctx, []journaldocument.Comparison{comparison, {}}, fixture.read.limits, fixture.read.codecs)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	assertNoPreparedDocumentRecord(t, prepared, err, fixture.codec)
	prepared, err = decoded.prepareDocuments(nil, []journaldocument.Comparison{comparison, {}}, fixture.read.limits, fixture.read.codecs)
	assertNoPreparedDocumentRecord(t, prepared, err, fixture.codec)
}

func TestNativeDocumentReaderKeepsMetadataRefusalBeforeImageReads(t *testing.T) {
	for _, kind := range []string{"selection", "claim"} {
		t.Run(kind, func(t *testing.T) {
			fixture, decoded, _ := preparedNativeFixture(t)
			if kind == "selection" {
				decoded.record.DocumentGroups[0].SelectedSubjects[0].Name += "-not-in-state"
			} else {
				decoded.record.DocumentGroups[0].ExclusiveClaims = recoveryDocumentClaimSet{Kind: "absent"}
			}
			reader := &preparedNativeReadCounter{RootedReader: fixture.read.filesystem}
			fixture.read.filesystem = reader
			loaded, err := loadDocuments(decoded, t.Context(), fixture.read)
			assertNoPreparedNativeDocuments(t, loaded, err)
			if reader.reads != 0 {
				t.Fatal("metadata refusal eagerly read private images")
			}
		})
	}
}

func preparedNativeFixture(t *testing.T) (nativeRecordFixture, nativeJournalSnapshots, journaldocument.Comparison) {
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
	participant, err := journaldocument.Participant(selection, fixture.read.codecs)
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

func preparedNativeFixtureFor(t *testing.T, fixture nativeRecordFixture, before aggregate.Document) (nativeRecordFixture, nativeJournalSnapshots, journaldocument.Comparison) {
	t.Helper()
	row := fixture.after.ManagedAggregates()[0]
	contract := row.Contribution().Contract()
	codec, found := fixture.read.codecs.Lookup(contract.CodecContractID())
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
	candidate, err := journaldocument.PrepareSelected(t.Context(), journaldocument.SelectedPreparationInput{Before: before, BeforeMode: mode, ExpectedMode: mode, Limits: fixture.read.limits}, []compose.ProjectionEdit{edit}, nil)
	if err != nil {
		t.Fatal(err)
	}
	nativeRecordImages(t, &fixture, before, candidate.Document())
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
			codec, found := fixture.read.codecs.Lookup(participant.Selection().CodecContractID())
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
			comparison, err := journaldocument.NewSelectedComparison(t.Context(), journaldocument.ComparisonInput{Before: canonical.BeforeDocument(), Expected: canonical.ExpectedDocument(), BeforeMode: canonical.BeforeMode(), ExpectedMode: recovery.NewPermissionMode(0o600), Limits: fixture.read.limits}, []compose.Participant{changed}, nil)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := decoded.prepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.read.limits, fixture.read.codecs)
			assertNoPreparedDocumentRecord(t, prepared, err, fixture.codec)
		})
	}
}

func TestPreparedNativeDocumentsRestoreOnlyThroughOperationCatalog(t *testing.T) {
	fixture, decoded, canonical := preparedNativeFixture(t)
	participant := preparedParticipant(t, fixture)
	codec, found := fixture.read.codecs.Lookup(participant.Selection().CodecContractID())
	if !found {
		t.Fatal("fixture codec missing")
	}
	caller := &preparedNativeCallerCodec{Codec: codec}
	provided, err := compose.NewParticipant(caller, participant.Selection(), participant.Syntax(), participant.RewriteAreas())
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := journaldocument.NewSelectedComparison(t.Context(), journaldocument.ComparisonInput{Before: canonical.BeforeDocument(), Expected: canonical.ExpectedDocument(), BeforeMode: canonical.BeforeMode(), ExpectedMode: recovery.NewPermissionMode(0o600), Limits: fixture.read.limits}, []compose.Participant{provided}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := decoded.prepareDocuments(t.Context(), []journaldocument.Comparison{comparison}, fixture.read.limits, fixture.read.codecs)
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
	codec, found := fixture.read.codecs.Lookup(model.CodecContractID())
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
	input := journaldocument.ComparisonInput{Before: comparison.BeforeDocument(), Expected: comparison.ExpectedDocument(), BeforeMode: comparison.BeforeMode(), ExpectedMode: recovery.NewPermissionMode(0o600), Limits: fixture.read.limits}
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

func assertNoPreparedDocumentRecord(t *testing.T, value preparedDocumentRecord, err error, codec durable.SnapshotCodec) {
	t.Helper()
	assertNoPreparedNativeDocuments(t, value.documents, err)
	if encoded, err := value.encode(t.Context(), codec); err == nil || encoded != nil {
		t.Fatal("invalid preparation produced an encodable record")
	}
}

func assertNoPreparedNativeDocuments(t *testing.T, value nativeDocumentRecords, err error) {
	t.Helper()
	if err == nil || value.groups != nil || value.images != nil || value.claims != nil || value.provisional != nil {
		t.Fatalf("invalid preparation exposed a complete or partial document result: %v", err)
	}
}

type preparedNativeReadCounter struct {
	mutationfs.RootedReader
	reads int
}

func (reader *preparedNativeReadCounter) ReadRootedRegularFileUpTo(ctx context.Context, capability rootedpath.CommitCapability, maximumBytes int64) ([]byte, fs.FileMode, mutationfs.EntryIdentity, error) {
	reader.reads++
	return reader.RootedReader.ReadRootedRegularFileUpTo(ctx, capability, maximumBytes)
}
