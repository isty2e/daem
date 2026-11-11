package publication

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/test/testkit/stateimage"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/workmeasure"
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
	fixture     nativeRecordFixture
	roots       journalnamespace.Roots
	statePath   string
	registry    string
	host        string
	groups      []nativeAssessmentGroup
	published   bool
	destination string
}

func TestNativeDocumentAssessmentUsesActualStateRegistryAndHost(t *testing.T) {
	for _, test := range []struct {
		name, state, host string
		want              recovery.Classification
	}{
		{"before/no-op", "before", "before", recovery.ClassificationCleanBefore},
		{"before/rollback", "before", "after", recovery.ClassificationNeedsRollback},
		{"after/clean", "after", "after", recovery.ClassificationCleanAfter},
		{"after/refuses-before", "after", "before", ""},
		{"before/refuses-third", "before", "third", ""},
		{"after/refuses-third", "after", "third", ""},
		{"neither/refuses", "neither", "after", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			setup := newNativeAssessmentFixture(t)
			fixture := &setup.fixture
			claims := *setup.groups[0].claims
			binding, _ := membershipBindingValue(t, fixture.claims).Get()
			registry := assessmentRegistry(t, claims.Before(), binding)
			state := fixture.before
			if test.state == "after" {
				state = fixture.after
			} else if test.state == "neither" {
				state = durable.EmptySnapshot()
			}
			host := setup.image(test.host == "after")
			if test.host == "third" {
				host = []byte(`{"model":false,"outside":1}`)
			}
			setup.persist(t, state, registry, host)
			journal, options := setup.reader(t)
			result, err := journalqualification.AssessNativeDocuments(journal, t.Context(), options)
			if test.want == "" {
				if err == nil || result.Facts().DocumentCount() != 0 || result.Facts().Classification() != recovery.ClassificationBlocked {
					t.Fatalf("refused phase returned usable assessment: %v", err)
				}
			} else if err != nil || result.Facts().Classification() != test.want || result.Facts().DocumentCount() != 1 {
				t.Fatalf("disposition=%s, want=%s: %v", result.Facts().Classification(), test.want, err)
			}
			setup.assertPersisted(t, state, registry, host)
		})
	}
}

func TestNativeDocumentAssessmentRequiresCompleteClaimAndBindingPhases(t *testing.T) {
	for _, test := range []struct {
		name, state, claims, binding string
		want                         recovery.Classification
	}{
		{"rollback-unprepared", "before", "before", "before", recovery.ClassificationNeedsRollback},
		{"rollback-prepared", "before", "prepared", "before", recovery.ClassificationNeedsRollback},
		{"rollback-refuses-active", "before", "after", "before", ""},
		{"rollback-refuses-after-binding", "before", "prepared", "after", ""},
		{"rollback-refuses-third-binding", "before", "prepared", "third", ""},
		{"finalize-refuses-third-binding", "after", "prepared", "third", ""},
		{"finalize-both", "after", "prepared", "before", recovery.ClassificationNeedsFinalize},
		{"finalize-claims", "after", "prepared", "after", recovery.ClassificationNeedsFinalize},
		{"finalize-binding", "after", "after", "before", recovery.ClassificationNeedsFinalize},
		{"after-complete", "after", "after", "after", recovery.ClassificationCleanAfter},
		{"after-refuses-absent-claims", "after", "before", "before", ""},
		{"before-refuses-third", "before", "third", "before", ""},
		{"after-refuses-third", "after", "third", "after", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			setup := newNativeAssessmentFixture(t)
			fixture := &setup.fixture
			fixture.before = durable.EmptySnapshot()
			setup.withImages(aggregate.ExistingDocument([]byte(`{"outside":1}`)), aggregate.ExistingDocument([]byte(`{"model":"fast","outside":1}`)))
			afterClaim := documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "model"))
			transition, err := ownershipmutation.NewDocumentClaimTransition(nil, []ownership.Claim{afterClaim}, fixture.journal.OperationID())
			if err != nil {
				t.Fatal(err)
			}
			setup.groups[0].claims = &transition
			bindingValue := membershipBindingValue(t, fixture.claims)
			binding, _ := bindingValue.Get()
			setup.groups[0].bindings = []ownership.DocumentBindingIntent{membershipIntent(t, fixture.claims, ownership.NoDocumentBinding(), bindingValue)}
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
				other := filepath.Join(namespacetest.LegacyRoot(t, setup.roots, output.RootPiAgent).SelectedPath, "foreign.json")
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
			host := setup.image(true)
			setup.persist(t, state, registry, host)
			journal, options := setup.reader(t)
			result, err := journalqualification.AssessNativeDocuments(journal, t.Context(), options)
			if test.want == "" {
				if err == nil || result.Facts().DocumentCount() != 0 || result.Facts().Classification() != recovery.ClassificationBlocked {
					t.Fatalf("metadata mismatch returned a usable prefix: %v", err)
				}
			} else if err != nil || result.Facts().Classification() != test.want {
				t.Fatalf("metadata disposition=%s want=%s: %v", result.Facts().Classification(), test.want, err)
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
			setup.withImages(aggregate.ExistingDocument(original), aggregate.ExistingDocument(expected))
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
			beforeClaim := documentActiveClaim(t, fixture.claims, output.WholePathFootprint())
			afterClaims := []ownership.Claim{documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "model")), documentActiveClaim(t, fixture.claims, documentKeyFootprint(t, "new_key"))}
			transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{beforeClaim}, afterClaims, fixture.journal.OperationID())
			if err != nil {
				t.Fatal(err)
			}
			setup.groups[0].claims = &transition
			binding, _ := membershipBindingValue(t, fixture.claims).Get()
			registry := assessmentRegistry(t, []ownership.Claim{beforeClaim}, binding)
			host := expected
			if external {
				host = []byte(`{"model":"fast","new_key":2,"outside":7}`)
			}
			setup.persist(t, fixture.before, registry, host)
			journal, options := setup.reader(t)
			result, err := journalqualification.AssessNativeDocuments(journal, t.Context(), options)
			if err != nil || result.Facts().Classification() != recovery.ClassificationNeedsRollback || result.Facts().DocumentCount() != 1 {
				t.Fatalf("narrowing assessment: %v", err)
			}
			restored := requireAssessedDocument(t, result, 0).Restoration().Document().Content()
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
			retry, err := journalqualification.AssessNativeDocuments(journal, t.Context(), options)
			if err != nil || retry.Facts().Classification() != recovery.ClassificationCleanBefore || requireAssessedDocument(t, retry, 0).Restoration().Changed() {
				t.Fatalf("retry did not recognize actual no-op: %v", err)
			}
		})
	}
}

