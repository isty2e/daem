package qualification_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/statefile"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
)

func publishedPiOrderFixture(t *testing.T, scope target.Scope) (nativeRecordFixture, pipackage.LoadedOrderInput) {
	t.Helper()
	return publishedPiOrderFixtureWithManifest(t, scope, nil)
}

func publishedPiOrderFixtureWithManifest(t *testing.T, scope target.Scope, selectManifest func(string) journalnamespace.RootSelection) (nativeRecordFixture, pipackage.LoadedOrderInput) {
	t.Helper()
	input, order := piEvidenceFixture(t, scope)
	if len(input.Relations) == 0 {
		t.Fatal("Pi order fixture needs lexical relation contexts")
	}
	command, present := input.Relations[0].SourceContext()
	if !present {
		t.Fatal("Pi order fixture has no command context")
	}
	var manifest journalnamespace.RootSelection
	if selectManifest == nil {
		manifest = nativeNamespaceRoot(t, command.BaseRoot())
	} else {
		manifest = selectManifest(command.BaseRoot())
	}
	pi := nativeNamespaceRoot(t, input.SettingsContext.BaseRoot())
	roots := namespacetest.LegacyRoots(t, map[output.RootRole]journalnamespace.RootSelection{output.RootProject: manifest, output.RootPiAgent: pi})
	if err := os.WriteFile(filepath.Join(pi.SelectedPath, "settings.json"), input.Document.Content(), 0o640); err != nil {
		t.Fatal(err)
	}
	alias, err := journalrecord.PiOrderDocumentAlias(scope)
	if err != nil {
		t.Fatal(err)
	}
	setupBudget := nativeFixtureBudget(t)
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{alias}, roots, journalTestFilesystem(), setupBudget)
	if err != nil {
		t.Fatal(err)
	}
	claims := documentClaimsForTest(t)
	claims.alias = alias
	claims.exact, present = namespace.Observation().Exact()
	if !present {
		t.Fatal("Pi order fixture has no exact namespace observation")
	}
	draft, err := journalrecord.BeginDocumentGroup(claims.owner, "native-pi-order", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := namespace.QualifyRoots(t.Context(), setupBudget)
	if err != nil {
		t.Fatal(err)
	}
	manifestGrant, present := grants.Manifest()
	if !present {
		t.Fatal("Pi order fixture has no manifest grant")
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
	var qualification journalrecord.PiQualification
	draft, qualification, err = draft.WithPiOrder(order)
	if err != nil {
		t.Fatal(err)
	}
	if err := journalqualification.QualifyPiContext(t.Context(), qualification, manifestGrant, namespace, setupBudget); err != nil {
		t.Fatal(err)
	}
	expectedBytes, exists := order.Candidate()
	if !exists {
		t.Fatal("Pi order candidate is absent")
	}
	mode := recovery.NewPermissionMode(0o640)
	limits := configdocument.Limits{MaximumBytes: pipackage.MaximumSettingsBytes, MaximumDepth: 64}
	selected, err := journaldocument.ReconstructSelected(t.Context(), journaldocument.ComparisonInput{Before: input.Document, Expected: aggregate.ExistingDocument(expectedBytes), BeforeMode: mode, ExpectedMode: mode, Limits: limits}, nil, &order)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := draft.WithComparison(selected.Comparison(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	group, comparison, err := candidate.Seal(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	before, after := durable.EmptySnapshot(), durable.EmptySnapshot()
	journal, err := journalrecord.NewNativeCapture("native-pi-order", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), claims.owner, manifestGrant.Provenance(), before, after, []journalrecord.SealedDocumentGroup{group})
	if err != nil {
		t.Fatal(err)
	}
	codec, codecs := statefile.Codec{}, aggregatecodec.Catalog()
	decoded, artifacts := publishNativeFixture(t, journal, comparison, journalpublication.StageOptions{StateCodec: codec, Codecs: codecs, Limits: limits, Budget: setupBudget})
	return nativeRecordFixture{journal: decoded, before: before, after: after, codec: codec, read: journalqualification.ReadOptions{Artifacts: artifacts, Filesystem: journalTestFilesystem(), Budget: nativeFixtureBudget(t), Limits: limits, Codecs: codecs}, claims: claims, roots: roots, comparison: comparison}, input
}
