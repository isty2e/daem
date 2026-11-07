package recoveryoperation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/topology"
)

type nativeRecordFixture struct {
	journal        journalrecord.Native
	before, after  durable.Snapshot
	codec          durable.SnapshotCodec
	read           journalqualification.ReadOptions
	claims         documentClaimFixture
	roots          journalnamespace.Roots
	comparison     journaldocument.Comparison
	capturedGroups []journalrecord.SealedDocumentGroup
}

func requireLoadedNativeDocument(t testing.TB, records journalrecord.LoadedDocuments, index int) journalrecord.LoadedDocument {
	t.Helper()
	value, present := records.At(index)
	if !present {
		t.Fatalf("complete loaded document %d is missing from %d groups", index, records.Len())
	}
	return value
}

func nativeFixtureBudget(t *testing.T) *physicalwork.Budget {
	t.Helper()
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func readStagedNativeRecord(t *testing.T, path string, codec durable.SnapshotCodec) journalrecord.Native {
	t.Helper()
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(parent, filepath.Base(path))
	snapshot, err := journalTestFilesystem().ReadRegularFileSnapshotUpTo(t.Context(), path, journalrecord.MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := journalrecord.DecodeNativeSnapshot(t.Context(), snapshot, path, codec)
	if err != nil {
		t.Fatal(err)
	}
	value, present := loaded.Native()
	if !present {
		t.Fatal("staged native record decoded as another family")
	}
	return value
}

func nativeJournalFixture(t *testing.T) nativeRecordFixture {
	t.Helper()
	claims := documentClaimsForTest(t)
	beforeRow := membershipNativeRow(t, claims.alias, "native-json-v1", "model")
	beforeRow = selectionNativeRow(t, beforeRow.Contribution().Contract(), nativeRecordCanonical(t, "true"))
	afterRow := selectionNativeRow(t, beforeRow.Contribution().Contract(), nativeRecordCanonical(t, `"fast"`))
	before, after := selectionState(t, beforeRow), selectionState(t, afterRow)
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	codecs, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{beforeRow.Contribution().CodecContractID()}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	manifest := nativeNamespaceRoot(t, filepath.Join(t.TempDir(), "manifest"))
	pi := nativeNamespaceRoot(t, filepath.Join(manifest.SelectedPath, "host"))
	rootSet := namespacetest.LegacyRoots(t, map[output.RootRole]journalnamespace.RootSelection{output.RootProject: manifest, output.RootPiAgent: pi})
	original := aggregate.ExistingDocument([]byte(`{"model":true,"outside":1}`))
	if err := os.WriteFile(filepath.Join(pi.SelectedPath, "custom.json"), original.Content(), 0o600); err != nil {
		t.Fatal(err)
	}
	setupBudget := nativeFixtureBudget(t)
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{claims.alias}, rootSet, journalTestFilesystem(), setupBudget)
	if err != nil {
		t.Fatal(err)
	}
	exact, present := namespace.Observation().Exact()
	if !present {
		t.Fatal("published fixture requires an exact host observation")
	}
	claims.exact = exact
	claim := documentActiveClaim(t, claims, beforeRow.Contribution().Contract().Address().Footprint())
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, "native-record-apply")
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, claims)
	bindings := []ownership.DocumentBindingIntent{membershipIntent(t, claims, value, value)}
	draft, err := journalrecord.BeginDocumentGroup(claims.owner, "native-record-apply", bindings, &transition, nil)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := namespace.QualifyRoots(t.Context(), setupBudget)
	if err != nil {
		t.Fatal(err)
	}
	manifestGrant, present := grants.Manifest()
	if !present {
		t.Fatal("published fixture requires a manifest grant")
	}
	aliases := make([]journalrecord.DocumentGroupAlias, 0, len(namespace.Aliases()))
	for _, alias := range namespace.Aliases() {
		aliases = append(aliases, journalrecord.NewDocumentGroupAlias(alias.Address(), alias.Root().Provenance(), alias.Observation()))
	}
	pin := namespace.WriterPin()
	draft, err = draft.WithNamespace(t.Context(), namespace.Observation(), pin.Provenance(), pin.Relative(), aliases, manifestGrant.Provenance())
	if err != nil {
		t.Fatal(err)
	}
	comparison := publishedNativeComparison(t, original, afterRow, codecs, limits)
	candidate, err := draft.WithComparison(comparison, 0, []topology.SubjectID{afterRow.Subject()})
	if err != nil {
		t.Fatal(err)
	}
	group, comparison, err := candidate.Seal(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := journalrecord.NewNativeCapture("native-record-apply", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), claims.owner, manifestGrant.Provenance(), before, after, []journalrecord.SealedDocumentGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	decoded, artifacts := publishNativeFixture(t, journal, comparison, journalpublication.StageOptions{StateCodec: codec, Codecs: codecs, Limits: limits, Budget: setupBudget})
	return nativeRecordFixture{journal: decoded, before: before, after: after, codec: codec, read: journalqualification.ReadOptions{Artifacts: artifacts, Filesystem: journalTestFilesystem(), Budget: nativeFixtureBudget(t), Limits: limits, Codecs: codecs}, claims: claims, roots: rootSet, comparison: comparison, capturedGroups: []journalrecord.SealedDocumentGroup{group}}
}

