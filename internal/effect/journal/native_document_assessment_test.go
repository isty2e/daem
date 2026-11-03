package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type nativeAssessmentFixture struct {
	fixture   nativeRecordFixture
	roots     NativeDocumentRoots
	statePath string
	registry  string
	host      string
}

func TestNativeDocumentAssessmentUsesActualStateRegistryAndHost(t *testing.T) {
	for _, test := range []struct {
		name, state, host string
		want              nativeDocumentDisposition
	}{
		{"before/no-op", "before", "before", nativeDocumentsBefore},
		{"before/rollback", "before", "after", nativeDocumentsRollback},
		{"after/clean", "after", "after", nativeDocumentsAfter},
		{"after/refuses-before", "after", "before", ""},
		{"before/refuses-third", "before", "third", ""},
		{"after/refuses-third", "after", "third", ""},
		{"neither/refuses", "neither", "after", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			setup := newNativeAssessmentFixture(t)
			fixture := &setup.fixture
			claims := fixtureTransition(t, *fixture)
			binding, _ := membershipBindingValue(t, fixture.claims).Get()
			registry := assessmentRegistry(t, claims.Before(), binding)
			state := fixture.before
			if test.state == "after" {
				state = fixture.after
			} else if test.state == "neither" {
				state = durable.EmptySnapshot()
			}
			host := assessmentImage(t, *fixture, test.host == "after")
			if test.host == "third" {
				host = []byte(`{"model":false,"outside":1}`)
			}
			setup.persist(t, state, registry, host)
			journal, options := setup.reader(t)
			result, err := journal.assessDocumentRecovery(t.Context(), options)
			if test.want == "" {
				if err == nil || result.stages != nil || result.disposition != "" {
					t.Fatalf("refused phase returned usable assessment: %v", err)
				}
			} else if err != nil || result.disposition != test.want || len(result.stages) != 1 {
				t.Fatalf("disposition=%s, want=%s: %v", result.disposition, test.want, err)
			}
			setup.assertPersisted(t, state, registry, host)
		})
	}
}

