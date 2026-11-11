package recoveryoperation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/statefile"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit/stateimage"
)

type nativeAssessmentGroup struct {
	namespace          journalnamespace.DocumentNamespace
	original, expected aggregate.Document
	claims             *ownershipmutation.DocumentClaimTransition
	bindings           []ownership.DocumentBindingIntent
	provisional        []journaldocument.ProvisionalAcquire
	whole              bool
	order              *pipackage.OrderObservation
}

func newNativeAssessmentFixture(t *testing.T) nativeAssessmentFixture {
	t.Helper()
	fixture, roots := nativeNamespaceRecordFixture(t)
	statePath := filepath.Join(namespacetest.LegacyRoot(t, roots, output.RootProject).SelectedPath, "state.json")
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(statePath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.claims.owner, err = stateauthority.New(observed.Exact(), filepath.Join(namespacetest.LegacyRoot(t, roots, output.RootProject).SelectedPath, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	claim := documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "model"))
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.journal.OperationID())
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, fixture.claims)
	namespace, err := journalnamespace.Observe(t.Context(), []aggregate.DocumentAddress{fixture.claims.alias}, roots, namespacetest.Reservations(t), journalTestFilesystem(), nativeFixtureBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	return nativeAssessmentFixture{
		fixture: fixture, roots: roots, statePath: statePath,
		registry: filepath.Join(namespacetest.LegacyRoot(t, roots, output.RootProject).SelectedPath, "ownership.json"),
		host:     filepath.Join(namespacetest.LegacyRoot(t, roots, output.RootPiAgent).SelectedPath, "custom.json"),
		groups: []nativeAssessmentGroup{{
			namespace: namespace, original: fixture.comparison.BeforeDocument(), expected: fixture.comparison.ExpectedDocument(),
			claims: &transition, bindings: []ownership.DocumentBindingIntent{membershipIntent(t, fixture.claims, value, value)},
		}},
	}
}

func (setup nativeAssessmentFixture) image(expected bool) []byte {
	if expected {
		return setup.groups[0].expected.Content()
	}
	return setup.groups[0].original.Content()
}