func publishedNativeComparison(t *testing.T, before aggregate.Document, row durable.ManagedAggregateState, codecs aggregate.CodecCatalog, limits configdocument.Limits) journaldocument.Comparison {
	t.Helper()
	contract := row.Contribution().Contract()
	codec, present := codecs.Lookup(contract.CodecContractID())
	if !present {
		t.Fatal("fixture codec is missing")
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
	contributions, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := aggregate.NewProjectionIntent(baseline.States()[0], &contributions)
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
	candidate, err := journaldocument.PrepareSelected(t.Context(), journaldocument.SelectedPreparationInput{Before: before, BeforeMode: mode, ExpectedMode: mode, Limits: limits}, []compose.ProjectionEdit{edit}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return candidate.Comparison()
}

func publishedNativeGroupVariant(t *testing.T, fixture nativeRecordFixture, revise func(map[string]json.RawMessage)) journalrecord.Native {
	t.Helper()
	return publishedNativeVariant(t, fixture, func(object map[string]json.RawMessage) {
		var groups []map[string]json.RawMessage
		if err := json.Unmarshal(object["document_groups"], &groups); err != nil {
			t.Fatal(err)
		}
		if len(groups) != 1 {
			t.Fatal("published variant requires exactly one document group")
		}
		revise(groups[0])
		object["document_groups"] = nativeRecordJSON(t, groups)
	})
}

func publishedNativeVariant(t *testing.T, fixture nativeRecordFixture, revise func(map[string]json.RawMessage)) journalrecord.Native {
	t.Helper()
	content, err := fixture.journal.Encode(t.Context(), fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(content, &object); err != nil {
		t.Fatal(err)
	}
	revise(object)
	content = nativeRecordJSON(t, object)
	authority, err := fixture.read.Artifacts.Authority()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authority.PhysicalRoot(), journalrecord.FileName), content, journalrecord.Mode); err != nil {
		t.Fatal(err)
	}
	snapshot, err := journalqualification.ReadJournalSnapshot(t.Context(), fixture.read.Artifacts, journalTestFilesystem(), nativeFixtureBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := journalrecord.DecodeNativeSnapshot(t.Context(), snapshot, journalrecord.FileName, fixture.codec)
	if err != nil {
		t.Fatal("persisted variant failed decode before the property under test", err)
	}
	journal, present := loaded.Native()
	if !present {
		t.Fatal("persisted variant changed record family")
	}
	return journal
}

func publishNativeFixture(t *testing.T, journal journalrecord.Native, comparison journaldocument.Comparison, options journalpublication.StageOptions) (journalrecord.Native, *rootedpath.CapturedRoot) {
	t.Helper()
	return publishNativeFixtureAt(t, filepath.Join(t.TempDir(), "published-journal"), journal, []journaldocument.Comparison{comparison}, options)
}

func publishNativeFixtureAt(t *testing.T, destination string, journal journalrecord.Native, comparisons []journaldocument.Comparison, options journalpublication.StageOptions) (journalrecord.Native, *rootedpath.CapturedRoot) {
	t.Helper()
	codec, codecs, limits, setupBudget := options.StateCodec, options.Codecs, options.Limits, options.Budget
	root, bound, err := rootedpath.CaptureDestination(destination)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	capability, err := root.Acquire(bound)
	if err != nil {
		t.Fatal(err)
	}
	defer capability.Close()
	prepared, err := journalTestFilesystem().PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		_, err := journalpublication.StageDocuments(journal, t.Context(), comparisons, writer, journalpublication.StageOptions{StateCodec: codec, Codecs: codecs, Limits: limits, Budget: setupBudget})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort(context.Background())
	if err := prepared.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	artifacts, err := rootedpath.CaptureRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = artifacts.Close() })
	snapshot, err := journalqualification.ReadJournalSnapshot(t.Context(), artifacts, journalTestFilesystem(), setupBudget)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := journalrecord.DecodeNativeSnapshot(t.Context(), snapshot, journalrecord.FileName, codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, present := loaded.Native()
	if !present {
		t.Fatal("published native journal decoded as another family")
	}
	return decoded, artifacts
}

func nativeNamespaceRecordFixture(t *testing.T) (nativeRecordFixture, journalnamespace.Roots) {
	t.Helper()
	fixture := nativeJournalFixture(t)
	return fixture, fixture.roots
}