func TestNativeDocumentAssessmentRequiresCompleteClaimAndBindingPhases(t *testing.T) {
	for _, test := range []struct {
		name, state, claims, binding string
		want                         nativeDocumentDisposition
	}{
		{"rollback-unprepared", "before", "before", "before", nativeDocumentsRollback},
		{"rollback-prepared", "before", "prepared", "before", nativeDocumentsRollback},
		{"rollback-refuses-active", "before", "after", "before", ""},
		{"rollback-refuses-after-binding", "before", "prepared", "after", ""},
		{"rollback-refuses-third-binding", "before", "prepared", "third", ""},
		{"finalize-refuses-third-binding", "after", "prepared", "third", ""},
		{"finalize-both", "after", "prepared", "before", nativeDocumentsFinalize},
		{"finalize-claims", "after", "prepared", "after", nativeDocumentsFinalize},
		{"finalize-binding", "after", "after", "before", nativeDocumentsFinalize},
		{"after-complete", "after", "after", "after", nativeDocumentsAfter},
		{"after-refuses-absent-claims", "after", "before", "before", ""},
		{"before-refuses-third", "before", "third", "before", ""},
		{"after-refuses-third", "after", "third", "after", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			setup := newNativeAssessmentFixture(t)
			fixture := &setup.fixture
			fixture.before = durable.EmptySnapshot()
			nativeRecordImages(t, fixture, aggregate.ExistingDocument([]byte(`{"outside":1}`)), aggregate.ExistingDocument([]byte(`{"model":"fast","outside":1}`)))
			afterClaim := documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "model"))
			transition, err := ownershipmutation.NewDocumentClaimTransition(nil, []ownership.Claim{afterClaim}, fixture.record.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			fixture.record.DocumentGroups[0].ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			bindingValue := membershipBindingValue(t, fixture.claims)
			binding, _ := bindingValue.Get()
			fixture.record.DocumentGroups[0].BindingIntents = []recoveryDocumentBindingIntent{assessmentBinding(t, membershipIntent(t, fixture.claims, ownership.NoDocumentBinding(), bindingValue))}
			claims := transition.Before()
			switch test.claims {
			case "prepared":
				claims = transition.Prepared()
			case "after":
				claims = transition.After()
			case "third":
				third, err := ownership.NewReservedClaim(afterClaim.Address(), fixture.claims.owner, "other-operation")
				if err != nil {
					t.Fatal(err)
				}
				claims = []ownership.Claim{third}
			}
			var bindings []ownership.DocumentBinding
			if test.binding == "after" {
				bindings = append(bindings, binding)
			} else if test.binding == "third" {
				other := filepath.Join(setup.roots.PiAgent.SelectedPath, "foreign.json")
				if err := os.WriteFile(other, []byte("foreign binding target"), 0o600); err != nil {
					t.Fatal(err)
				}
				observed, err := mutation.ObservePersistedDirectoryEntryAuthority(other)
				if err != nil {
					t.Fatal(err)
				}
				third, err := ownership.NewDocumentBinding(fixture.claims.owner, fixture.claims.alias.AggregateRoot(), observed.Exact())
				if err != nil {
					t.Fatal(err)
				}
				bindings = append(bindings, third)
			}
			registry := assessmentRegistry(t, claims, bindings...)
			state := fixture.before
			if test.state == "after" {
				state = fixture.after
			}
			host := assessmentImage(t, *fixture, true)
			setup.persist(t, state, registry, host)
			journal, options := setup.reader(t)
			result, err := journal.assessDocumentRecovery(t.Context(), options)
			if test.want == "" {
				if err == nil || result.stages != nil || result.disposition != "" {
					t.Fatalf("metadata mismatch returned a usable prefix: %v", err)
				}
			} else if err != nil || result.disposition != test.want {
				t.Fatalf("metadata disposition=%s want=%s: %v", result.disposition, test.want, err)
			}
			setup.assertPersisted(t, state, registry, host)
		})
	}
}

func TestNativeDocumentAssessmentStagesExactNarrowingBeforeSemanticNoOp(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact-expected", true: "released-external-edit"}[external], func(t *testing.T) {
			setup := newNativeAssessmentFixture(t)
			fixture := &setup.fixture
			original := []byte("{\n  \"model\": true,\n  \"outside\": 1\n}\n")
			expected := []byte(`{"model":"fast","new_key":2,"outside":1}`)
			nativeRecordImages(t, fixture, aggregate.ExistingDocument(original), aggregate.ExistingDocument(expected))
			id, _, found := topologyprojection.HostConfigKeyIdentity(fixture.after.ManagedAggregates()[0].Subject())
			if !found {
				t.Fatal("native declaration identity missing")
			}
			subject, err := aggregate.HostConfigDocumentSubject(id, target.TargetPi, target.ScopeGlobal)
			if err != nil {
				t.Fatal(err)
			}
			whole, err := durable.NewManagedPathState(subject, []target.Target{target.TargetPi}, target.ScopeGlobal, fixture.claims.alias.AggregateRoot(), artifact.HashFileContent(original), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			fixture.before, err = durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{whole}})
			if err != nil {
				t.Fatal(err)
			}
			added := selectionNativeRow(t, stateGroupContract(t, fixture.claims.alias, "new_key"), nativeRecordCanonical(t, "2"))
			fixture.after = selectionState(t, fixture.after.ManagedAggregates()[0], added)
			fixture.record.DocumentGroups[0].SelectedSubjects = append(fixture.record.DocumentGroups[0].SelectedSubjects, persistedSubjectRef{Kind: string(added.Subject().Kind()), Namespace: added.Subject().Namespace(), Name: added.Subject().Key()})
			beforeClaim := documentActiveClaim(t, fixture.claims, output.WholePathFootprint())
			afterClaims := []ownership.Claim{documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "model")), documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "new_key"))}
			transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{beforeClaim}, afterClaims, fixture.record.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			fixture.record.DocumentGroups[0].ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			binding, _ := membershipBindingValue(t, fixture.claims).Get()
			registry := assessmentRegistry(t, []ownership.Claim{beforeClaim}, binding)
			host := expected
			if external {
				host = []byte(`{"model":"fast","new_key":2,"outside":7}`)
			}
			setup.persist(t, fixture.before, registry, host)
			journal, options := setup.reader(t)
			result, err := journal.assessDocumentRecovery(t.Context(), options)
			if err != nil || result.disposition != nativeDocumentsRollback || len(result.stages) != 1 {
				t.Fatalf("narrowing assessment: %v", err)
			}
			restored := result.stages[0].restoration.Document().Content()
			if !external && !bytes.Equal(restored, original) {
				t.Fatalf("unmodified expected did not stage exact actual original: %s", restored)
			}
			if external {
				var values map[string]any
				if err := json.Unmarshal(restored, &values); err != nil || values["outside"] != float64(7) || values["model"] != true || values["new_key"] != nil {
					t.Fatalf("selected fallback overwrote released edit or lost original selected values: %s, %v", restored, err)
				}
			}
			setup.assertPersisted(t, fixture.before, registry, host)
			if err := os.WriteFile(setup.host, restored, 0o600); err != nil {
				t.Fatal(err)
			}
			journal, options = setup.reader(t)
			retry, err := journal.assessDocumentRecovery(t.Context(), options)
			if err != nil || retry.disposition != nativeDocumentsBefore || retry.stages[0].restoration.Changed() {
				t.Fatalf("retry did not recognize actual no-op: %v", err)
			}
		})
	}
}