func (setup *nativeAssessmentFixture) publish(t *testing.T) {
	t.Helper()
	if setup.published {
		return
	}
	fixture := &setup.fixture
	budget := physicalwork.NewCaptureBudget(nil)
	manifest, err := namespacetest.LegacyRoot(t, setup.roots, output.RootProject).Root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := manifest.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := journaldocument.IndexSnapshots(t.Context(), fixture.before, fixture.after)
	if err != nil {
		t.Fatal(err)
	}
	candidates := make([]journalrecord.DocumentGroupCandidate, 0, len(setup.groups))
	for index, group := range setup.groups {
		if group.original.Exists() {
			path, err := group.namespace.WriterPath()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, group.original.Content(), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		subjects := assessmentSubjects(fixture, group)
		selections, err := indexes.SelectedContracts(t.Context(), subjects, group.namespace.Addresses())
		if err != nil {
			t.Fatal(err)
		}
		participants := make([]compose.Participant, 0, len(selections))
		for _, selection := range selections {
			participant, err := journaldocument.Participant(selection, fixture.read.Codecs)
			if err != nil {
				t.Fatal(err)
			}
			participants = append(participants, participant)
		}
		var beforeMode, expectedMode *recovery.PermissionMode
		if group.original.Exists() {
			beforeMode = recovery.NewPermissionMode(0o600)
		}
		if group.expected.Exists() {
			expectedMode = recovery.NewPermissionMode(0o600)
		}
		images := journaldocument.ComparisonInput{Before: group.original, Expected: group.expected, BeforeMode: beforeMode, ExpectedMode: expectedMode, Limits: fixture.read.Limits}
		var comparison journaldocument.Comparison
		if group.whole {
			comparison, err = journaldocument.NewWholeComparison(t.Context(), images)
		} else {
			var selected journaldocument.SelectedCandidate
			selected, err = journalrecord.ReconstructNative16Selected(t.Context(), images, participants, group.order)
			comparison = selected.Comparison()
		}
		if err != nil {
			t.Fatal(err)
		}
		draft, err := journalrecord.BeginDocumentGroup(fixture.claims.owner, fixture.journal.OperationID(), group.bindings, group.claims, group.provisional)
		if err != nil {
			t.Fatal(err)
		}
		aliases := make([]journalrecord.DocumentGroupAlias, 0, len(group.namespace.Aliases()))
		for _, alias := range group.namespace.Aliases() {
			aliases = append(aliases, journalrecord.NewDocumentGroupAlias(alias.Address(), alias.Root().Provenance(), alias.Observation()))
		}
		pin := group.namespace.WriterPin()
		draft, err = draft.WithNamespace(t.Context(), group.namespace.Observation(), pin.Provenance(), pin.Coordinate(), aliases, provenance)
		if err != nil {
			t.Fatal(err)
		}
		if group.order != nil {
			var qualification journalrecord.PiQualification
			draft, qualification, err = draft.WithPiOrder(*group.order)
			if err != nil {
				t.Fatal(err)
			}
			grants, err := group.namespace.QualifyRoots(t.Context(), budget)
			if err != nil {
				t.Fatal(err)
			}
			manifest, present := grants.Manifest()
			if !present {
				t.Fatal("assessment Pi fixture has no manifest grant")
			}
			if err := journalqualification.QualifyPiContext(t.Context(), qualification, manifest, group.namespace, budget); err != nil {
				t.Fatal(err)
			}
		}
		candidate, err := draft.WithComparison(comparison, index, subjects)
		if err != nil {
			t.Fatal(err)
		}
		demands, err := comparison.RemovalDemands(t.Context(), group.namespace.Addresses())
		if err != nil || demands.Len() != 0 {
			t.Fatal("inventory fixture requires a non-removing native group", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := budget.AdmitRemovalDemands(recovery.RemovalDemandSet{}); err != nil {
		t.Fatal(err)
	}
	groups := make([]journalrecord.SealedDocumentGroup, 0, len(candidates))
	comparisons := make([]journaldocument.Comparison, 0, len(candidates))
	for _, candidate := range candidates {
		group, comparison, err := candidate.Seal(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		groups, comparisons = append(groups, group), append(comparisons, comparison)
	}
	journal, err := journalrecord.NewNativeCapture(fixture.journal.OperationID(), time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), fixture.claims.owner, provenance, fixture.before, fixture.after, groups)
	if err != nil {
		t.Fatal(err)
	}
	destination := setup.destination
	if destination == "" {
		destination = filepath.Join(t.TempDir(), "published-assessment")
	}
	fixture.journal, fixture.read.Artifacts = publishNativeFixtureAt(t, destination, journal, comparisons, journalpublication.StageOptions{StateCodec: fixture.codec, Codecs: fixture.read.Codecs, Limits: fixture.read.Limits, Budget: budget})
	fixture.capturedGroups = groups
	setup.published = true
}

func assessmentSubjects(fixture *nativeRecordFixture, group nativeAssessmentGroup) []topology.SubjectID {
	var subjects []topology.SubjectID
	if group.whole {
		return subjects
	}
	seen := make(map[topology.SubjectID]bool)
	for _, snapshot := range []durable.Snapshot{fixture.before, fixture.after} {
		for _, row := range snapshot.ManagedAggregates() {
			for _, alias := range group.namespace.Addresses() {
				if row.Contribution().Contract().Address().Document() == alias && !seen[row.Subject()] {
					subjects = append(subjects, row.Subject())
					seen[row.Subject()] = true
				}
			}
		}
	}
	return subjects
}

type nativeAssessmentFixture struct {
	fixture     nativeRecordFixture
	roots       journalnamespace.Roots
	statePath   string
	registry    string
	host        string
	groups      []nativeAssessmentGroup
	published   bool
	destination string
}

func (setup *nativeAssessmentFixture) reader(t *testing.T) (journalrecord.Native, journalqualification.NativeReadOptions) {
	t.Helper()
	setup.publish(t)
	registry, err := ownershipstore.NewRecoveryReader(setup.registry)
	if err != nil {
		t.Fatal(err)
	}
	documents := setup.fixture.read
	documents.Budget = imageBudget(t)
	return setup.fixture.journal, journalqualification.NativeReadOptions{Documents: documents, Roots: setup.roots, Registry: registry, State: func(ctx context.Context) (durable.Snapshot, error) {
		return statefile.LoadOptionalWithOptions(ctx, setup.statePath, statefile.ReadOptions{Codec: stateimage.Physical(t, setup.fixture.codec), PhysicalPathBudget: documents.Budget})
	}}
}

func (setup *nativeAssessmentFixture) persist(t *testing.T, state durable.Snapshot, registry ownership.Registry, host []byte) {
	t.Helper()
	setup.publish(t)
	stateBytes, err := setup.fixture.codec.Encode(state)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string][]byte{setup.statePath: stateBytes, setup.registry: registryBytes, setup.host: host} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func assessmentRegistry(t *testing.T, claims []ownership.Claim, bindings ...ownership.DocumentBinding) ownership.Registry {
	t.Helper()
	registry, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