func TestNativeDocumentAssessmentEqualStateAndObservationFailures(t *testing.T) {
	setup := newNativeAssessmentFixture(t)
	fixture := &setup.fixture
	fixture.after = fixture.before
	before := setup.image(false)
	setup.withImages(aggregate.ExistingDocument(before), aggregate.ExistingDocument(before))
	transition := *setup.groups[0].claims
	binding, _ := membershipBindingValue(t, fixture.claims).Get()
	registry := assessmentRegistry(t, transition.Before(), binding)
	setup.persist(t, fixture.before, registry, before)
	journal, options := setup.reader(t)
	result, err := journalqualification.AssessNativeDocuments(journal, t.Context(), options)
	if err != nil || result.Facts().Classification() != recovery.ClassificationCleanAfter || requireAssessedDocument(t, result, 0).Restoration().Changed() {
		t.Fatalf("equal-state expected should remain clean-after: %v", err)
	}

	for _, failure := range []string{"state reader", "cancellation", "host budget", "host read"} {
		t.Run(failure, func(t *testing.T) {
			journal, options := setup.reader(t)
			sentinel := errors.New("native observation refused")
			switch failure {
			case "state reader":
				options.State = func(context.Context) (durable.Snapshot, error) { return durable.Snapshot{}, sentinel }
			case "cancellation":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				result, err := journalqualification.AssessNativeDocuments(journal, ctx, options)
				if !errors.Is(err, context.Canceled) || result.Facts().DocumentCount() != 0 {
					t.Fatalf("cancellation returned a usable prefix: %v", err)
				}
				return
			case "host budget":
				read := options.State
				options.State = func(ctx context.Context) (durable.Snapshot, error) {
					work, err := workmeasure.NewArtifactWork(0, options.Documents.Budget.RemainingBytes())
					if err == nil {
						err = options.Documents.Budget.AdmitTree(work)
					}
					if err != nil {
						return durable.Snapshot{}, err
					}
					return read(ctx)
				}
			case "host read":
				read := options.State
				options.State = func(ctx context.Context) (durable.Snapshot, error) {
					if err := os.WriteFile(setup.host, []byte("invalid selected JSON"), 0o600); err != nil {
						return durable.Snapshot{}, err
					}
					return read(ctx)
				}
			}
			result, err := journalqualification.AssessNativeDocuments(journal, t.Context(), options)
			if err == nil || result.Facts().DocumentCount() != 0 || result.Facts().Classification() != recovery.ClassificationBlocked || failure == "state reader" && !errors.Is(err, sentinel) {
				t.Fatalf("observation failure returned a usable prefix: %v", err)
			}
		})
	}
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

func assessmentRegistry(t *testing.T, claims []ownership.Claim, bindings ...ownership.DocumentBinding) ownership.Registry {
	t.Helper()
	registry, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}