func TestNativeDocumentAssessmentEqualStateAndObservationFailures(t *testing.T) {
	setup := newNativeAssessmentFixture(t)
	fixture := &setup.fixture
	fixture.after = fixture.before
	before := assessmentImage(t, *fixture, false)
	nativeRecordImages(t, fixture, aggregate.ExistingDocument(before), aggregate.ExistingDocument(before))
	transition := fixtureTransition(t, *fixture)
	binding, _ := membershipBindingValue(t, fixture.claims).Get()
	registry := assessmentRegistry(t, transition.Before(), binding)
	setup.persist(t, fixture.before, registry, before)
	journal, options := setup.reader(t)
	result, err := journal.assessDocumentRecovery(t.Context(), options)
	if err != nil || result.disposition != nativeDocumentsAfter || result.stages[0].restoration.Changed() {
		t.Fatalf("equal-state expected should remain clean-after: %v", err)
	}

	for _, failure := range []string{"state reader", "cancellation", "host budget", "host read"} {
		t.Run(failure, func(t *testing.T) {
			journal, options := setup.reader(t)
			sentinel := errors.New("native observation refused")
			switch failure {
			case "state reader":
				options.state = func(context.Context) (durable.Snapshot, error) { return durable.Snapshot{}, sentinel }
			case "cancellation":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				result, err := journal.assessDocumentRecovery(ctx, options)
				if !errors.Is(err, context.Canceled) || result.stages != nil {
					t.Fatalf("cancellation returned a usable prefix: %v", err)
				}
				return
			case "host budget":
				read := options.state
				options.state = func(ctx context.Context) (durable.Snapshot, error) {
					work, err := recovery.NewArtifactWork(0, options.documents.budget.RemainingBytes())
					if err == nil {
						err = options.documents.budget.AdmitTree(work)
					}
					if err != nil {
						return durable.Snapshot{}, err
					}
					return read(ctx)
				}
			case "host read":
				read := options.state
				options.state = func(ctx context.Context) (durable.Snapshot, error) {
					if err := os.WriteFile(setup.host, []byte("invalid selected JSON"), 0o600); err != nil {
						return durable.Snapshot{}, err
					}
					return read(ctx)
				}
			}
			result, err := journal.assessDocumentRecovery(t.Context(), options)
			if err == nil || result.stages != nil || result.disposition != "" || failure == "state reader" && !errors.Is(err, sentinel) {
				t.Fatalf("observation failure returned a usable prefix: %v", err)
			}
		})
	}
}

func newNativeAssessmentFixture(t *testing.T) nativeAssessmentFixture {
	t.Helper()
	fixture, roots := nativeNamespaceRecordFixture(t)
	statePath := filepath.Join(roots.Manifest.SelectedPath, "state.json")
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(statePath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.claims.owner, err = stateauthority.New(observed.Exact(), filepath.Join(roots.Manifest.SelectedPath, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.record.StateAuthority = recoveryStateAuthority{StatefileAuthority: *persistedPathAuthority(fixture.claims.owner.StatefileAuthority()), ManifestPath: fixture.claims.owner.ManifestPath()}
	claim := documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "model"))
	transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, []ownership.Claim{claim}, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.record.DocumentGroups[0].ExclusiveClaims, err = persistedDocumentClaimTransition(transition, fixture.record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	value := membershipBindingValue(t, fixture.claims)
	fixture.record.DocumentGroups[0].BindingIntents = []recoveryDocumentBindingIntent{assessmentBinding(t, membershipIntent(t, fixture.claims, value, value))}
	return nativeAssessmentFixture{fixture: fixture, roots: roots, statePath: statePath, registry: filepath.Join(roots.Manifest.SelectedPath, "ownership.json"), host: filepath.Join(roots.PiAgent.SelectedPath, "custom.json")}
}

func (setup nativeAssessmentFixture) reader(t *testing.T) (nativeJournalSnapshots, nativeDocumentRecoveryReadOptions) {
	t.Helper()
	content, err := marshalNativeJournalRecord(t.Context(), setup.fixture.record, setup.fixture.before, setup.fixture.after, setup.fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := decodeNativeJournalRecord(t.Context(), content, setup.fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownershipstore.NewRecoveryReader(setup.registry)
	if err != nil {
		t.Fatal(err)
	}
	documents := setup.fixture.read
	documents.budget = imageBudget(t)
	return journal, nativeDocumentRecoveryReadOptions{documents: documents, roots: setup.roots, registry: registry, state: func(ctx context.Context) (durable.Snapshot, error) {
		return statefile.LoadOptionalWithOptions(ctx, setup.statePath, statefile.ReadOptions{Codec: setup.fixture.codec, PhysicalPathBudget: documents.budget})
	}}
}

func (setup nativeAssessmentFixture) persist(t *testing.T, state durable.Snapshot, registry ownership.Registry, host []byte) {
	t.Helper()
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

func (setup nativeAssessmentFixture) assertPersisted(t *testing.T, state durable.Snapshot, registry ownership.Registry, host []byte) {
	t.Helper()
	stateBytes, err := setup.fixture.codec.Encode(state)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string][]byte{setup.statePath: stateBytes, setup.registry: registryBytes, setup.host: host} {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("assessment changed durable path %q: %v", path, err)
		}
	}
}

func fixtureTransition(t *testing.T, fixture nativeRecordFixture) ownershipmutation.DocumentClaimTransition {
	t.Helper()
	transition, err := fixture.record.DocumentGroups[0].ExclusiveClaims.canonicalExact(fixture.claims.owner, fixture.record.OperationID, fixture.claims.exact)
	if err != nil {
		t.Fatal(err)
	}
	return transition
}

func assessmentBinding(t *testing.T, intent ownership.DocumentBindingIntent) recoveryDocumentBindingIntent {
	t.Helper()
	record, err := persistedDocumentBindingIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func assessmentRegistry(t *testing.T, claims []ownership.Claim, bindings ...ownership.DocumentBinding) ownership.Registry {
	t.Helper()
	registry, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func assessmentImage(t *testing.T, fixture nativeRecordFixture, expected bool) []byte {
	t.Helper()
	name := "before"
	if expected {
		name = "expected"
	}
	authority, err := fixture.read.artifacts.Authority()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(authority.PhysicalRoot(), "images", name))
	if err != nil {
		t.Fatal(err)
	}
	return content
}
